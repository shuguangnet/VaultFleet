package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/ws"
	"vaultfleet/pkg/protocol"
)

func TestRouterAssemblyAuthCheckUninitialized(t *testing.T) {
	setup := setupRouterAssembly(t)

	w := routerAssemblyRequest(setup.router, http.MethodGet, "/api/auth/check", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	require.Equal(t, true, body["ok"])
	data := requireMap(t, body["data"])
	assert.Equal(t, false, data["initialized"])
}

func TestRouterAssemblyProtectedRoutesRequireInitBeforeAuth(t *testing.T) {
	setup := setupRouterAssembly(t)

	w := routerAssemblyRequest(setup.router, http.MethodGet, "/api/agents", nil)

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "init_required", body["error"])
}

func TestRouterAssemblyProtectedRoutesRequireAuthOnceInitialized(t *testing.T) {
	setup := setupRouterAssembly(t)
	createRouterAssemblyUser(t, setup.database)

	for _, path := range []string{"/api/agents", "/api/notifications", "/api/system/export"} {
		t.Run(path, func(t *testing.T) {
			w := routerAssemblyRequest(setup.router, http.MethodGet, path, nil)

			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
}

func TestRouterAssemblySessionCookieAccessesProtectedRoute(t *testing.T) {
	setup := setupRouterAssembly(t)

	initResponse := postJSON(t, setup.router, "/api/auth/init", map[string]string{
		"username": "admin",
		"password": "secret123",
	})
	require.Equal(t, http.StatusOK, initResponse.Code, initResponse.Body.String())
	cookie := getSessionCookie(t, initResponse)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	setup.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestRouterAssemblyFrontendFallback(t *testing.T) {
	setup := setupRouterAssembly(t)

	w := routerAssemblyRequest(setup.router, http.MethodGet, "/dashboard", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "VaultFleet")
}

func TestRouterAssemblyMissingAPIRouteDoesNotFallThroughToFrontend(t *testing.T) {
	setup := setupRouterAssembly(t)

	w := routerAssemblyRequest(setup.router, http.MethodGet, "/api/not-a-route", nil)

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.NotContains(t, w.Header().Get("Content-Type"), "text/html")
	assert.NotContains(t, w.Body.String(), "VaultFleet")
}

func TestRouterAssemblyPublicAgentEnrollIsNotBlockedByAuthOrInit(t *testing.T) {
	setup := setupRouterAssembly(t)

	w := routerAssemblyRequest(setup.router, http.MethodPost, "/api/agent/enroll", bytes.NewReader([]byte(`{}`)))

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	assert.NotEqual(t, http.StatusConflict, w.Code)
}

func TestRouterAssemblyAgentWebSocketUsesInjectedHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRouterAssemblyDatabase(t)
	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      ws.NewHub(),
		EventBus: events.NewBus(),
		AgentWebSocket: func(c *gin.Context) {
			c.JSON(http.StatusTeapot, gin.H{"ok": true, "source": "injected"})
		},
	})

	w := routerAssemblyRequest(router, http.MethodGet, "/ws/agent", nil)

	require.Equal(t, http.StatusTeapot, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "injected", body["source"])
}

func TestNewRouterPanicsWithClearMessageWhenDatabaseMissing(t *testing.T) {
	require.PanicsWithValue(t, "router database is required", func() {
		NewRouter(RouterConfig{})
	})
}

func TestCurrentPolicyLookupNoPolicyReturnsFalse(t *testing.T) {
	database := newRouterAssemblyDatabase(t)

	msg, ok := CurrentPolicyLookup(database)("agent-1")

	assert.False(t, ok)
	assert.Nil(t, msg)
}

func TestCurrentPolicyLookupSyncedPolicyReturnsFalse(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent, storage := createRouterAssemblyPolicyFixtures(t, database)
	createStorageTestPolicy(t, database, agent.ID, storage.ID, true)

	msg, ok := CurrentPolicyLookup(database)(agent.ID)

	assert.False(t, ok)
	assert.Nil(t, msg)
}

func TestCurrentPolicyLookupUnsyncedPolicyReturnsPolicyPushWithDecryptedCredentials(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent, storage := createRouterAssemblyPolicyFixtures(t, database)
	createStorageTestPolicy(t, database, agent.ID, storage.ID, false)

	msg, ok := CurrentPolicyLookup(database)(agent.ID)

	require.True(t, ok)
	require.NotNil(t, msg)
	assert.Equal(t, protocol.TypePolicyPush, msg.Type)

	payload, err := protocol.ParsePayload[protocol.PolicyPushPayload](msg)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, payload.AgentID)
	assert.Equal(t, "restic-password", payload.ResticPassword)
	assert.Equal(t, "s3", payload.Storage.RcloneType)
	assert.Equal(t, "vaultfleet/"+agent.ID, payload.Storage.RepoPath)
	assert.Equal(t, map[string]string{
		"provider":          "Cloudflare",
		"access_key_id":     "AKID123",
		"secret_access_key": "SECRET456",
	}, payload.Storage.RcloneConfig)
	assert.Equal(t, []string{"/etc"}, payload.BackupDirs)
	assert.Equal(t, protocol.RetentionPolicy{KeepLast: 3}, payload.Retention)
}

