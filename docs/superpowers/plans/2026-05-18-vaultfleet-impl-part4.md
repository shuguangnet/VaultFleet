### Task 14: Notification System (Telegram + Webhook)

**Files:**
- `internal/master/notify/notify.go` — Notifier interface + NotifyMessage
- `internal/master/notify/telegram.go` — Telegram Bot API sender
- `internal/master/notify/webhook.go` — Generic webhook POST
- `internal/master/notify/dispatcher.go` — Event→notification routing
- `internal/master/notify/notify_test.go`
- `internal/master/notify/telegram_test.go`
- `internal/master/notify/webhook_test.go`
- `internal/master/notify/dispatcher_test.go`
- `internal/master/api/notifications.go` — notification config CRUD API
- `internal/master/api/notifications_test.go`

**Steps:**

- [ ] 14.1 — Write Notifier interface and NotifyMessage tests (`internal/master/notify/notify_test.go`)

```go
// internal/master/notify/notify_test.go
package notify

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNotifyMessageLevels(t *testing.T) {
	levels := []Level{LevelInfo, LevelWarning, LevelError}
	assert.Len(t, levels, 3)

	seen := make(map[Level]bool)
	for _, l := range levels {
		assert.NotEmpty(t, string(l))
		assert.False(t, seen[l], "duplicate level: %s", l)
		seen[l] = true
	}
}

func TestNotifyMessageFormat(t *testing.T) {
	msg := NotifyMessage{
		Title:     "Backup Failed",
		Body:      "restic exit code 1 - repository locked",
		Level:     LevelError,
		AgentName: "Tokyo-1",
		Timestamp: time.Date(2026, 5, 18, 3, 0, 15, 0, time.UTC),
	}

	assert.Equal(t, "Backup Failed", msg.Title)
	assert.Equal(t, LevelError, msg.Level)
	assert.Equal(t, "Tokyo-1", msg.AgentName)
	assert.Contains(t, msg.Body, "repository locked")
}

type mockNotifier struct {
	sent []NotifyMessage
	err  error
}

func (m *mockNotifier) Send(ctx context.Context, msg NotifyMessage) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockNotifier) Type() string {
	return "mock"
}

func TestNotifierInterface(t *testing.T) {
	var n Notifier = &mockNotifier{}
	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test body",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := n.Send(context.Background(), msg)
	assert.NoError(t, err)
	assert.Equal(t, "mock", n.Type())
}
```

- [ ] 14.2 — Implement Notifier interface and NotifyMessage (`internal/master/notify/notify.go`)

```go
// internal/master/notify/notify.go
package notify

import (
	"context"
	"time"
)

type Level string

const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

type NotifyMessage struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Level     Level     `json:"level"`
	AgentName string    `json:"agent_name"`
	Timestamp time.Time `json:"timestamp"`
}

type Notifier interface {
	Send(ctx context.Context, msg NotifyMessage) error
	Type() string
}
```

- [ ] 14.3 — Run tests (expect pass for interface + message)

```bash
go test ./internal/master/notify/... -run "TestNotifyMessage|TestNotifierInterface" -v
```

- [ ] 14.4 — Write Telegram notifier tests (`internal/master/notify/telegram_test.go`)

```go
// internal/master/notify/telegram_test.go
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramNotifier_Send(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tg := NewTelegramNotifier(TelegramConfig{
		BotToken: "123456:ABC-DEF",
		ChatID:   "-100999888",
		BaseURL:  server.URL,
	})

	msg := NotifyMessage{
		Title:     "❌ 备份失败",
		Body:      "restic exit code 1 - repository locked",
		Level:     LevelError,
		AgentName: "Tokyo-1",
		Timestamp: time.Date(2026, 5, 18, 3, 0, 15, 0, time.UTC),
	}

	err := tg.Send(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, "/bot123456:ABC-DEF/sendMessage", receivedPath)
	assert.Equal(t, "-100999888", receivedBody["chat_id"])
	text := receivedBody["text"].(string)
	assert.Contains(t, text, "备份失败")
	assert.Contains(t, text, "Tokyo-1")
	assert.Contains(t, text, "repository locked")
	assert.Equal(t, "HTML", receivedBody["parse_mode"])
}

func TestTelegramNotifier_SendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer server.Close()

	tg := NewTelegramNotifier(TelegramConfig{
		BotToken: "123456:ABC-DEF",
		ChatID:   "-100invalid",
		BaseURL:  server.URL,
	})

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := tg.Send(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "telegram API error")
}

func TestTelegramNotifier_SendNetworkError(t *testing.T) {
	tg := NewTelegramNotifier(TelegramConfig{
		BotToken: "123456:ABC-DEF",
		ChatID:   "-100999",
		BaseURL:  "http://127.0.0.1:1",
	})

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := tg.Send(context.Background(), msg)
	require.Error(t, err)
}

func TestTelegramNotifier_Type(t *testing.T) {
	tg := NewTelegramNotifier(TelegramConfig{
		BotToken: "token",
		ChatID:   "chat",
	})
	assert.Equal(t, "telegram", tg.Type())
}

func TestTelegramNotifier_FormatMessage(t *testing.T) {
	tg := NewTelegramNotifier(TelegramConfig{
		BotToken: "token",
		ChatID:   "chat",
	})

	tests := []struct {
		name  string
		msg   NotifyMessage
		check func(string)
	}{
		{
			name: "error level",
			msg: NotifyMessage{
				Title:     "Backup Failed",
				Body:      "disk full",
				Level:     LevelError,
				AgentName: "Tokyo-1",
				Timestamp: time.Date(2026, 5, 18, 3, 0, 0, 0, time.UTC),
			},
			check: func(text string) {
				assert.Contains(t, text, "Backup Failed")
				assert.Contains(t, text, "Tokyo-1")
				assert.Contains(t, text, "disk full")
			},
		},
		{
			name: "warning level",
			msg: NotifyMessage{
				Title:     "Agent Offline",
				Body:      "offline for 5 minutes",
				Level:     LevelWarning,
				AgentName: "London-2",
				Timestamp: time.Date(2026, 5, 18, 4, 0, 0, 0, time.UTC),
			},
			check: func(text string) {
				assert.Contains(t, text, "Agent Offline")
				assert.Contains(t, text, "London-2")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := tg.formatMessage(tt.msg)
			tt.check(text)
		})
	}
}
```

- [ ] 14.5 — Run Telegram tests (expect fail — implementation not written)

```bash
go test ./internal/master/notify/... -run "TestTelegram" -v
```

- [ ] 14.6 — Implement Telegram notifier (`internal/master/notify/telegram.go`)

```go
// internal/master/notify/telegram.go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	BaseURL  string `json:"-"`
}

type TelegramNotifier struct {
	config TelegramConfig
	client *http.Client
}

func NewTelegramNotifier(config TelegramConfig) *TelegramNotifier {
	if config.BaseURL == "" {
		config.BaseURL = defaultTelegramBaseURL
	}
	return &TelegramNotifier{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TelegramNotifier) Type() string {
	return "telegram"
}

func (t *TelegramNotifier) Send(ctx context.Context, msg NotifyMessage) error {
	text := t.formatMessage(msg)

	payload := map[string]interface{}{
		"chat_id":    t.config.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.config.BaseURL, t.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (t *TelegramNotifier) formatMessage(msg NotifyMessage) string {
	levelIcon := "ℹ️"
	switch msg.Level {
	case LevelWarning:
		levelIcon = "⚠️"
	case LevelError:
		levelIcon = "❌"
	}

	return fmt.Sprintf(
		"%s <b>%s</b>\n\n节点: %s\n时间: %s\n\n%s",
		levelIcon,
		msg.Title,
		msg.AgentName,
		msg.Timestamp.Format("2006-01-02 15:04:05 MST"),
		msg.Body,
	)
}
```

- [ ] 14.7 — Verify Telegram tests pass

```bash
go test ./internal/master/notify/... -run "TestTelegram" -v
```

