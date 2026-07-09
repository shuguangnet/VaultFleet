package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

const (
	EventBackupSucceeded             = "backup_succeeded"
	EventBackupFailed                = "backup_failed"
	EventBackupVerificationSucceeded = "backup_verification_succeeded"
	EventBackupVerificationFailed    = "backup_verification_failed"
	EventAgentOffline                = "agent_offline"

	defaultHTTPTimeout = 10 * time.Second
	defaultSendTimeout = 10 * time.Second
)

type NotifierFactory func(notificationType string, raw json.RawMessage) (Notifier, error)

type Dispatcher struct {
	db      *db.Database
	bus     *events.Bus
	factory NotifierFactory
	now     func() time.Time
}

type DispatcherOption func(*Dispatcher)

func NewDispatcher(database *db.Database, bus *events.Bus, options ...DispatcherOption) *Dispatcher {
	dispatcher := &Dispatcher{
		db:      database,
		bus:     bus,
		factory: NewNotifierFromConfig,
		now:     time.Now,
	}
	for _, option := range options {
		option(dispatcher)
	}
	return dispatcher
}

func WithNotifierFactory(factory NotifierFactory) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		if factory != nil {
			dispatcher.factory = factory
		}
	}
}

func (d *Dispatcher) Start() {
	if d == nil || d.bus == nil {
		return
	}

	d.bus.Subscribe(events.AgentOffline, d.handleEvent)
	d.bus.Subscribe(events.TaskResult, d.handleEvent)
	d.bus.Subscribe(events.EventType(EventBackupFailed), d.handleEvent)
}

func (d *Dispatcher) handleEvent(event events.Event) {
	msg, eventNames, ok := d.notificationForEvent(event)
	if !ok {
		return
	}

	go func() {
		if err := d.dispatch(context.Background(), eventNames, msg); err != nil {
			log.Printf("dispatch notification %s failed: %v", strings.Join(eventNames, ","), err)
		}
	}()
}

func (d *Dispatcher) dispatch(ctx context.Context, eventNames []string, msg NotifyMessage) error {
	if d == nil || d.db == nil || d.db.DB == nil {
		return errors.New("notification database not configured")
	}

	var configs []db.NotificationConfig
	if err := d.db.DB.Order("created_at ASC").Find(&configs).Error; err != nil {
		return fmt.Errorf("load notification configs: %w", err)
	}

	var errs []error
	for _, config := range configs {
		if !configMatchesAnyEvent(config.Events, eventNames) {
			continue
		}

		rawConfig, err := decryptNotificationConfig(config.Config, d.db.MasterKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("decrypt notification config %s: %w", config.ID, err))
			continue
		}

		notifier, err := d.factory(config.Type, json.RawMessage(rawConfig))
		if err != nil {
			errs = append(errs, fmt.Errorf("create %s notifier %s: %w", config.Type, config.ID, err))
			continue
		}
		go d.send(ctx, notifier, config.ID, msg)
	}

	return errors.Join(errs...)
}

func (d *Dispatcher) send(parent context.Context, notifier Notifier, configID string, msg NotifyMessage) {
	ctx, cancel := context.WithTimeout(parent, defaultSendTimeout)
	defer cancel()

	if err := notifier.Send(ctx, msg); err != nil {
		log.Printf("send %s notification %s failed: %v", notifier.Type(), configID, err)
	}
}

func (d *Dispatcher) notificationForEvent(event events.Event) (NotifyMessage, []string, bool) {
	switch event.Type {
	case events.AgentOffline:
		agentName := d.displayAgentName(payloadAgentName(event.Payload))
		if agentName == "" {
			return NotifyMessage{}, nil, false
		}
		return NotifyMessage{
			Title:     "Agent Offline",
			Body:      fmt.Sprintf("Agent %s is offline.", agentName),
			Level:     LevelWarning,
			AgentName: agentName,
			Timestamp: d.now().UTC(),
		}, []string{EventAgentOffline}, true
	case events.TaskResult:
		return d.taskResultMessage(event.Payload)
	case events.EventType(EventBackupFailed):
		return d.directBackupFailedMessage(event.Payload)
	default:
		return NotifyMessage{}, nil, false
	}
}

