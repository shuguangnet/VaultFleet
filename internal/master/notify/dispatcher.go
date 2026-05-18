package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

const (
	EventBackupFailed = "backup_failed"
	EventAgentOffline = "agent_offline"
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
}

func (d *Dispatcher) handleEvent(event events.Event) {
	msg, eventName, ok := d.notificationForEvent(event)
	if !ok {
		return
	}

	if err := d.dispatch(context.Background(), eventName, msg); err != nil {
		log.Printf("dispatch notification %s failed: %v", eventName, err)
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, eventName string, msg NotifyMessage) error {
	if d == nil || d.db == nil || d.db.DB == nil {
		return errors.New("notification database not configured")
	}

	var configs []db.NotificationConfig
	if err := d.db.DB.Order("created_at ASC").Find(&configs).Error; err != nil {
		return fmt.Errorf("load notification configs: %w", err)
	}

	var errs []error
	for _, config := range configs {
		if !configMatchesEvent(config.Events, eventName) {
			continue
		}

		notifier, err := d.factory(config.Type, json.RawMessage(config.Config))
		if err != nil {
			errs = append(errs, fmt.Errorf("create %s notifier %s: %w", config.Type, config.ID, err))
			continue
		}
		if err := notifier.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("send %s notification %s: %w", notifier.Type(), config.ID, err))
		}
	}

	return errors.Join(errs...)
}

func (d *Dispatcher) notificationForEvent(event events.Event) (NotifyMessage, string, bool) {
	switch event.Type {
	case events.AgentOffline:
		agentName := payloadAgentName(event.Payload)
		if agentName == "" {
			return NotifyMessage{}, "", false
		}
		return NotifyMessage{
			Title:     "Agent Offline",
			Body:      fmt.Sprintf("Agent %s is offline.", agentName),
			Level:     LevelWarning,
			AgentName: agentName,
			Timestamp: d.now().UTC(),
		}, EventAgentOffline, true
	case events.TaskResult:
		return d.backupFailedMessage(event.Payload)
	default:
		return NotifyMessage{}, "", false
	}
}

func (d *Dispatcher) backupFailedMessage(payload any) (NotifyMessage, string, bool) {
	result, fallbackAgentID, ok := parseTaskResultPayload(payload)
	if !ok {
		return NotifyMessage{}, "", false
	}
	if result.TaskType != "backup" || !isFailureStatus(result.Status) {
		return NotifyMessage{}, "", false
	}

	agentName := result.AgentID
	if agentName == "" {
		agentName = fallbackAgentID
	}
	body := result.ErrorLog
	if body == "" {
		body = fmt.Sprintf("Backup task failed with status %q.", result.Status)
	}
	timestamp := result.FinishedAt
	if timestamp.IsZero() {
		timestamp = d.now().UTC()
	}

	return NotifyMessage{
		Title:     "Backup Failed",
		Body:      body,
		Level:     LevelError,
		AgentName: agentName,
		Timestamp: timestamp.UTC(),
	}, EventBackupFailed, true
}

func NewNotifierFromConfig(notificationType string, raw json.RawMessage) (Notifier, error) {
	switch notificationType {
	case "telegram":
		var config TelegramConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode telegram config: %w", err)
		}
		if strings.TrimSpace(config.BotToken) == "" {
			return nil, errors.New("telegram bot_token is required")
		}
		if strings.TrimSpace(config.ChatID) == "" {
			return nil, errors.New("telegram chat_id is required")
		}
		return NewTelegramNotifier(config), nil
	case "webhook":
		var config WebhookConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode webhook config: %w", err)
		}
		if strings.TrimSpace(config.URL) == "" {
			return nil, errors.New("webhook url is required")
		}
		return NewWebhookNotifier(config), nil
	default:
		return nil, fmt.Errorf("unknown notification type %q", notificationType)
	}
}

func configMatchesEvent(rawEvents string, eventName string) bool {
	var eventNames []string
	if err := json.Unmarshal([]byte(rawEvents), &eventNames); err != nil {
		return false
	}
	for _, name := range eventNames {
		if name == eventName {
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

func isFailureStatus(status string) bool {
	switch strings.ToLower(status) {
	case "failed", "failure", "error":
		return true
	default:
		return false
	}
}