func TestCurrentPolicyLookupRejectsNonStringRcloneConfigValues(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent := createStorageTestAgent(t, database, "Tokyo-1")
	storage := db.StorageConfig{
		Name:       "Cloudflare R2",
		RcloneType: "s3",
		RcloneConfig: mustEncryptMap(t, database, map[string]any{
			"provider":   "Cloudflare",
			"chunk_size": 123,
		}),
	}
	require.NoError(t, database.DB.Create(&storage).Error)
	createStorageTestPolicy(t, database, agent.ID, storage.ID, false)

	msg, ok := CurrentPolicyLookup(database)(agent.ID)

	assert.False(t, ok)
	assert.Nil(t, msg)
}

func TestPolicyAckProcessorSuccessfulAckMarksNewestUnsyncedPolicySynced(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent, storage := createRouterAssemblyPolicyFixtures(t, database)
	older := createStorageTestPolicy(t, database, agent.ID, storage.ID, false)
	newer := createStorageTestPolicy(t, database, agent.ID, storage.ID, false)
	require.NoError(t, database.DB.Model(&older).Update("updated_at", time.Now().Add(-time.Hour)).Error)
	require.NoError(t, database.DB.Model(&newer).Update("updated_at", time.Now()).Error)

	msg := policyAckMessage(t, protocol.PolicyAckPayload{AgentID: agent.ID, Success: true})

	require.NoError(t, NewPolicyAckProcessor(database)(agent.ID, *msg))

	var storedOlder db.BackupPolicy
	require.NoError(t, database.DB.First(&storedOlder, "id = ?", older.ID).Error)
	assert.False(t, storedOlder.Synced)
	var storedNewer db.BackupPolicy
	require.NoError(t, database.DB.First(&storedNewer, "id = ?", newer.ID).Error)
	assert.True(t, storedNewer.Synced)
}

func TestPolicyAckProcessorFailedAckLeavesUnsyncedPolicyUnsynced(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent, storage := createRouterAssemblyPolicyFixtures(t, database)
	policy := createStorageTestPolicy(t, database, agent.ID, storage.ID, false)

	msg := policyAckMessage(t, protocol.PolicyAckPayload{AgentID: agent.ID, Success: false, Error: "rejected"})

	require.NoError(t, NewPolicyAckProcessor(database)(agent.ID, *msg))

	var stored db.BackupPolicy
	require.NoError(t, database.DB.First(&stored, "id = ?", policy.ID).Error)
	assert.False(t, stored.Synced)
}

func TestPolicyAckProcessorUsesAuthenticatedAgentIDOverPayloadAgentID(t *testing.T) {
	database := newRouterAssemblyDatabase(t)
	agent, storage := createRouterAssemblyPolicyFixtures(t, database)
	otherAgent := createStorageTestAgent(t, database, "Osaka-1")
	policy := createStorageTestPolicy(t, database, agent.ID, storage.ID, false)
	otherPolicy := createStorageTestPolicy(t, database, otherAgent.ID, storage.ID, false)

	msg := policyAckMessage(t, protocol.PolicyAckPayload{AgentID: otherAgent.ID, Success: true})

	require.NoError(t, NewPolicyAckProcessor(database)(agent.ID, *msg))

	var stored db.BackupPolicy
	require.NoError(t, database.DB.First(&stored, "id = ?", policy.ID).Error)
	assert.True(t, stored.Synced)
	var storedOther db.BackupPolicy
	require.NoError(t, database.DB.First(&storedOther, "id = ?", otherPolicy.ID).Error)
	assert.False(t, storedOther.Synced)
}

type routerAssemblySetup struct {
	database *db.Database
	router   *gin.Engine
}

func setupRouterAssembly(t *testing.T) routerAssemblySetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	router := NewRouter(RouterConfig{
		Database: database,
		Hub:      ws.NewHub(),
		EventBus: events.NewBus(),
	})

	return routerAssemblySetup{
		database: database,
		router:   router,
	}
}

func routerAssemblyRequest(router http.Handler, method string, path string, body *bytes.Reader) *httptest.ResponseRecorder {
	if body == nil {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if body.Len() > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func createRouterAssemblyUser(t *testing.T, database *db.Database) {
	t.Helper()

	require.NoError(t, database.DB.Create(&db.User{
		Username:     "admin",
		PasswordHash: "hashed-password",
	}).Error)
}

func newRouterAssemblyDatabase(t *testing.T) *db.Database {
	t.Helper()

	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	return database
}

func createRouterAssemblyPolicyFixtures(t *testing.T, database *db.Database) (db.Agent, db.StorageConfig) {
	t.Helper()

	agent := createStorageTestAgent(t, database, "Tokyo-1")
	storage := db.StorageConfig{
		Name:       "Cloudflare R2",
		RcloneType: "s3",
		RcloneConfig: mustEncryptMap(t, database, map[string]any{
			"provider":          "Cloudflare",
			"access_key_id":     "AKID123",
			"secret_access_key": "SECRET456",
		}),
	}
	require.NoError(t, database.DB.Create(&storage).Error)
	return agent, storage
}

func policyAckMessage(t *testing.T, payload protocol.PolicyAckPayload) *protocol.Message {
	t.Helper()

	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return &protocol.Message{
		Type:    protocol.TypePolicyAck,
		ID:      "policy-push-message-id",
		Payload: raw,
	}
}