- [ ] 14.8 — Write Webhook notifier tests (`internal/master/notify/webhook_test.go`)

```go
// internal/master/notify/webhook_test.go
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookNotifier_Send(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedMethod string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := NewWebhookNotifier(WebhookConfig{
		URL: server.URL,
	})

	msg := NotifyMessage{
		Title:     "Backup Failed",
		Body:      "restic exit code 1",
		Level:     LevelError,
		AgentName: "Tokyo-1",
		Timestamp: time.Date(2026, 5, 18, 3, 0, 15, 0, time.UTC),
	}

	err := wh.Send(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, "POST", receivedMethod)
	assert.Equal(t, "application/json", receivedContentType)
	assert.Equal(t, "Backup Failed", receivedBody["title"])
	assert.Equal(t, "restic exit code 1", receivedBody["body"])
	assert.Equal(t, "error", receivedBody["level"])
	assert.Equal(t, "Tokyo-1", receivedBody["agent_name"])
	assert.NotEmpty(t, receivedBody["timestamp"])
}

func TestWebhookNotifier_SendWithHeaders(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := NewWebhookNotifier(WebhookConfig{
		URL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer secret-token",
		},
	})

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test body",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := wh.Send(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret-token", receivedAuth)
}

func TestWebhookNotifier_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	wh := NewWebhookNotifier(WebhookConfig{URL: server.URL})

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := wh.Send(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook error")
}

func TestWebhookNotifier_NetworkError(t *testing.T) {
	wh := NewWebhookNotifier(WebhookConfig{
		URL: "http://127.0.0.1:1",
	})

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := wh.Send(context.Background(), msg)
	require.Error(t, err)
}

func TestWebhookNotifier_Type(t *testing.T) {
	wh := NewWebhookNotifier(WebhookConfig{URL: "http://example.com"})
	assert.Equal(t, "webhook", wh.Type())
}

func TestWebhookNotifier_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := NewWebhookNotifier(WebhookConfig{URL: server.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := wh.Send(ctx, msg)
	require.Error(t, err)
}
```

- [ ] 14.9 — Run Webhook tests (expect fail)

```bash
go test ./internal/master/notify/... -run "TestWebhook" -v
```

- [ ] 14.10 — Implement Webhook notifier (`internal/master/notify/webhook.go`)

```go
// internal/master/notify/webhook.go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type WebhookNotifier struct {
	config WebhookConfig
	client *http.Client
}

func NewWebhookNotifier(config WebhookConfig) *WebhookNotifier {
	return &WebhookNotifier{
		config: config,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *WebhookNotifier) Type() string {
	return "webhook"
}

func (w *WebhookNotifier) Send(ctx context.Context, msg NotifyMessage) error {
	payload := map[string]interface{}{
		"title":      msg.Title,
		"body":       msg.Body,
		"level":      string(msg.Level),
		"agent_name": msg.AgentName,
		"timestamp":  msg.Timestamp.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
```

- [ ] 14.11 — Verify Webhook tests pass

```bash
go test ./internal/master/notify/... -run "TestWebhook" -v
```

- [ ] 14.12 — Write Dispatcher tests (`internal/master/notify/dispatcher_test.go`)

```go
// internal/master/notify/dispatcher_test.go
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/events"
)

type fakeNotifierRepo struct {
	mu      sync.Mutex
	configs []NotificationConfigRecord
}

func (r *fakeNotifierRepo) ListNotificationConfigs() ([]NotificationConfigRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.configs, nil
}

func TestDispatcher_BackupFailed(t *testing.T) {
	mock := &mockNotifier{}
	repo := &fakeNotifierRepo{
		configs: []NotificationConfigRecord{
			{
				ID:     "nc-1",
				Type:   "mock",
				Config: `{}`,
				Events: `["backup_failed"]`,
			},
		},
	}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		return mock, nil
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type: events.EventType("backup_failed"),
		Payload: map[string]interface{}{
			"agent_name": "Tokyo-1",
			"error":      "restic exit code 1",
		},
	})

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, mock.sent, 1)
	assert.Equal(t, "Tokyo-1", mock.sent[0].AgentName)
	assert.Equal(t, LevelError, mock.sent[0].Level)
	assert.Contains(t, mock.sent[0].Body, "restic exit code 1")
}

func TestDispatcher_AgentOffline(t *testing.T) {
	mock := &mockNotifier{}
	repo := &fakeNotifierRepo{
		configs: []NotificationConfigRecord{
			{
				ID:     "nc-2",
				Type:   "mock",
				Config: `{}`,
				Events: `["agent_offline"]`,
			},
		},
	}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		return mock, nil
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type:    events.AgentOffline,
		Payload: map[string]interface{}{"agent_name": "London-2"},
	})

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, mock.sent, 1)
	assert.Equal(t, "London-2", mock.sent[0].AgentName)
	assert.Equal(t, LevelWarning, mock.sent[0].Level)
}

func TestDispatcher_IgnoresUnsubscribedEvents(t *testing.T) {
	mock := &mockNotifier{}
	repo := &fakeNotifierRepo{
		configs: []NotificationConfigRecord{
			{
				ID:     "nc-3",
				Type:   "mock",
				Config: `{}`,
				Events: `["backup_failed"]`,
			},
		},
	}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		return mock, nil
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type:    events.AgentOnline,
		Payload: map[string]interface{}{"agent_name": "Tokyo-1"},
	})

	time.Sleep(100 * time.Millisecond)
	assert.Len(t, mock.sent, 0)
}

func TestDispatcher_MultipleConfigs(t *testing.T) {
	mock1 := &mockNotifier{}
	mock2 := &mockNotifier{}
	callCount := 0

	repo := &fakeNotifierRepo{
		configs: []NotificationConfigRecord{
			{ID: "nc-a", Type: "telegram", Config: `{"bot_token":"t1","chat_id":"c1"}`, Events: `["backup_failed"]`},
			{ID: "nc-b", Type: "webhook", Config: `{"url":"http://example.com"}`, Events: `["backup_failed","agent_offline"]`},
		},
	}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		callCount++
		if ncType == "telegram" {
			return mock1, nil
		}
		return mock2, nil
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type: events.EventType("backup_failed"),
		Payload: map[string]interface{}{
			"agent_name": "Tokyo-1",
			"error":      "disk full",
		},
	})

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, mock1.sent, 1)
	assert.Len(t, mock2.sent, 1)
}

func TestDispatcher_NotifierError(t *testing.T) {
	failingNotifier := &mockNotifier{err: errors.New("send failed")}
	repo := &fakeNotifierRepo{
		configs: []NotificationConfigRecord{
			{ID: "nc-fail", Type: "mock", Config: `{}`, Events: `["backup_failed"]`},
		},
	}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		return failingNotifier, nil
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type: events.EventType("backup_failed"),
		Payload: map[string]interface{}{
			"agent_name": "Tokyo-1",
			"error":      "test",
		},
	})

	time.Sleep(100 * time.Millisecond)
	// Should not panic even when notifier fails
}

func TestDispatcher_NoConfigs(t *testing.T) {
	repo := &fakeNotifierRepo{configs: nil}

	bus := events.NewBus()
	factory := func(ncType, config string) (Notifier, error) {
		return nil, errors.New("should not be called")
	}

	d := NewDispatcher(bus, repo, factory)
	d.Start()
	defer d.Stop()

	bus.Publish(events.Event{
		Type:    events.EventType("backup_failed"),
		Payload: map[string]interface{}{"agent_name": "Tokyo-1", "error": "test"},
	})

	time.Sleep(100 * time.Millisecond)
	// Should not panic with zero configs
}

func TestBuildNotifyMessage_BackupFailed(t *testing.T) {
	payload := map[string]interface{}{
		"agent_name": "Tokyo-1",
		"error":      "restic exit code 1",
	}

	msg := buildNotifyMessage("backup_failed", payload)
	assert.Equal(t, "备份失败", msg.Title)
	assert.Equal(t, LevelError, msg.Level)
	assert.Equal(t, "Tokyo-1", msg.AgentName)
	assert.Contains(t, msg.Body, "restic exit code 1")
}

func TestBuildNotifyMessage_AgentOffline(t *testing.T) {
	payload := map[string]interface{}{
		"agent_name": "London-2",
	}

	msg := buildNotifyMessage("agent_offline", payload)
	assert.Equal(t, "节点离线", msg.Title)
	assert.Equal(t, LevelWarning, msg.Level)
	assert.Equal(t, "London-2", msg.AgentName)
}

func TestNotificationConfigRecord_ParseEvents(t *testing.T) {
	record := NotificationConfigRecord{
		Events: `["backup_failed","agent_offline"]`,
	}

	var evts []string
	err := json.Unmarshal([]byte(record.Events), &evts)
	require.NoError(t, err)
	assert.Equal(t, []string{"backup_failed", "agent_offline"}, evts)
}
```

