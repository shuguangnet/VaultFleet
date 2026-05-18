package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
)

type testPolicySetup struct {
	database *db.Database
	bus      *events.Bus
	router   *gin.Engine
}

func setupTestPolicyAPI(t *testing.T) testPolicySetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	bus := events.NewBus()
	handler := NewPolicyHandler(database, bus)
	router := gin.New()
	api := router.Group("/api")
	RegisterPolicyRoutes(api, handler)

	return testPolicySetup{
		database: database,
		bus:      bus,
		router:   router,
	}
}

func TestCreatePolicy(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":         agent.ID,
		"storage_id":       storage.ID,
		"backup_dirs":      []string{"/etc", "/home"},
		"exclude_patterns": []string{"*.log", "*.tmp"},
		"schedule":         "0 3 * * *",
		"retention": map[string]any{
			"keep_last":    3,
			"keep_daily":   7,
			"keep_weekly":  4,
			"keep_monthly": 6,
		},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, agent.ID, body["agent_id"])
	assert.Equal(t, storage.ID, body["storage_id"])
	assert.Equal(t, "vaultfleet/"+agent.ID, body["repo_path"])
	assert.NotEmpty(t, body["restic_password"])
	assert.Equal(t, false, body["synced"])
	assertJSONList(t, body["backup_dirs"], []string{"/etc", "/home"})
	assertJSONList(t, body["exclude_patterns"], []string{"*.log", "*.tmp"})
	retention := requireMap(t, body["retention"])
	assert.Equal(t, float64(3), retention["keep_last"])
	assert.Equal(t, float64(7), retention["keep_daily"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.Equal(t, `["/etc","/home"]`, stored.BackupDirs)
	assert.Equal(t, `["*.log","*.tmp"]`, stored.ExcludePatterns)
	assert.JSONEq(t, `{"keep_last":3,"keep_daily":7,"keep_weekly":4,"keep_monthly":6}`, stored.Retention)
	assert.NotContains(t, stored.ResticPassword, body["restic_password"].(string))
	assert.NotEmpty(t, stored.ResticPassword)
}

func TestCreatePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Contains(t, []string{"create", "created"}, payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}

	assert.NotEmpty(t, created["id"])
}

func TestCreatePolicyWithProvidedRepoPathAndPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":        agent.ID,
		"storage_id":      storage.ID,
		"repo_path":       "custom/repo",
		"restic_password": "provided-secret",
		"backup_dirs":     []string{"/etc"},
		"schedule":        "0 3 * * *",
		"retention":       map[string]any{"keep_last": 3},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "custom/repo", body["repo_path"])
	assert.Equal(t, "provided-secret", body["restic_password"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.NotContains(t, stored.ResticPassword, "provided-secret")
}

func TestCreatePolicyAutoGeneratesPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	password := created["restic_password"].(string)

	assert.GreaterOrEqual(t, len(password), 32)

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", created["id"]).Error)
	assert.NotEqual(t, password, stored.ResticPassword)
	assert.NotContains(t, stored.ResticPassword, password)
}

func TestCreatePolicyValidatesReferencedAgentAndStorage(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    "missing-agent",
		"storage_id":  storage.ID,
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    agent.ID,
		"storage_id":  "missing-storage",
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListPoliciesOmitsResticPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	createPolicy(t, setup.router, agent.ID, storage.ID)

	w := getJSON(t, setup.router, "/api/policies")

	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	parseJSONInto(t, w, &list)
	require.Len(t, list, 1)
	assert.NotContains(t, list[0], "restic_password")
	assertJSONList(t, list[0]["backup_dirs"], []string{"/etc"})
	retention := requireMap(t, list[0]["retention"])
	assert.Equal(t, float64(3), retention["keep_last"])
}

func TestGetPolicyOmitsResticPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	w := getJSON(t, setup.router, "/api/policies/"+created["id"].(string))

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, created["id"], body["id"])
	assert.NotContains(t, body, "restic_password")
}

func TestGetPolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := getJSON(t, setup.router, "/api/policies/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdatePolicyMarksSyncedFalse(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)
	require.NoError(t, setup.database.DB.Model(&db.BackupPolicy{}).Where("id = ?", id).Update("synced", true).Error)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"schedule":         "0 4 * * *",
		"backup_dirs":      []string{"/var/lib"},
		"exclude_patterns": []string{"cache"},
		"retention":        map[string]any{"keep_last": 5, "keep_daily": 2},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["synced"])
	assert.Equal(t, "0 4 * * *", body["schedule"])
	assert.NotContains(t, body, "restic_password")
	assertJSONList(t, body["backup_dirs"], []string{"/var/lib"})
	assertJSONList(t, body["exclude_patterns"], []string{"cache"})
	retention := requireMap(t, body["retention"])
	assert.Equal(t, float64(5), retention["keep_last"])
	assert.Equal(t, float64(2), retention["keep_daily"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.False(t, stored.Synced)
	assert.Equal(t, `["/var/lib"]`, stored.BackupDirs)
	assert.Equal(t, `["cache"]`, stored.ExcludePatterns)
	assert.JSONEq(t, `{"keep_daily":2,"keep_last":5}`, stored.Retention)
}

func TestUpdatePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := putJSON(t, setup.router, "/api/policies/"+created["id"].(string), map[string]any{
		"schedule": "0 5 * * *",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Equal(t, "updated", payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}
}

func TestUpdatePolicyValidatesNewStorage(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	w := putJSON(t, setup.router, "/api/policies/"+created["id"].(string), map[string]any{
		"storage_id": "missing-storage",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdatePolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := putJSON(t, setup.router, "/api/policies/missing", map[string]any{"schedule": "0 1 * * *"})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePolicy(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := deleteJSON(t, setup.router, "/api/policies/"+id)

	require.Equal(t, http.StatusNoContent, w.Code)

	w = getJSON(t, setup.router, "/api/policies/"+id)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := deleteJSON(t, setup.router, "/api/policies/"+id)

	require.Equal(t, http.StatusNoContent, w.Code)

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Contains(t, []string{"delete", "deleted"}, payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}
}

func TestDeletePolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := deleteJSON(t, setup.router, "/api/policies/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func createPolicyTestAgent(t *testing.T, database *db.Database) db.Agent {
	t.Helper()

	agent := db.Agent{
		Name:   "Tokyo-1",
		Status: "online",
	}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func createPolicyTestStorage(t *testing.T, database *db.Database) db.StorageConfig {
	t.Helper()

	encrypted, err := db.Encrypt(`{"provider":"Cloudflare"}`, database.MasterKey)
	require.NoError(t, err)

	storage := db.StorageConfig{
		Name:         "Test Storage",
		RcloneType:   "s3",
		RcloneConfig: encrypted,
	}
	require.NoError(t, database.DB.Create(&storage).Error)
	return storage
}

func createPolicy(t *testing.T, router http.Handler, agentID string, storageID string) map[string]any {
	t.Helper()

	w := postAnyJSON(t, router, "/api/policies", map[string]any{
		"agent_id":    agentID,
		"storage_id":  storageID,
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return parseJSON(t, w)
}

func assertJSONList(t *testing.T, value any, expected []string) {
	t.Helper()

	raw, ok := value.([]any)
	require.True(t, ok, "expected list, got %T", value)
	require.Len(t, raw, len(expected))
	for i, expectedValue := range expected {
		assert.Equal(t, expectedValue, raw[i])
	}
}