func (d *Dispatcher) directBackupFailedMessage(payload any) (NotifyMessage, []string, bool) {
	agentName := d.displayAgentName(payloadAgentName(payload))
	if agentName == "" {
		agentName = "unknown"
	}

	body := payloadString(payload, "error", "error_log", "message")
	if body == "" {
		body = "Backup failed."
	}

	timestamp := payloadTimestamp(payload)
	if timestamp.IsZero() {
		timestamp = d.now().UTC()
	}

	return NotifyMessage{
		Title:     "Backup Failed",
		Body:      body,
		Level:     LevelError,
		AgentName: agentName,
		Timestamp: timestamp.UTC(),
	}, []string{EventBackupFailed}, true
}

func (d *Dispatcher) taskResultMessage(payload any) (NotifyMessage, []string, bool) {
	result, fallbackAgentID, ok := parseTaskResultPayload(payload)
	if !ok {
		return NotifyMessage{}, nil, false
	}
	taskType := strings.ToLower(strings.TrimSpace(result.TaskType))
	if taskType != "backup" && taskType != "verify" {
		return NotifyMessage{}, nil, false
	}

	success := isSuccessStatus(result.Status)
	failure := isFailureStatus(result.Status)
	if !success && !failure {
		return NotifyMessage{}, nil, false
	}

	var eventNames []string
	level := LevelInfo
	outcome := "Succeeded"
	if taskType == "verify" {
		if success {
			eventNames = []string{EventBackupVerificationSucceeded}
		} else {
			eventNames = []string{EventBackupVerificationFailed, EventBackupFailed}
		}
	} else if success {
		eventNames = []string{EventBackupSucceeded}
	} else {
		eventNames = []string{EventBackupFailed}
	}
	if failure {
		level = LevelError
		outcome = "Failed"
	}

	agentName := d.displayAgentName(result.AgentID)
	if agentName == "" {
		agentName = d.displayAgentName(fallbackAgentID)
	}
	timestamp := result.FinishedAt
	if timestamp.IsZero() {
		timestamp = d.now().UTC()
	}

	return NotifyMessage{
		Title:     taskResultDisplayName(taskType) + " " + outcome,
		Body:      taskResultNotificationBody(result),
		Level:     level,
		AgentName: agentName,
		Timestamp: timestamp.UTC(),
	}, eventNames, true
}

func taskResultDisplayName(taskType string) string {
	if taskType == "verify" {
		return "Backup Verification"
	}
	return "Backup"
}

func (d *Dispatcher) displayAgentName(agentIDOrName string) string {
	if strings.TrimSpace(agentIDOrName) == "" {
		return ""
	}
	if d == nil || d.db == nil || d.db.DB == nil {
		return agentIDOrName
	}

	var agent db.Agent
	if err := d.db.DB.First(&agent, "id = ?", agentIDOrName).Error; err == nil && agent.Name != "" {
		return agent.Name
	}
	return agentIDOrName
}

func NewNotifierFromConfig(notificationType string, raw json.RawMessage) (Notifier, error) {
	switch notificationType {
	case "telegram":
		var config TelegramConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, fmt.Errorf("decode telegram config: %w", err)
		}
		if strings.TrimSpace(config.BotToken) == "" {
			return nil, errors.New("telegram bot_token is required")
		}
		if strings.TrimSpace(config.ChatID) == "" {
			return nil, errors.New("telegram chat_id is required")
		}
		if strings.TrimSpace(config.BaseURL) != "" {
			if err := validateWebhookURL(config.BaseURL); err != nil {
				return nil, fmt.Errorf("telegram base_url: %w", err)
			}
		}
		return NewTelegramNotifier(config), nil
	case "webhook":
		var config WebhookConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, fmt.Errorf("decode webhook config: %w", err)
		}
		if strings.TrimSpace(config.URL) == "" {
			return nil, errors.New("webhook url is required")
		}
		if err := validateWebhookURL(config.URL); err != nil {
			return nil, err
		}
		if err := validateWebhookHeaders(config.Headers); err != nil {
			return nil, err
		}
		return NewWebhookNotifier(config), nil
	case "email":
		var config EmailConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, fmt.Errorf("decode email config: %w", err)
		}
		if err := ValidateEmailConfig(config); err != nil {
			return nil, err
		}
		return NewEmailNotifier(config), nil
	default:
		return nil, fmt.Errorf("unknown notification type %q", notificationType)
	}
}