- [ ] 14.13 — Run Dispatcher tests (expect fail)

```bash
go test ./internal/master/notify/... -run "TestDispatcher|TestBuild" -v
```

- [ ] 14.14 — Implement Dispatcher (`internal/master/notify/dispatcher.go`)

```go
// internal/master/notify/dispatcher.go
package notify

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"vaultfleet/internal/master/events"
)

type NotificationConfigRecord struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Config string `json:"config"`
	Events string `json:"events"`
}

type NotifierRepo interface {
	ListNotificationConfigs() ([]NotificationConfigRecord, error)
}

type NotifierFactory func(ncType, config string) (Notifier, error)

type Dispatcher struct {
	bus     *events.Bus
	repo    NotifierRepo
	factory NotifierFactory
}

func NewDispatcher(bus *events.Bus, repo NotifierRepo, factory NotifierFactory) *Dispatcher {
	return &Dispatcher{
		bus:     bus,
		repo:    repo,
		factory: factory,
	}
}

func (d *Dispatcher) Start() {
	d.bus.Subscribe(events.EventType("backup_failed"), func(e events.Event) {
		d.handleEvent("backup_failed", e)
	})
	d.bus.Subscribe(events.AgentOffline, func(e events.Event) {
		d.handleEvent("agent_offline", e)
	})
}

func (d *Dispatcher) Stop() {}

func (d *Dispatcher) handleEvent(eventName string, e events.Event) {
	configs, err := d.repo.ListNotificationConfigs()
	if err != nil {
		log.Printf("notify dispatcher: failed to load configs: %v", err)
		return
	}

	payload, ok := e.Payload.(map[string]interface{})
	if !ok {
		log.Printf("notify dispatcher: unexpected payload type for event %s", eventName)
		return
	}

	msg := buildNotifyMessage(eventName, payload)

	for _, cfg := range configs {
		var subscribedEvents []string
		if err := json.Unmarshal([]byte(cfg.Events), &subscribedEvents); err != nil {
			log.Printf("notify dispatcher: failed to parse events for config %s: %v", cfg.ID, err)
			continue
		}

		if !containsString(subscribedEvents, eventName) {
			continue
		}

		notifier, err := d.factory(cfg.Type, cfg.Config)
		if err != nil {
			log.Printf("notify dispatcher: failed to create notifier %s: %v", cfg.Type, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := notifier.Send(ctx, msg); err != nil {
			log.Printf("notify dispatcher: failed to send via %s: %v", cfg.Type, err)
		}
		cancel()
	}
}

func buildNotifyMessage(eventName string, payload map[string]interface{}) NotifyMessage {
	agentName, _ := payload["agent_name"].(string)

	switch eventName {
	case "backup_failed":
		errMsg, _ := payload["error"].(string)
		return NotifyMessage{
			Title:     "备份失败",
			Body:      errMsg,
			Level:     LevelError,
			AgentName: agentName,
			Timestamp: time.Now(),
		}
	case "agent_offline":
		return NotifyMessage{
			Title:     "节点离线",
			Body:      "节点已离线",
			Level:     LevelWarning,
			AgentName: agentName,
			Timestamp: time.Now(),
		}
	default:
		return NotifyMessage{
			Title:     eventName,
			Body:      "event triggered",
			Level:     LevelInfo,
			AgentName: agentName,
			Timestamp: time.Now(),
		}
	}
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
```

- [ ] 14.15 — Verify Dispatcher tests pass

```bash
go test ./internal/master/notify/... -v
```

- [ ] 14.16 — Write notification config CRUD API tests (`internal/master/api/notifications_test.go`)

```go
// internal/master/api/notifications_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

func setupNotificationTestRouter(t *testing.T) (*gin.Engine, *db.Database) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	r := gin.New()
	h := NewNotificationHandler(database)
	api := r.Group("/api")
	RegisterNotificationRoutes(api, h)
	return r, database
}

func TestCreateNotificationConfig_Telegram(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	body := map[string]interface{}{
		"type": "telegram",
		"config": map[string]string{
			"bot_token": "123456:ABC-DEF",
			"chat_id":   "-100999888",
		},
		"events": []string{"backup_failed", "agent_offline"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["id"])
	assert.Equal(t, "telegram", resp["type"])
	events := resp["events"].([]interface{})
	assert.Len(t, events, 2)
}

func TestCreateNotificationConfig_Webhook(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	body := map[string]interface{}{
		"type": "webhook",
		"config": map[string]string{
			"url": "https://hooks.example.com/notify",
		},
		"events": []string{"backup_failed", "agent_offline"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "webhook", resp["type"])
}

func TestCreateNotificationConfig_InvalidType(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	body := map[string]interface{}{
		"type":   "email",
		"config": map[string]string{},
		"events": []string{"backup_failed"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListNotificationConfigs(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	for _, nc := range []map[string]interface{}{
		{"type": "telegram", "config": map[string]string{"bot_token": "t1", "chat_id": "c1"}, "events": []string{"backup_failed"}},
		{"type": "webhook", "config": map[string]string{"url": "http://example.com"}, "events": []string{"agent_offline"}},
	} {
		body, _ := json.Marshal(nc)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/notifications", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var list []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 2)
}

func TestUpdateNotificationConfig(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"type":   "telegram",
		"config": map[string]string{"bot_token": "t1", "chat_id": "c1"},
		"events": []string{"backup_failed"},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	var created map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &created)
	id := created["id"].(string)

	update, _ := json.Marshal(map[string]interface{}{
		"events": []string{"backup_failed", "agent_offline"},
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/notifications/"+id, bytes.NewBuffer(update))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var updated map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &updated)
	events := updated["events"].([]interface{})
	assert.Len(t, events, 2)
}

func TestDeleteNotificationConfig(t *testing.T) {
	router, _ := setupNotificationTestRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"type":   "webhook",
		"config": map[string]string{"url": "http://example.com"},
		"events": []string{"backup_failed"},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	var created map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &created)
	id := created["id"].(string)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/notifications/"+id, nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/notifications/"+id, nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTestNotification(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	router, _ := setupNotificationTestRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"type":   "webhook",
		"config": map[string]string{"url": server.URL},
		"events": []string{"backup_failed"},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/notifications", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	var created map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &created)
	id := created["id"].(string)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/notifications/"+id+"/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called, "test notification should have been sent")
}
```

- [ ] 14.17 — Run notification API tests (expect fail)

```bash
go test ./internal/master/api/... -run "TestCreateNotificationConfig|TestListNotificationConfigs|TestUpdateNotificationConfig|TestDeleteNotificationConfig|TestTestNotification" -v
```

- [ ] 14.18 — Implement notification config CRUD API (`internal/master/api/notifications.go`)

