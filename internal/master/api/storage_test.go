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
)

type testConfigSetup struct {
	database *db.Database
	bus      *events.Bus
	router   *gin.Engine
}

func setupTestConfigAPI(t *testing.T) testConfigSetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	bus := events.NewBus()
	handler := NewConfigHandler(database)
	handler.EventBus = bus
	router := gin.New()
	api := router.Group("/api")
	RegisterStorageRoutes(api, handler)

	return testConfigSetup{
		database: database,
		bus:      bus,
		router:   router,
	}
}

func TestCreateStorageConfig(t *testing.T) {
	setup := setupTestConfigAPI(t)

	w := postAnyJSON(t, setup.router, "/api/storage", map[string]any{
		"name":        "Cloudflare R2",
		"rclone_type": "s3",
		"rclone_config": map[string]any{
			"provider":          "Cloudflare",
			"access_key_id":     "AKID123",
			"secret_access_key": "SECRET456",
			"endpoint":          "https://example.r2.cloudflarestorage.com",
		},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, "Cloudflare R2", body["name"])
	assert.Equal(t, "s3", body["rclone_type"])
	config := requireMap(t, body["rclone_config"])
	assert.Equal(t, "Cloudflare", config["provider"])
	assert.Equal(t, redactedSecretValue, config["secret_access_key"])
	assert.Equal(t, redactedSecretValue, config["access_key_id"])
	assert.Equal(t, "https://example.r2.cloudflarestorage.com", config["endpoint"])

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.NotContains(t, stored.RcloneConfig, "SECRET456")
	assert.NotContains(t, stored.RcloneConfig, "AKID123")
	assert.NotEqual(t, "", stored.RcloneConfig)
}

func TestStorageResponsesRedactSecretFields(t *testing.T) {
	setup := setupTestConfigAPI(t)
	created := createStorageConfig(t, setup.router, "Cloudflare R2", map[string]any{
		"provider":          "Cloudflare",
		"access_key_id":     "AKID123",
		"secret_access_key": "SECRET456",
		"secret":            "generic-secret",
		"password":          "pw",
		"pass":              "pass-value",
		"token":             "token-value",
		"client_secret":     "client-secret-value",
		"endpoint":          "https://example.r2.cloudflarestorage.com",
	})
	id := created["id"].(string)

	w := getJSON(t, setup.router, "/api/storage/"+id)
	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	config := requireMap(t, body["rclone_config"])
	assert.Equal(t, "Cloudflare", config["provider"])
	assert.Equal(t, "https://example.r2.cloudflarestorage.com", config["endpoint"])
	for _, key := range []string{"access_key_id", "secret_access_key", "secret", "password", "pass", "token", "client_secret"} {
		assert.Equal(t, redactedSecretValue, config[key], key)
	}

	w = getJSON(t, setup.router, "/api/storage")
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	parseJSONInto(t, w, &list)
	require.Len(t, list, 1)
	listConfig := requireMap(t, list[0]["rclone_config"])
	assert.Equal(t, redactedSecretValue, listConfig["secret_access_key"])
	assert.Equal(t, "Cloudflare", listConfig["provider"])

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.NotContains(t, stored.RcloneConfig, "SECRET456")
	assert.NotContains(t, stored.RcloneConfig, "AKID123")

	plaintext, err := db.Decrypt(stored.RcloneConfig, setup.database.MasterKey)
	require.NoError(t, err)
	assert.Contains(t, plaintext, "SECRET456")
	assert.Contains(t, plaintext, "AKID123")
}

func TestCreateStorageConfigValidatesRequiredFields(t *testing.T) {
	setup := setupTestConfigAPI(t)

	w := postAnyJSON(t, setup.router, "/api/storage", map[string]any{
		"name":        "Missing config",
		"rclone_type": "s3",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListStorageConfigs(t *testing.T) {
	setup := setupTestConfigAPI(t)

	createStorageConfig(t, setup.router, "Storage A", map[string]any{"key": "a"})
	createStorageConfig(t, setup.router, "Storage B", map[string]any{"key": "b"})

	w := getJSON(t, setup.router, "/api/storage")

	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	parseJSONInto(t, w, &list)
	require.Len(t, list, 2)

	configsByName := map[string]map[string]any{}
	for _, item := range list {
		configsByName[item["name"].(string)] = requireMap(t, item["rclone_config"])
	}
	assert.Equal(t, "a", configsByName["Storage A"]["key"])
	assert.Equal(t, "b", configsByName["Storage B"]["key"])
}

func TestGetStorageConfig(t *testing.T) {
	setup := setupTestConfigAPI(t)
	created := createStorageConfig(t, setup.router, "Primary Storage", map[string]any{"secret": "value"})

	w := getJSON(t, setup.router, "/api/storage/"+created["id"].(string))

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, created["id"], body["id"])
	assert.Equal(t, "Primary Storage", body["name"])
	config := requireMap(t, body["rclone_config"])
	assert.Equal(t, redactedSecretValue, config["secret"])
}

func TestGetStorageConfigNotFound(t *testing.T) {
	setup := setupTestConfigAPI(t)

	w := getJSON(t, setup.router, "/api/storage/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateStorageConfig(t *testing.T) {
	setup := setupTestConfigAPI(t)
	created := createStorageConfig(t, setup.router, "Old Name", map[string]any{"secret": "old"})
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/storage/"+id, map[string]any{
		"name":        "New Name",
		"rclone_type": "sftp",
		"rclone_config": map[string]any{
			"secret": "new",
			"host":   "backup.example.com",
		},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, id, body["id"])
	assert.Equal(t, "New Name", body["name"])
	assert.Equal(t, "sftp", body["rclone_type"])
	config := requireMap(t, body["rclone_config"])
	assert.Equal(t, redactedSecretValue, config["secret"])
	assert.Equal(t, "backup.example.com", config["host"])

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.NotContains(t, stored.RcloneConfig, "new")
	assert.NotContains(t, stored.RcloneConfig, "backup.example.com")
}

func TestUpdateStorageConfigCredentialsMarkReferencedPoliciesUnsyncedAndPublishEvents(t *testing.T) {
	setup := setupTestConfigAPI(t)
	agentA := createStorageTestAgent(t, setup.database, "Tokyo-1")
	agentB := createStorageTestAgent(t, setup.database, "Osaka-1")
	created := createStorageConfig(t, setup.router, "Shared Storage", map[string]any{"secret": "old", "endpoint": "old.example.com"})
	storageID := created["id"].(string)
	policyA := createStorageTestPolicy(t, setup.database, agentA.ID, storageID, true)
	policyB := createStorageTestPolicy(t, setup.database, agentB.ID, storageID, true)

	received := make(chan events.Event, 2)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := putJSON(t, setup.router, "/api/storage/"+storageID, map[string]any{
		"rclone_config": map[string]any{
			"secret":   "new",
			"endpoint": "new.example.com",
		},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updatedA db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&updatedA, "id = ?", policyA.ID).Error)
	assert.False(t, updatedA.Synced)
	var updatedB db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&updatedB, "id = ?", policyB.ID).Error)
	assert.False(t, updatedB.Synced)

	assertPolicyChangedEvent(t, received, agentA.ID, "storage_updated")
	assertPolicyChangedEvent(t, received, agentB.ID, "storage_updated")
	assertNoPolicyChangedEvent(t, received)
}

func TestUpdateStorageConfigNameOnlyDoesNotMarkPoliciesUnsyncedOrPublishEvents(t *testing.T) {
	setup := setupTestConfigAPI(t)
	agent := createStorageTestAgent(t, setup.database, "Tokyo-1")
	created := createStorageConfig(t, setup.router, "Old Name", map[string]any{"secret": "old"})
	storageID := created["id"].(string)
	policy := createStorageTestPolicy(t, setup.database, agent.ID, storageID, true)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := putJSON(t, setup.router, "/api/storage/"+storageID, map[string]any{
		"name": "New Name",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", policy.ID).Error)
	assert.True(t, stored.Synced)
	assertNoPolicyChangedEvent(t, received)
}

func TestDeleteStorageConfigReferencedByPolicyReturnsConflict(t *testing.T) {
	setup := setupTestConfigAPI(t)
	agent := createStorageTestAgent(t, setup.database, "Tokyo-1")
	created := createStorageConfig(t, setup.router, "In Use", map[string]any{"key": "value"})
	storageID := created["id"].(string)
	createStorageTestPolicy(t, setup.database, agent.ID, storageID, true)

	w := deleteJSON(t, setup.router, "/api/storage/"+storageID)

	require.Equal(t, http.StatusConflict, w.Code)

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", storageID).Error)
}

func TestUpdateStorageConfigNotFound(t *testing.T) {
	setup := setupTestConfigAPI(t)

	w := putJSON(t, setup.router, "/api/storage/missing", map[string]any{"name": "Nope"})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteStorageConfig(t *testing.T) {
	setup := setupTestConfigAPI(t)
	created := createStorageConfig(t, setup.router, "To Delete", map[string]any{"key": "value"})
	id := created["id"].(string)

	w := deleteJSON(t, setup.router, "/api/storage/"+id)

	require.Equal(t, http.StatusNoContent, w.Code)

	w = getJSON(t, setup.router, "/api/storage/"+id)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteStorageConfigNotFound(t *testing.T) {
	setup := setupTestConfigAPI(t)

	w := deleteJSON(t, setup.router, "/api/storage/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func createStorageConfig(t *testing.T, router http.Handler, name string, config map[string]any) map[string]any {
	t.Helper()

	w := postAnyJSON(t, router, "/api/storage", map[string]any{
		"name":          name,
		"rclone_type":   "s3",
		"rclone_config": config,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return parseJSON(t, w)
}

func parseJSONInto(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()

	require.NoError(t, json.Unmarshal(w.Body.Bytes(), target))
}

func postAnyJSON(t *testing.T, router http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func putJSON(t *testing.T, router http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func requireMap(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	require.True(t, ok, "expected map, got %T", value)
	return result
}

func createStorageTestAgent(t *testing.T, database *db.Database, name string) db.Agent {
	t.Helper()

	agent := db.Agent{
		Name:   name,
		Status: "online",
	}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func createStorageTestPolicy(t *testing.T, database *db.Database, agentID string, storageID string, synced bool) db.BackupPolicy {
	t.Helper()

	encryptedPassword, err := db.Encrypt("restic-password", database.MasterKey)
	require.NoError(t, err)

	policy := db.BackupPolicy{
		AgentID:         agentID,
		StorageID:       storageID,
		RepoPath:        "vaultfleet/" + agentID,
		ResticPassword:  encryptedPassword,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3}`,
		Synced:          synced,
	}
	require.NoError(t, database.DB.Create(&policy).Error)
	return policy
}

func assertPolicyChangedEvent(t *testing.T, received <-chan events.Event, agentID string, action string) {
	t.Helper()

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agentID, payload["agent_id"])
		assert.Equal(t, action, payload["action"])
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for policy changed event for %s", agentID)
	}
}

func assertNoPolicyChangedEvent(t *testing.T, received <-chan events.Event) {
	t.Helper()

	select {
	case event := <-received:
		t.Fatalf("unexpected policy changed event: %#v", event)
	case <-time.After(25 * time.Millisecond):
	}
}
