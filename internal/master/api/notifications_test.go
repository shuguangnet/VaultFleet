package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/notify"
)

type notificationAPISetup struct {
	database *db.Database
	handler  *NotificationHandler
	router   *gin.Engine
}

func setupNotificationAPI(t *testing.T) notificationAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	handler := NewNotificationHandler(database)
	router := gin.New()
	RegisterNotificationRoutes(router.Group("/api"), handler)

	return notificationAPISetup{
		database: database,
		handler:  handler,
		router:   router,
	}
}

func TestCreateNotificationConfig(t *testing.T) {
	setup := setupNotificationAPI(t)

	w := postAnyJSON(t, setup.router, "/api/notifications", map[string]any{
		"type": "webhook",
		"config": map[string]any{
			"url": "https://hooks.example.test/notify",
			"headers": map[string]any{
				"Authorization": "Bearer secret",
				"X-Nested":      map[string]any{"value": "kept"},
			},
		},
		"events": []string{"backup_failed", "agent_offline"},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, "webhook", body["type"])
	assertJSONList(t, body["events"], []string{"backup_failed", "agent_offline"})
	config := requireMap(t, body["config"])
	assert.Equal(t, "https://hooks.example.test/notify", config["url"])
	headers := requireMap(t, config["headers"])
	assert.Equal(t, "Bearer secret", headers["Authorization"])
	assert.Equal(t, map[string]any{"value": "kept"}, requireMap(t, headers["X-Nested"]))

	var stored db.NotificationConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.JSONEq(t, `["backup_failed","agent_offline"]`, stored.Events)
	assert.JSONEq(t, `{"url":"https://hooks.example.test/notify","headers":{"Authorization":"Bearer secret","X-Nested":{"value":"kept"}}}`, stored.Config)
}

func TestCreateNotificationConfigValidatesTypeAndRequiredConfig(t *testing.T) {
	setup := setupNotificationAPI(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "unknown type",
			body: map[string]any{
				"type":   "email",
				"config": map[string]any{},
				"events": []string{"backup_failed"},
			},
		},
		{
			name: "telegram missing chat id",
			body: map[string]any{
				"type":   "telegram",
				"config": map[string]any{"bot_token": "token"},
				"events": []string{"backup_failed"},
			},
		},
		{
			name: "webhook missing url",
			body: map[string]any{
				"type":   "webhook",
				"config": map[string]any{"headers": map[string]any{"X-Test": "value"}},
				"events": []string{"agent_offline"},
			},
		},
		{
			name: "events missing",
			body: map[string]any{
				"type":   "webhook",
				"config": map[string]any{"url": "https://hooks.example.test"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := postAnyJSON(t, setup.router, "/api/notifications", tt.body)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestListAndGetNotificationConfigsReturnEventArrays(t *testing.T) {
	setup := setupNotificationAPI(t)
	first := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://first.example.test"}, []string{"backup_failed"})
	second := createNotificationConfigViaAPI(t, setup.router, "telegram", map[string]any{"bot_token": "token", "chat_id": "chat"}, []string{"agent_offline"})

	w := getJSON(t, setup.router, "/api/notifications")
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	parseJSONInto(t, w, &list)
	require.Len(t, list, 2)
	seen := map[string]map[string]any{}
	for _, item := range list {
		seen[item["id"].(string)] = item
	}
	assertJSONList(t, seen[first["id"].(string)]["events"], []string{"backup_failed"})
	assertJSONList(t, seen[second["id"].(string)]["events"], []string{"agent_offline"})

	w = getJSON(t, setup.router, "/api/notifications/"+first["id"].(string))
	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, first["id"], body["id"])
	assert.Equal(t, "webhook", body["type"])
	assertJSONList(t, body["events"], []string{"backup_failed"})
}

func TestGetNotificationConfigNotFound(t *testing.T) {
	setup := setupNotificationAPI(t)

	w := getJSON(t, setup.router, "/api/notifications/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateNotificationConfig(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://old.example.test"}, []string{"backup_failed"})
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/notifications/"+id, map[string]any{
		"type": "telegram",
		"config": map[string]any{
			"bot_token": "new-token",
			"chat_id":   "new-chat",
		},
		"events": []string{"agent_offline"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, id, body["id"])
	assert.Equal(t, "telegram", body["type"])
	assertJSONList(t, body["events"], []string{"agent_offline"})
	config := requireMap(t, body["config"])
	assert.Equal(t, "new-token", config["bot_token"])
	assert.Equal(t, "new-chat", config["chat_id"])

	var stored db.NotificationConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.Equal(t, "telegram", stored.Type)
	assert.JSONEq(t, `{"bot_token":"new-token","chat_id":"new-chat"}`, stored.Config)
	assert.JSONEq(t, `["agent_offline"]`, stored.Events)
}

func TestUpdateNotificationConfigCanUpdateOnlyEvents(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://hooks.example.test"}, []string{"backup_failed"})
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/notifications/"+id, map[string]any{
		"events": []string{"backup_failed", "agent_offline"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "webhook", body["type"])
	assertJSONList(t, body["events"], []string{"backup_failed", "agent_offline"})
	config := requireMap(t, body["config"])
	assert.Equal(t, "https://hooks.example.test", config["url"])
}

func TestUpdateNotificationConfigValidationAndNotFound(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://hooks.example.test"}, []string{"backup_failed"})

	w := putJSON(t, setup.router, "/api/notifications/"+created["id"].(string), map[string]any{
		"type":   "webhook",
		"config": map[string]any{},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = putJSON(t, setup.router, "/api/notifications/missing", map[string]any{"events": []string{"agent_offline"}})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteNotificationConfig(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://hooks.example.test"}, []string{"backup_failed"})
	id := created["id"].(string)

	w := deleteJSON(t, setup.router, "/api/notifications/"+id)
	require.Equal(t, http.StatusNoContent, w.Code)

	w = getJSON(t, setup.router, "/api/notifications/"+id)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteNotificationConfigNotFound(t *testing.T) {
	setup := setupNotificationAPI(t)

	w := deleteJSON(t, setup.router, "/api/notifications/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestTestNotificationConfigSendsSampleNotification(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://hooks.example.test"}, []string{"backup_failed"})
	recorder := &apiRecordingNotifier{}
	setup.handler.notifierFactory = func(notificationType string, raw json.RawMessage) (notify.Notifier, error) {
		assert.Equal(t, "webhook", notificationType)
		assert.JSONEq(t, `{"url":"https://hooks.example.test"}`, string(raw))
		return recorder, nil
	}

	w := postAnyJSON(t, setup.router, "/api/notifications/"+created["id"].(string)+"/test", map[string]any{})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	require.Len(t, recorder.sent, 1)
	msg := recorder.sent[0]
	assert.Equal(t, "Test Notification", msg.Title)
	assert.Equal(t, notify.LevelInfo, msg.Level)
	assert.Equal(t, "VaultFleet", msg.AgentName)
	assert.NotEmpty(t, msg.Body)
}

func TestTestNotificationConfigReturnsSendAndNotFoundErrors(t *testing.T) {
	setup := setupNotificationAPI(t)
	created := createNotificationConfigViaAPI(t, setup.router, "webhook", map[string]any{"url": "https://hooks.example.test"}, []string{"backup_failed"})
	setup.handler.notifierFactory = func(string, json.RawMessage) (notify.Notifier, error) {
		return &apiRecordingNotifier{err: assert.AnError}, nil
	}

	w := postAnyJSON(t, setup.router, "/api/notifications/"+created["id"].(string)+"/test", map[string]any{})
	require.Equal(t, http.StatusBadGateway, w.Code)

	w = postAnyJSON(t, setup.router, "/api/notifications/missing/test", map[string]any{})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func createNotificationConfigViaAPI(t *testing.T, router http.Handler, notificationType string, config map[string]any, events []string) map[string]any {
	t.Helper()

	w := postAnyJSON(t, router, "/api/notifications", map[string]any{
		"type":   notificationType,
		"config": config,
		"events": events,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return parseJSON(t, w)
}

type apiRecordingNotifier struct {
	sent []notify.NotifyMessage
	err  error
}

func (n *apiRecordingNotifier) Send(_ context.Context, msg notify.NotifyMessage) error {
	n.sent = append(n.sent, msg)
	return n.err
}

func (n *apiRecordingNotifier) Type() string {
	return "recording"
}