```go
// internal/master/api/notifications.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/notify"
)

type NotificationHandler struct {
	DB *db.Database
}

func NewNotificationHandler(database *db.Database) *NotificationHandler {
	return &NotificationHandler{DB: database}
}

func RegisterNotificationRoutes(rg *gin.RouterGroup, h *NotificationHandler) {
	rg.POST("/notifications", h.Create)
	rg.GET("/notifications", h.List)
	rg.GET("/notifications/:id", h.Get)
	rg.PUT("/notifications/:id", h.Update)
	rg.DELETE("/notifications/:id", h.Delete)
	rg.POST("/notifications/:id/test", h.TestSend)
}

type CreateNotificationRequest struct {
	Type   string            `json:"type" binding:"required"`
	Config map[string]string `json:"config" binding:"required"`
	Events []string          `json:"events" binding:"required"`
}

type UpdateNotificationRequest struct {
	Config map[string]string `json:"config"`
	Events []string          `json:"events"`
}

var validNotifierTypes = map[string]bool{
	"telegram": true,
	"webhook":  true,
}

func (h *NotificationHandler) Create(c *gin.Context) {
	var req CreateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !validNotifierTypes[req.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification type, must be telegram or webhook"})
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	eventsJSON, _ := json.Marshal(req.Events)

	nc := db.NotificationConfig{
		ID:     uuid.New().String(),
		Type:   req.Type,
		Config: string(configJSON),
		Events: string(eventsJSON),
	}

	if err := h.DB.DB.Create(&nc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, h.ncToResponse(nc))
}

func (h *NotificationHandler) List(c *gin.Context) {
	var configs []db.NotificationConfig
	h.DB.DB.Find(&configs)

	result := make([]gin.H, len(configs))
	for i, nc := range configs {
		result[i] = h.ncToResponse(nc)
	}
	c.JSON(http.StatusOK, result)
}

func (h *NotificationHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var nc db.NotificationConfig
	if err := h.DB.DB.First(&nc, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, h.ncToResponse(nc))
}

func (h *NotificationHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var nc db.NotificationConfig
	if err := h.DB.DB.First(&nc, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	var req UpdateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Config != nil {
		configJSON, _ := json.Marshal(req.Config)
		nc.Config = string(configJSON)
	}
	if req.Events != nil {
		eventsJSON, _ := json.Marshal(req.Events)
		nc.Events = string(eventsJSON)
	}

	h.DB.DB.Save(&nc)
	c.JSON(http.StatusOK, h.ncToResponse(nc))
}

func (h *NotificationHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	result := h.DB.DB.Delete(&db.NotificationConfig{}, "id = ?", id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotificationHandler) TestSend(c *gin.Context) {
	id := c.Param("id")
	var nc db.NotificationConfig
	if err := h.DB.DB.First(&nc, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	notifier, err := createNotifier(nc.Type, nc.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create notifier: " + err.Error()})
		return
	}

	msg := notify.NotifyMessage{
		Title:     "测试通知",
		Body:      "这是一条 VaultFleet 测试通知，如果您收到此消息，说明通知配置正常工作。",
		Level:     notify.LevelInfo,
		AgentName: "VaultFleet-Test",
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, msg); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "send failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
}

func createNotifier(ncType, configJSON string) (notify.Notifier, error) {
	var config map[string]string
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, err
	}

	switch ncType {
	case "telegram":
		return notify.NewTelegramNotifier(notify.TelegramConfig{
			BotToken: config["bot_token"],
			ChatID:   config["chat_id"],
		}), nil
	case "webhook":
		return notify.NewWebhookNotifier(notify.WebhookConfig{
			URL: config["url"],
		}), nil
	default:
		return nil, nil
	}
}

func (h *NotificationHandler) ncToResponse(nc db.NotificationConfig) gin.H {
	var config map[string]string
	json.Unmarshal([]byte(nc.Config), &config)
	var events []string
	json.Unmarshal([]byte(nc.Events), &events)

	return gin.H{
		"id":         nc.ID,
		"type":       nc.Type,
		"config":     config,
		"events":     events,
		"created_at": nc.CreatedAt,
	}
}
```

- [ ] 14.19 — Verify all Task 14 tests pass

```bash
go test ./internal/master/notify/... -v
go test ./internal/master/api/... -run "TestCreateNotificationConfig|TestListNotificationConfigs|TestUpdateNotificationConfig|TestDeleteNotificationConfig|TestTestNotification" -v
```

- [ ] 14.20 — Commit

```bash
git add internal/master/notify/ internal/master/api/notifications.go internal/master/api/notifications_test.go
git commit -m "feat: notification system with Telegram + Webhook + event dispatcher

- Notifier interface with Send(ctx, NotifyMessage) and Type()
- TelegramNotifier: HTML-formatted messages via Bot API with httptest mocking
- WebhookNotifier: JSON POST with custom headers and timeout
- Dispatcher: subscribes to event bus, routes backup_failed/agent_offline to configs
- Notification config CRUD API: create, list, update, delete, test send
- Test endpoint sends a verification message through configured channel
- Full test coverage for all notifiers, dispatcher routing, and API endpoints"
```

---

### Task 15: Master Data Export/Restore

**Files:**
- `internal/master/backup/export.go` — zip the /data directory
- `internal/master/backup/restore.go` — detect backup.zip on startup, rollback + restore
- `internal/master/backup/export_test.go`
- `internal/master/backup/restore_test.go`
- `internal/master/api/system.go` — export download endpoint + password change
- `internal/master/api/system_test.go`

**Steps:**

- [ ] 15.1 — Write export tests (`internal/master/backup/export_test.go`)

```go
// internal/master/backup/export_test.go
package backup

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "vaultfleet.db"), []byte("fake-sqlite-data"), 0644)
	os.WriteFile(filepath.Join(dir, "master.key"), []byte("0123456789abcdef0123456789abcdef"), 0600)

	os.MkdirAll(filepath.Join(dir, "rollback"), 0755)
	os.WriteFile(filepath.Join(dir, "rollback", "20260517-030000.zip"), []byte("old-rollback"), 0644)

	return dir
}

func TestExportDataDir(t *testing.T) {
	dataDir := setupTestDataDir(t)

	buf, err := ExportDataDir(dataDir)
	require.NoError(t, err)
	assert.Greater(t, buf.Len(), 0)

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	fileNames := make(map[string]bool)
	for _, f := range reader.File {
		fileNames[f.Name] = true
	}

	assert.True(t, fileNames["vaultfleet.db"], "should contain vaultfleet.db")
	assert.True(t, fileNames["master.key"], "should contain master.key")
	assert.False(t, fileNames["rollback/20260517-030000.zip"], "should skip rollback/ dir")
	assert.False(t, fileNames["rollback/"], "should skip rollback/ dir")
}

func TestExportDataDir_SkipsBackupZip(t *testing.T) {
	dataDir := setupTestDataDir(t)
	os.WriteFile(filepath.Join(dataDir, "backup.zip"), []byte("restore-in-progress"), 0644)

	buf, err := ExportDataDir(dataDir)
	require.NoError(t, err)

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	for _, f := range reader.File {
		assert.NotEqual(t, "backup.zip", f.Name, "should skip backup.zip")
	}
}

func TestExportDataDir_EmptyDir(t *testing.T) {
	dataDir := t.TempDir()

	buf, err := ExportDataDir(dataDir)
	require.NoError(t, err)
	assert.Greater(t, buf.Len(), 0)
}

func TestExportDataDir_PreservesSubdirs(t *testing.T) {
	dataDir := t.TempDir()
	os.MkdirAll(filepath.Join(dataDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dataDir, "subdir", "nested.txt"), []byte("nested-data"), 0644)

	buf, err := ExportDataDir(dataDir)
	require.NoError(t, err)

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	found := false
	for _, f := range reader.File {
		if f.Name == "subdir/nested.txt" {
			found = true
		}
	}
	assert.True(t, found, "should contain subdir/nested.txt")
}
```

- [ ] 15.2 — Run export tests (expect fail)

```bash
go test ./internal/master/backup/... -run "TestExport" -v
```

- [ ] 15.3 — Implement export (`internal/master/backup/export.go`)