func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func decryptNotificationConfig(rawConfig string, key []byte) (string, error) {
	plaintext, err := db.Decrypt(rawConfig, key)
	if err == nil {
		return plaintext, nil
	}
	if json.Valid([]byte(rawConfig)) {
		return rawConfig, nil
	}
	return "", err
}

func configMatchesAnyEvent(rawEvents string, wantedEvents []string) bool {
	var configuredEvents []string
	if err := json.Unmarshal([]byte(rawEvents), &configuredEvents); err != nil {
		return false
	}
	wanted := make(map[string]struct{}, len(wantedEvents))
	for _, name := range wantedEvents {
		wanted[name] = struct{}{}
	}
	for _, name := range configuredEvents {
		if _, ok := wanted[name]; ok {
			return true
		}
	}
	return false
}

func payloadAgentName(payload any) string {
	switch value := payload.(type) {
	case string:
		return value
	case map[string]any:
		return stringFromMap(value, "agent_name", "agent_id", "id")
	default:
		return ""
	}
}

func parseTaskResultPayload(payload any) (protocol.TaskResultPayload, string, bool) {
	var fallbackAgentID string
	raw := json.RawMessage(nil)

	switch value := payload.(type) {
	case protocol.TaskResultPayload:
		return value, value.AgentID, true
	case *protocol.TaskResultPayload:
		if value == nil {
			return protocol.TaskResultPayload{}, "", false
		}
		return *value, value.AgentID, true
	case json.RawMessage:
		raw = value
	case []byte:
		raw = value
	case map[string]any:
		fallbackAgentID = stringFromMap(value, "agent_id")
		raw = rawPayloadFromMap(value)
	default:
		return protocol.TaskResultPayload{}, "", false
	}

	if len(raw) == 0 {
		return protocol.TaskResultPayload{}, fallbackAgentID, false
	}

	var result protocol.TaskResultPayload
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.TaskResultPayload{}, fallbackAgentID, false
	}
	return result, fallbackAgentID, true
}

func rawPayloadFromMap(payload map[string]any) json.RawMessage {
	switch raw := payload["payload"].(type) {
	case json.RawMessage:
		return raw
	case []byte:
		return raw
	case string:
		return json.RawMessage(raw)
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return data
	}
}

func stringFromMap(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func payloadString(payload any, keys ...string) string {
	if value, ok := payload.(map[string]any); ok {
		return stringFromMap(value, keys...)
	}
	return ""
}

func payloadTimestamp(payload any) time.Time {
	value, ok := payload.(map[string]any)
	if !ok {
		return time.Time{}
	}

	switch timestamp := value["timestamp"].(type) {
	case time.Time:
		return timestamp
	case string:
		parsed, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			return parsed
		}
		parsed, err = time.Parse(time.RFC3339Nano, timestamp)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func isFailureStatus(status string) bool {
	switch strings.ToLower(status) {
	case "failed", "failure", "error", "timeout", "timed_out", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func isSuccessStatus(status string) bool {
	switch strings.ToLower(status) {
	case "success", "succeeded", "ok":
		return true
	default:
		return false
	}
}

func taskResultNotificationBody(result protocol.TaskResultPayload) string {
	lines := []string{
		fmt.Sprintf("Status: %s", fallbackText(result.Status, "unknown")),
	}
	if result.DurationMs > 0 {
		lines = append(lines, fmt.Sprintf("Duration: %s", formatDurationMs(result.DurationMs)))
	}
	if result.SnapshotID != "" {
		lines = append(lines, "Snapshot: "+result.SnapshotID)
	}
	if result.ArtifactName != "" {
		lines = append(lines, "Artifact: "+result.ArtifactName)
	}
	if result.ArtifactPath != "" {
		lines = append(lines, "Artifact path: "+result.ArtifactPath)
	}
	if result.ArtifactSize > 0 {
		lines = append(lines, fmt.Sprintf("Artifact size: %d bytes", result.ArtifactSize))
	}
	if result.RepoSize > 0 {
		lines = append(lines, fmt.Sprintf("Repository size: %d bytes", result.RepoSize))
	}
	if strings.TrimSpace(result.ErrorLog) != "" {
		lines = append(lines, "Error: "+strings.TrimSpace(result.ErrorLog))
	}
	return strings.Join(lines, "\n")
}

func fallbackText(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func formatDurationMs(durationMs int64) string {
	duration := time.Duration(durationMs) * time.Millisecond
	if duration < time.Second {
		return duration.String()
	}
	return duration.Round(time.Millisecond).String()
}
