package notify

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

func TestNewNotifierFromConfigBuildsTelegramAndWebhook(t *testing.T) {
	tgConfig := json.RawMessage(`{"bot_token":"token","chat_id":"chat","base_url":"https://example.test"}`)
	tg, err := NewNotifierFromConfig("telegram", tgConfig)
	require.NoError(t, err)
	assert.Equal(t, "telegram", tg.Type())

	whConfig := json.RawMessage(`{"url":"https://hooks.example.test","headers":{"Authorization":"Bearer secret"}}`)
	wh, err := NewNotifierFromConfig("webhook", whConfig)
	require.NoError(t, err)
	assert.Equal(t, "webhook", wh.Type())
}

func TestNewNotifierFromConfigRejectsUnknownOrInvalidConfig(t *testing.T) {
	_, err := NewNotifierFromConfig("email", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown notification type")

	_, err = NewNotifierFromConfig("telegram", json.RawMessage(`{"bot_token":"token"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat_id")

	_, err = NewNotifierFromConfig("webhook", json.RawMessage(`{"headers":{"X-Test":"value"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url")
}

func TestDispatcherSendsAgentOfflineNotificationsForMatchingConfigs(t *testing.T) {
	database := setupNotifyTestDB(t)
	bus := events.NewBus()
	notifier := &recordingNotifier{}
	dispatcher := NewDispatcher(database, bus, WithNotifierFactory(func(notificationType string, raw json.RawMessage) (Notifier, error) {
		assert.Equal(t, "webhook", notificationType)
		assert.JSONEq(t, `{"url":"https://example.test"}`, string(raw))
		return notifier, nil
	}))
	createNotifyConfig(t, database, "webhook", `{"url":"https://example.test"}`, []string{"agent_offline"})

	dispatcher.Start()
	bus.Publish(events.Event{Type: events.AgentOffline, Payload: "agent-1"})

	require.Len(t, notifier.sent, 1)
	msg := notifier.sent[0]
	assert.Equal(t, "Agent Offline", msg.Title)
	assert.Equal(t, LevelWarning, msg.Level)
	assert.Equal(t, "agent-1", msg.AgentName)
	assert.Contains(t, msg.Body, "agent-1")
	assert.False(t, msg.Timestamp.IsZero())
}

func TestDispatcherDerivesBackupFailedFromFailedBackupTaskResult(t *testing.T) {
	database := setupNotifyTestDB(t)
	bus := events.NewBus()
	notifier := &recordingNotifier{}
	dispatcher := NewDispatcher(database, bus, WithNotifierFactory(func(string, json.RawMessage) (Notifier, error) {
		return notifier, nil
	}))
	createNotifyConfig(t, database, "webhook", `{"url":"https://example.test"}`, []string{"backup_failed"})
	payload, err := json.Marshal(protocol.TaskResultPayload{
		AgentID:  "agent-from-result",
		TaskType: "backup",
		Status:   "failed",
		ErrorLog: "repository locked",
	})
	require.NoError(t, err)

	dispatcher.Start()
	bus.Publish(events.Event{
		Type: events.TaskResult,
		Payload: map[string]any{
			"agent_id": "agent-from-event",
			"payload":  json.RawMessage(payload),
		},
	})

	require.Len(t, notifier.sent, 1)
	msg := notifier.sent[0]
	assert.Equal(t, "Backup Failed", msg.Title)
	assert.Equal(t, LevelError, msg.Level)
	assert.Equal(t, "agent-from-result", msg.AgentName)
	assert.Contains(t, msg.Body, "repository locked")
}

func TestDispatcherIgnoresSuccessfulOrNonBackupTaskResults(t *testing.T) {
	database := setupNotifyTestDB(t)
	bus := events.NewBus()
	notifier := &recordingNotifier{}
	dispatcher := NewDispatcher(database, bus, WithNotifierFactory(func(string, json.RawMessage) (Notifier, error) {
		return notifier, nil
	}))
	createNotifyConfig(t, database, "webhook", `{"url":"https://example.test"}`, []string{"backup_failed"})

	dispatcher.Start()
	publishTaskResult(t, bus, protocol.TaskResultPayload{AgentID: "agent-1", TaskType: "backup", Status: "success"})
	publishTaskResult(t, bus, protocol.TaskResultPayload{AgentID: "agent-1", TaskType: "restore", Status: "failed", ErrorLog: "restore failed"})

	assert.Empty(t, notifier.sent)
}

func TestDispatcherSkipsNonMatchingConfigsAndContinuesAfterSendError(t *testing.T) {
	database := setupNotifyTestDB(t)
	bus := events.NewBus()
	failing := &recordingNotifier{err: errors.New("send failed")}
	successful := &recordingNotifier{}
	calls := 0
	dispatcher := NewDispatcher(database, bus, WithNotifierFactory(func(string, json.RawMessage) (Notifier, error) {
		calls++
		if calls == 1 {
			return failing, nil
		}
		return successful, nil
	}))
	createNotifyConfig(t, database, "webhook", `{"url":"https://first.example.test"}`, []string{"agent_offline"})
	createNotifyConfig(t, database, "webhook", `{"url":"https://ignored.example.test"}`, []string{"backup_failed"})
	createNotifyConfig(t, database, "webhook", `{"url":"https://second.example.test"}`, []string{"agent_offline"})

	dispatcher.Start()
	bus.Publish(events.Event{Type: events.AgentOffline, Payload: "agent-1"})

	assert.Len(t, failing.sent, 1)
	assert.Len(t, successful.sent, 1)
	assert.Equal(t, 2, calls)
}

func publishTaskResult(t *testing.T, bus *events.Bus, result protocol.TaskResultPayload) {
	t.Helper()

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	bus.Publish(events.Event{
		Type: events.TaskResult,
		Payload: map[string]any{
			"agent_id": result.AgentID,
			"payload":  json.RawMessage(payload),
		},
	})
}

func setupNotifyTestDB(t *testing.T) *db.Database {
	t.Helper()

	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	return database
}

func createNotifyConfig(t *testing.T, database *db.Database, notificationType, config string, eventNames []string) db.NotificationConfig {
	t.Helper()

	eventsJSON, err := json.Marshal(eventNames)
	require.NoError(t, err)

	notificationConfig := db.NotificationConfig{
		Type:   notificationType,
		Config: config,
		Events: string(eventsJSON),
	}
	require.NoError(t, database.DB.Create(&notificationConfig).Error)
	return notificationConfig
}

type recordingNotifier struct {
	sent []NotifyMessage
	err  error
}

func (n *recordingNotifier) Send(_ context.Context, msg NotifyMessage) error {
	n.sent = append(n.sent, msg)
	return n.err
}

func (n *recordingNotifier) Type() string {
	return "recording"
}