```go
// internal/master/backup/export.go
package backup

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var skipDirs = map[string]bool{
	"rollback": true,
}

var skipFiles = map[string]bool{
	"backup.zip": true,
}

func ExportDataDir(dataDir string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	defer w.Close()

	dataDir = filepath.Clean(dataDir)

	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}

		if rel == "." {
			return nil
		}

		topDir := strings.SplitN(rel, string(os.PathSeparator), 2)[0]
		if skipDirs[topDir] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if skipFiles[info.Name()] && !info.IsDir() {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("create zip header for %s: %w", rel, err)
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate

		writer, err := w.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry for %s: %w", rel, err)
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer file.Close()

		if _, err := io.Copy(writer, file); err != nil {
			return fmt.Errorf("write %s to zip: %w", rel, err)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("export data dir: %w", err)
	}

	return buf, nil
}
```

- [ ] 15.4 — Verify export tests pass

```bash
go test ./internal/master/backup/... -run "TestExport" -v
```

- [ ] 15.5 — Write restore tests (`internal/master/backup/restore_test.go`)

```go
// internal/master/backup/restore_test.go
package backup

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestBackupZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestCheckAndRestore_NoBackupZip(t *testing.T) {
	dataDir := t.TempDir()
	os.WriteFile(filepath.Join(dataDir, "vaultfleet.db"), []byte("current-data"), 0644)

	restored, err := CheckAndRestore(dataDir)
	require.NoError(t, err)
	assert.False(t, restored)

	content, _ := os.ReadFile(filepath.Join(dataDir, "vaultfleet.db"))
	assert.Equal(t, "current-data", string(content))
}

func TestCheckAndRestore_WithBackupZip(t *testing.T) {
	dataDir := t.TempDir()

	os.WriteFile(filepath.Join(dataDir, "vaultfleet.db"), []byte("old-data"), 0644)
	os.WriteFile(filepath.Join(dataDir, "master.key"), []byte("old-key-32-bytes-padded-to-32bb"), 0600)

	backupData := createTestBackupZip(t, map[string]string{
		"vaultfleet.db": "restored-data",
		"master.key":    "new-key-32-bytes-padded-to-32bb",
	})
	os.WriteFile(filepath.Join(dataDir, "backup.zip"), backupData, 0644)

	restored, err := CheckAndRestore(dataDir)
	require.NoError(t, err)
	assert.True(t, restored)

	// backup.zip should be deleted
	_, err = os.Stat(filepath.Join(dataDir, "backup.zip"))
	assert.True(t, os.IsNotExist(err))

	// Restored files should contain new content
	content, _ := os.ReadFile(filepath.Join(dataDir, "vaultfleet.db"))
	assert.Equal(t, "restored-data", string(content))

	keyContent, _ := os.ReadFile(filepath.Join(dataDir, "master.key"))
	assert.Equal(t, "new-key-32-bytes-padded-to-32bb", string(keyContent))
}

func TestCheckAndRestore_CreatesRollback(t *testing.T) {
	dataDir := t.TempDir()

	os.WriteFile(filepath.Join(dataDir, "vaultfleet.db"), []byte("pre-restore-data"), 0644)
	os.WriteFile(filepath.Join(dataDir, "master.key"), []byte("pre-restore-key-32bytes-paddddd"), 0600)

	backupData := createTestBackupZip(t, map[string]string{
		"vaultfleet.db": "restored-data",
	})
	os.WriteFile(filepath.Join(dataDir, "backup.zip"), backupData, 0644)

	restored, err := CheckAndRestore(dataDir)
	require.NoError(t, err)
	assert.True(t, restored)

	rollbackDir := filepath.Join(dataDir, "rollback")
	entries, err := os.ReadDir(rollbackDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	rollbackName := entries[0].Name()
	assert.Contains(t, rollbackName, ".zip")

	now := time.Now()
	expectedPrefix := now.Format("20060102")
	assert.Contains(t, rollbackName, expectedPrefix)
}

func TestCheckAndRestore_BackupZipWithSubdirs(t *testing.T) {
	dataDir := t.TempDir()

	backupData := createTestBackupZip(t, map[string]string{
		"vaultfleet.db":        "db-data",
		"subdir/nested.txt":    "nested-content",
		"subdir/deep/file.txt": "deep-content",
	})
	os.WriteFile(filepath.Join(dataDir, "backup.zip"), backupData, 0644)

	restored, err := CheckAndRestore(dataDir)
	require.NoError(t, err)
	assert.True(t, restored)

	content, _ := os.ReadFile(filepath.Join(dataDir, "subdir", "nested.txt"))
	assert.Equal(t, "nested-content", string(content))

	deepContent, _ := os.ReadFile(filepath.Join(dataDir, "subdir", "deep", "file.txt"))
	assert.Equal(t, "deep-content", string(deepContent))
}

func TestCheckAndRestore_InvalidZip(t *testing.T) {
	dataDir := t.TempDir()
	os.WriteFile(filepath.Join(dataDir, "backup.zip"), []byte("not-a-zip"), 0644)

	_, err := CheckAndRestore(dataDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zip")
}
```

- [ ] 15.6 — Run restore tests (expect fail)

```bash
go test ./internal/master/backup/... -run "TestCheckAndRestore" -v
```

- [ ] 15.7 — Implement restore (`internal/master/backup/restore.go`)

```go
// internal/master/backup/restore.go
package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func CheckAndRestore(dataDir string) (bool, error) {
	backupPath := filepath.Join(dataDir, "backup.zip")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return false, nil
	}

	if err := createRollback(dataDir); err != nil {
		return false, fmt.Errorf("create rollback: %w", err)
	}

	if err := extractZip(backupPath, dataDir); err != nil {
		return false, fmt.Errorf("extract backup.zip: %w", err)
	}

	if err := os.Remove(backupPath); err != nil {
		return true, fmt.Errorf("remove backup.zip: %w", err)
	}

	return true, nil
}

func createRollback(dataDir string) error {
	rollbackDir := filepath.Join(dataDir, "rollback")
	if err := os.MkdirAll(rollbackDir, 0755); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	rollbackPath := filepath.Join(rollbackDir, timestamp+".zip")

	buf, err := ExportDataDir(dataDir)
	if err != nil {
		return fmt.Errorf("export current state: %w", err)
	}

	return os.WriteFile(rollbackPath, buf.Bytes(), 0644)
}

func extractZip(zipPath, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	for _, f := range reader.File {
		target := filepath.Join(destDir, f.Name)

		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q escapes destination directory", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", f.Name, err)
		}

		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			src.Close()
			return fmt.Errorf("create file %s: %w", target, err)
		}

		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()

		if copyErr != nil {
			return fmt.Errorf("extract %s: %w", f.Name, copyErr)
		}
	}

	return nil
}
```

- [ ] 15.8 — Verify restore tests pass

```bash
go test ./internal/master/backup/... -v
```

- [ ] 15.9 — Write system API tests (`internal/master/api/system_test.go`)

```go
// internal/master/api/system_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"vaultfleet/internal/master/db"
)

func setupSystemTestRouter(t *testing.T) (*gin.Engine, *db.Database) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	r := gin.New()
	h := NewSystemHandler(database)
	api := r.Group("/api/system")
	RegisterSystemRoutes(api, h)
	return r, database
}

func TestExportEndpoint(t *testing.T) {
	router, database := setupSystemTestRouter(t)

	database.DB.Create(&db.User{Username: "admin", PasswordHash: "hash"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/system/export", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "backup.zip")
	assert.Greater(t, w.Body.Len(), 0)
}

func TestChangePassword(t *testing.T) {
	router, database := setupSystemTestRouter(t)

	oldHash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	database.DB.Create(&db.User{Username: "admin", PasswordHash: string(oldHash)})

	body, _ := json.Marshal(map[string]string{
		"current_password": "oldpassword",
		"new_password":     "newpassword123",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var user db.User
	database.DB.First(&user, "username = ?", "admin")
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("newpassword123"))
	assert.NoError(t, err, "password should be updated")
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	router, database := setupSystemTestRouter(t)

	oldHash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.DefaultCost)
	database.DB.Create(&db.User{Username: "admin", PasswordHash: string(oldHash)})

	body, _ := json.Marshal(map[string]string{
		"current_password": "wrong",
		"new_password":     "newpassword123",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestChangePassword_TooShort(t *testing.T) {
	router, database := setupSystemTestRouter(t)

	oldHash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.DefaultCost)
	database.DB.Create(&db.User{Username: "admin", PasswordHash: string(oldHash)})

	body, _ := json.Marshal(map[string]string{
		"current_password": "correct",
		"new_password":     "short",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] 15.10 — Run system API tests (expect fail)

```bash
go test ./internal/master/api/... -run "TestExport|TestChangePassword" -v
```

- [ ] 15.11 — Implement system API (`internal/master/api/system.go`)

```go
// internal/master/api/system.go
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"vaultfleet/internal/master/backup"
	"vaultfleet/internal/master/db"
)

type SystemHandler struct {
	DB *db.Database
}

func NewSystemHandler(database *db.Database) *SystemHandler {
	return &SystemHandler{DB: database}
}

func RegisterSystemRoutes(rg *gin.RouterGroup, h *SystemHandler) {
	rg.GET("/export", h.Export)
	rg.PUT("/password", h.ChangePassword)
}

func (h *SystemHandler) Export(c *gin.Context) {
	buf, err := backup.ExportDataDir(h.DB.DataDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export failed: " + err.Error()})
		return
	}

	filename := fmt.Sprintf("vaultfleet-backup-%s.zip", time.Now().Format("20060102-150405"))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

func (h *SystemHandler) ChangePassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user db.User
	if err := h.DB.DB.First(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no admin user found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	h.DB.DB.Model(&user).Update("password_hash", string(newHash))
	c.JSON(http.StatusOK, gin.H{"message": "password changed"})
}
```

- [ ] 15.12 — Verify all Task 15 tests pass

```bash
go test ./internal/master/backup/... -v
go test ./internal/master/api/... -run "TestExport|TestChangePassword" -v
```

- [ ] 15.13 — Commit

```bash
git add internal/master/backup/ internal/master/api/system.go internal/master/api/system_test.go
git commit -m "feat: master data export/restore and system settings API

- ExportDataDir: zips /data excluding rollback/ and backup.zip
- CheckAndRestore: on startup detects backup.zip, creates rollback, extracts, cleans up
- Rollback: timestamped zip of current state saved before restore
- Zip path traversal protection in extract
- GET /api/system/export: serves zip download of entire data directory
- PUT /api/system/password: change admin password with current password verification
- Full test coverage for export, restore, rollback creation, and API endpoints"
```

---

### Task 16: Master Entry Point + Router Assembly

**Files:**
- `cmd/master/main.go` — wire everything together
- `internal/master/api/router.go` — assemble all routes
- `internal/master/api/frontend.go` — serve embedded Vue SPA (placeholder for now)
- `internal/master/api/router_test.go` — smoke test
- `internal/master/api/frontend_test.go`

**Steps:**

- [ ] 16.1 — Write frontend placeholder tests (`internal/master/api/frontend_test.go`)

```go
// internal/master/api/frontend_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestFrontendPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterFrontendRoutes(r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "VaultFleet")
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestFrontendPlaceholder_AnyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterFrontendRoutes(r)

	paths := []string{"/", "/dashboard", "/agents", "/settings"}
	for _, path := range paths {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "path %s should return 200", path)
		assert.Contains(t, w.Body.String(), "VaultFleet")
	}
}
```

- [ ] 16.2 — Run frontend tests (expect fail)

```bash
go test ./internal/master/api/... -run "TestFrontendPlaceholder" -v
```

- [ ] 16.3 — Implement frontend placeholder (`internal/master/api/frontend.go`)

```go
// internal/master/api/frontend.go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const placeholderHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>VaultFleet</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
			display: flex; justify-content: center; align-items: center;
			min-height: 100vh; margin: 0;
			background: #f5f5f5; color: #333;
		}
		.container { text-align: center; }
		h1 { font-size: 2.5rem; font-weight: 300; }
		p { color: #666; margin-top: 0.5rem; }
	</style>
</head>
<body>
	<div class="container">
		<h1>VaultFleet</h1>
		<p>多客户端集中管理备份系统</p>
		<p style="font-size: 0.85rem; color: #999;">Vue 前端即将上线</p>
	</div>
</body>
</html>`

func RegisterFrontendRoutes(r *gin.Engine) {
	r.NoRoute(func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
	})
}
```

- [ ] 16.4 — Verify frontend tests pass

```bash
go test ./internal/master/api/... -run "TestFrontendPlaceholder" -v
```

- [ ] 16.5 — Write router assembly smoke test (`internal/master/api/router_test.go`)

```go
// internal/master/api/router_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/ws"
)

func TestRouterAssembly_AllRoutesRegistered(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	publicRoutes := []struct {
		method string
		path   string
		status int
	}{
		{"GET", "/api/auth/check", http.StatusOK},
	}

	for _, route := range publicRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(route.method, route.path, nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, route.status, w.Code)
		})
	}
}

func TestRouterAssembly_AuthCheckReturnsInit(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/auth/check", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.False(t, data["initialized"].(bool))
}

func TestRouterAssembly_ProtectedRoutesRequireAuth(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	database.DB.Create(&db.User{Username: "admin", PasswordHash: "hash"})

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	protectedPaths := []string{
		"/api/agents",
		"/api/notifications",
		"/api/system/export",
	}

	for _, path := range protectedPaths {
		t.Run("GET "+path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", path, nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"path %s should require auth", path)
		})
	}
}

func TestRouterAssembly_UninitializedBlocksProtected(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/agents", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "init_required", resp["error"])
}

func TestRouterAssembly_FrontendFallback(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "VaultFleet")
}

func TestRouterAssembly_EnrollIsPublic(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agent/enroll", nil)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Should NOT be 401 or 409 (enroll is public), expect 400 (bad body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] 16.6 — Run router tests (expect fail)

```bash
go test ./internal/master/api/... -run "TestRouterAssembly" -v
```

- [ ] 16.7 — Implement router assembly (`internal/master/api/router.go`)

```go
// internal/master/api/router.go
package api

import (
	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/ws"
)

type RouterConfig struct {
	Database *db.Database
	Hub      *ws.Hub
	EventBus *events.Bus
}

func NewRouter(cfg RouterConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	authHandler := NewAuthHandler(cfg.Database)
	agentHandler := NewAgentHandler(cfg.Database)
	notifHandler := NewNotificationHandler(cfg.Database)
	systemHandler := NewSystemHandler(cfg.Database)

	public := r.Group("/api")
	{
		public.GET("/auth/check", authHandler.CheckInit)
		public.POST("/auth/init", authHandler.InitSetup)
		public.POST("/auth/login", authHandler.Login)
		public.POST("/agent/enroll", agentHandler.Enroll)
	}

	r.GET("/ws/agent", func(c *gin.Context) {
		wsHandler := ws.NewHandler(cfg.Hub, cfg.EventBus,
			func(token string) (string, error) {
				var agent db.Agent
				if err := cfg.Database.DB.Where("agent_token = ?", token).First(&agent).Error; err != nil {
					return "", err
				}
				return agent.ID, nil
			},
			func(agentID string) (*interface{}, bool) {
				return nil, false
			},
		)
		wsHandler.HandleWebSocket(c)
	})

	protected := r.Group("/api")
	protected.Use(RequireInit(cfg.Database), RequireAuth(authHandler.Sessions))
	{
		protected.POST("/agents", agentHandler.Create)
		protected.GET("/agents", agentHandler.List)
		protected.GET("/agents/:id", agentHandler.Get)
		protected.DELETE("/agents/:id", agentHandler.Delete)
		protected.POST("/agents/:id/regenerate-token", agentHandler.RegenerateToken)

		RegisterNotificationRoutes(protected, notifHandler)

		system := protected.Group("/system")
		RegisterSystemRoutes(system, systemHandler)
	}

	RegisterFrontendRoutes(r)

	return r
}
```

- [ ] 16.8 — Verify router tests pass

```bash
go test ./internal/master/api/... -run "TestRouterAssembly|TestFrontendPlaceholder" -v
```

- [ ] 16.9 — Implement master entry point (`cmd/master/main.go`)

```go
// cmd/master/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vaultfleet/internal/master/api"
	"vaultfleet/internal/master/backup"
	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/notify"
	"vaultfleet/internal/master/ws"
)

func main() {
	dataDir := flag.String("data-dir", "/data", "path to data directory")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	log.Printf("VaultFleet Master starting, data-dir=%s", *dataDir)

	restored, err := backup.CheckAndRestore(*dataDir)
	if err != nil {
		log.Fatalf("restore check failed: %v", err)
	}
	if restored {
		log.Println("data restored from backup.zip")
	}

	database, err := db.New(*dataDir)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}

	hub := ws.NewHub()
	bus := events.NewBus()

	notifyDispatcher := notify.NewDispatcher(bus, &dbNotifierRepo{db: database}, func(ncType, config string) (notify.Notifier, error) {
		return createNotifierFromDB(ncType, config)
	})
	notifyDispatcher.Start()

	router := api.NewRouter(api.RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	srv := &http.Server{
		Addr:    *addr,
		Handler: router,
	}

	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
	log.Println("server stopped")
}

type dbNotifierRepo struct {
	db *db.Database
}

func (r *dbNotifierRepo) ListNotificationConfigs() ([]notify.NotificationConfigRecord, error) {
	var configs []db.NotificationConfig
	if err := r.db.DB.Find(&configs).Error; err != nil {
		return nil, err
	}

	records := make([]notify.NotificationConfigRecord, len(configs))
	for i, c := range configs {
		records[i] = notify.NotificationConfigRecord{
			ID:     c.ID,
			Type:   c.Type,
			Config: c.Config,
			Events: c.Events,
		}
	}
	return records, nil
}

func createNotifierFromDB(ncType, configJSON string) (notify.Notifier, error) {
	switch ncType {
	case "telegram":
		var config notify.TelegramConfig
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, err
		}
		return notify.NewTelegramNotifier(config), nil
	case "webhook":
		var config notify.WebhookConfig
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, err
		}
		return notify.NewWebhookNotifier(config), nil
	default:
		return nil, fmt.Errorf("unknown notifier type: %s", ncType)
	}
}
```

Note: Add `"encoding/json"` to the import block alongside the other imports.

- [ ] 16.10 — Verify build compiles

```bash
go build ./cmd/master/...
```

- [ ] 16.11 — Commit

```bash
git add cmd/master/main.go internal/master/api/router.go internal/master/api/router_test.go internal/master/api/frontend.go internal/master/api/frontend_test.go
git commit -m "feat: master entry point, router assembly, and frontend placeholder

- cmd/master/main.go: --data-dir flag, restore check, DB init, WS hub, notify dispatcher, HTTP server with graceful shutdown
- router.go: assembles all route groups (public auth/enroll, WS, protected API, frontend fallback)
- frontend.go: placeholder HTML page with VaultFleet branding (Vue SPA comes later)
- RequireInit middleware blocks protected routes until admin is created
- RequireAuth middleware validates session cookie
- Smoke tests: all routes registered, auth check, protected routes, enroll is public, frontend fallback"
```

---

### Task 17: Makefile + Docker Build

**Files:**
- `Makefile` — build targets for master, agent, all
- `build/Dockerfile` — multi-stage build for master Docker image
- `build/install.sh` — agent install script
- `docker-compose.yml`

**Steps:**

- [ ] 17.1 — Create `Makefile`

```makefile
# Makefile
.PHONY: build-master build-agent build-all test docker-build clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build-master:
	CGO_ENABLED=1 go build $(LDFLAGS) -o bin/vaultfleet-master ./cmd/master

build-agent:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/vaultfleet-agent ./cmd/agent

build-all: build-master build-agent

test:
	go test ./... -v -race -count=1

docker-build:
	docker build -t vaultfleet/master:$(VERSION) -t vaultfleet/master:latest -f build/Dockerfile .

clean:
	rm -rf bin/
	go clean -cache -testcache
```

- [ ] 17.2 — Create `build/Dockerfile`

```dockerfile
# build/Dockerfile
# Stage 1: Build
FROM golang:1.22-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /bin/vaultfleet-master ./cmd/master

# Stage 2: Runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata libc6-compat

COPY --from=builder /bin/vaultfleet-master /usr/local/bin/vaultfleet-master

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["vaultfleet-master"]
CMD ["--data-dir", "/data", "--addr", ":8080"]
```

- [ ] 17.3 — Create `build/install.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

MASTER_URL=""
ENROLL_TOKEN=""
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/vaultfleet"

usage() {
    echo "Usage: $0 --server <master-url> --token <enroll-token>"
    exit 1
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --server) MASTER_URL="$2"; shift 2 ;;
        --token) ENROLL_TOKEN="$2"; shift 2 ;;
        *) usage ;;
    esac
done

if [[ -z "$MASTER_URL" || -z "$ENROLL_TOKEN" ]]; then
    usage
fi

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [[ "$OS" != "linux" ]]; then
    echo "Unsupported OS: $OS (only Linux is supported)"
    exit 1
fi

echo "==> Installing VaultFleet Agent (${OS}/${ARCH})"

echo "==> Downloading vaultfleet-agent..."
curl -fsSL "${MASTER_URL}/download/agent-${OS}-${ARCH}" -o "${INSTALL_DIR}/vaultfleet-agent"
chmod +x "${INSTALL_DIR}/vaultfleet-agent"

if ! command -v restic &>/dev/null; then
    echo "==> Downloading restic..."
    RESTIC_VERSION="0.17.3"
    curl -fsSL "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_${OS}_${ARCH}.bz2" | bunzip2 > "${INSTALL_DIR}/restic"
    chmod +x "${INSTALL_DIR}/restic"
fi

if ! command -v rclone &>/dev/null; then
    echo "==> Downloading rclone..."
    curl -fsSL https://rclone.org/install.sh | bash -s beta 2>/dev/null || {
        echo "Warning: rclone auto-install failed, please install rclone manually"
    }
fi

echo "==> Creating config directory..."
mkdir -p "$CONFIG_DIR"
chmod 700 "$CONFIG_DIR"

echo "==> Enrolling agent with master..."
"${INSTALL_DIR}/vaultfleet-agent" \
    --server "$MASTER_URL" \
    --token "$ENROLL_TOKEN" \
    --config "${CONFIG_DIR}/agent.yaml"

echo "==> Creating systemd service..."
cat > /etc/systemd/system/vaultfleet-agent.service <<EOF
[Unit]
Description=VaultFleet Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/vaultfleet-agent --config ${CONFIG_DIR}/agent.yaml
Restart=always
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

echo "==> Starting vaultfleet-agent service..."
systemctl daemon-reload
systemctl enable vaultfleet-agent
systemctl start vaultfleet-agent

echo "==> VaultFleet Agent installed and running!"
echo "    Config: ${CONFIG_DIR}/agent.yaml"
echo "    Service: systemctl status vaultfleet-agent"
```

- [ ] 17.4 — Create `docker-compose.yml`

```yaml
# docker-compose.yml
services:
  vaultfleet:
    image: vaultfleet/master:latest
    build:
      context: .
      dockerfile: build/Dockerfile
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    restart: unless-stopped
```

- [ ] 17.5 — Verify Makefile works

```bash
make test
make build-all
ls -la bin/
```

- [ ] 17.6 — Verify install.sh is executable

```bash
chmod +x build/install.sh
bash -n build/install.sh
```

- [ ] 17.7 — Commit

```bash
git add Makefile build/Dockerfile build/install.sh docker-compose.yml
git commit -m "feat: add Makefile, Dockerfile, install script, and docker-compose

- Makefile: build-master (CGO=1), build-agent (CGO=0), test, docker-build, clean
- Dockerfile: multi-stage build with golang:1.22-bookworm → alpine:3.20
- install.sh: detect arch, download agent+restic+rclone, enroll, create systemd service
- docker-compose.yml: single service with port 8080 and ./data:/data volume"
```

---

### Task 18: Integration Smoke Test

**Files:**
- `test/integration_test.go` — full stack smoke test

**Steps:**

- [ ] 18.1 — Write integration smoke test (`test/integration_test.go`)

```go
// test/integration_test.go
package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/api"
	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/ws"
)

func setupFullServer(t *testing.T) *httptest.Server {
	t.Helper()

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	bus := events.NewBus()

	router := api.NewRouter(api.RouterConfig{
		Database: database,
		Hub:      hub,
		EventBus: bus,
	})

	return httptest.NewServer(router)
}

func doJSON(t *testing.T, server *httptest.Server, method, path string, body interface{}, cookies []*http.Cookie) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, server.URL+path, bodyReader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func parseBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &result))
	return result
}

func getSessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	return nil
}

func TestIntegration_FullFlow(t *testing.T) {
	server := setupFullServer(t)
	defer server.Close()

	// Step 1: Check system is not initialized
	resp := doJSON(t, server, "GET", "/api/auth/check", nil, nil)
	result := parseBody(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	data := result["data"].(map[string]interface{})
	assert.False(t, data["initialized"].(bool))

	// Step 2: Protected routes should return 409 (init_required)
	resp = doJSON(t, server, "GET", "/api/agents", nil, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	result = parseBody(t, resp)
	assert.Equal(t, "init_required", result["error"])

	// Step 3: Initialize admin user
	resp = doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "supersecret123",
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	sessionCookie := getSessionCookie(resp)
	require.NotNil(t, sessionCookie, "should receive session cookie")
	parseBody(t, resp)

	// Step 4: Verify system is now initialized
	resp = doJSON(t, server, "GET", "/api/auth/check", nil, nil)
	result = parseBody(t, resp)
	data = result["data"].(map[string]interface{})
	assert.True(t, data["initialized"].(bool))

	// Step 5: Access protected routes with session
	resp = doJSON(t, server, "GET", "/api/agents", nil, []*http.Cookie{sessionCookie})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	agents := result["data"].([]interface{})
	assert.Len(t, agents, 0)

	// Step 6: Create an agent
	resp = doJSON(t, server, "POST", "/api/agents", map[string]string{
		"name": "Tokyo-1",
	}, []*http.Cookie{sessionCookie})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	agentData := result["data"].(map[string]interface{})
	agentID := agentData["id"].(string)
	enrollToken := agentData["enroll_token"].(string)
	assert.NotEmpty(t, agentID)
	assert.Contains(t, enrollToken, "ek_")

	// Step 7: Verify agent appears in list
	resp = doJSON(t, server, "GET", "/api/agents", nil, []*http.Cookie{sessionCookie})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	agents = result["data"].([]interface{})
	assert.Len(t, agents, 1)

	// Step 8: Get agent details
	resp = doJSON(t, server, "GET", "/api/agents/"+agentID, nil, []*http.Cookie{sessionCookie})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	agentDetail := result["data"].(map[string]interface{})
	assert.Equal(t, "Tokyo-1", agentDetail["name"])

	// Step 9: Simulate agent enrollment
	resp = doJSON(t, server, "POST", "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
		"system_info":  `{"os":"linux","arch":"amd64"}`,
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	enrollData := result["data"].(map[string]interface{})
	agentToken := enrollData["agent_token"].(string)
	assert.Contains(t, agentToken, "ak_")
	assert.Equal(t, agentID, enrollData["agent_id"])

	// Step 10: Verify enrollment token is consumed (re-enrollment fails)
	resp = doJSON(t, server, "POST", "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	parseBody(t, resp)

	// Step 11: Verify auth check still works
	resp = doJSON(t, server, "GET", "/api/auth/check", nil, nil)
	result = parseBody(t, resp)
	data = result["data"].(map[string]interface{})
	assert.True(t, data["initialized"].(bool))

	// Step 12: Login with the admin account
	resp = doJSON(t, server, "POST", "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "supersecret123",
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	newCookie := getSessionCookie(resp)
	require.NotNil(t, newCookie)
	parseBody(t, resp)

	// Step 13: Frontend fallback serves HTML
	req, _ := http.NewRequest("GET", server.URL+"/dashboard", nil)
	client := &http.Client{}
	resp2, err := client.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	bodyBytes, _ := io.ReadAll(resp2.Body)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Contains(t, string(bodyBytes), "VaultFleet")
}

func TestIntegration_LoginFails(t *testing.T) {
	server := setupFullServer(t)
	defer server.Close()

	// Init first
	resp := doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "correct_password",
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	parseBody(t, resp)

	// Login with wrong password
	resp = doJSON(t, server, "POST", "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "wrong_password",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	parseBody(t, resp)
}

func TestIntegration_DoubleInitBlocked(t *testing.T) {
	server := setupFullServer(t)
	defer server.Close()

	resp := doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	parseBody(t, resp)

	resp = doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin2",
		"password": "password456",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	result := parseBody(t, resp)
	assert.Equal(t, "system already initialized", result["error"])
}

func TestIntegration_InvalidSessionRejected(t *testing.T) {
	server := setupFullServer(t)
	defer server.Close()

	// Init first
	resp := doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	parseBody(t, resp)

	// Use fake session cookie
	fakeCookie := &http.Cookie{Name: "session", Value: "ss_fake_invalid_token"}
	resp = doJSON(t, server, "GET", "/api/agents", nil, []*http.Cookie{fakeCookie})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	parseBody(t, resp)
}

func TestIntegration_RegenerateTokenAndReEnroll(t *testing.T) {
	server := setupFullServer(t)
	defer server.Close()

	// Init + login
	resp := doJSON(t, server, "POST", "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	sessionCookie := getSessionCookie(resp)
	parseBody(t, resp)

	// Create agent
	resp = doJSON(t, server, "POST", "/api/agents", map[string]string{
		"name": "Singapore-1",
	}, []*http.Cookie{sessionCookie})
	result := parseBody(t, resp)
	agentData := result["data"].(map[string]interface{})
	agentID := agentData["id"].(string)
	enrollToken := agentData["enroll_token"].(string)

	// Enroll
	resp = doJSON(t, server, "POST", "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	parseBody(t, resp)

	// Regenerate token
	resp = doJSON(t, server, "POST", "/api/agents/"+agentID+"/regenerate-token",
		nil, []*http.Cookie{sessionCookie})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	regenData := result["data"].(map[string]interface{})
	newToken := regenData["enroll_token"].(string)
	assert.NotEqual(t, enrollToken, newToken)

	// Re-enroll with new token
	resp = doJSON(t, server, "POST", "/api/agent/enroll", map[string]string{
		"enroll_token": newToken,
		"system_info":  `{"os":"linux","arch":"arm64"}`,
	}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	result = parseBody(t, resp)
	reEnrollData := result["data"].(map[string]interface{})
	assert.Equal(t, agentID, reEnrollData["agent_id"])
	assert.Contains(t, reEnrollData["agent_token"].(string), "ak_")
}
```

- [ ] 18.2 — Run integration tests (expect fail initially if router not wired)

```bash
go test ./test/... -v -count=1
```

- [ ] 18.3 — Fix any compilation or routing issues revealed by integration test

- [ ] 18.4 — Verify all integration tests pass

```bash
go test ./test/... -v -count=1 -run "TestIntegration"
```

- [ ] 18.5 — Run full project test suite

```bash
go test ./... -v -race -count=1
```

- [ ] 18.6 — Commit

```bash
git add test/integration_test.go
git commit -m "test: add full-stack integration smoke test

- Boots real master server with in-memory SQLite via httptest
- Tests complete lifecycle: check init → init wizard → login → create agent → enroll → verify token consumed
- Validates protected routes block without auth (401) and before init (409)
- Tests token regeneration and re-enrollment flow
- Verifies frontend fallback serves placeholder HTML
- Tests invalid session rejection and double-init blocking
- All 5 integration tests validate end-to-end route assembly works correctly"
```
