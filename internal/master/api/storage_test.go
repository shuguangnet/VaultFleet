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

type testConfigSetup struct {
	database *db.Database
	router   *gin.Engine
}

func setupTestConfigAPI(t *testing.T) testConfigSetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	handler := NewConfigHandler(database)
	router := gin.New()
	api := router.Group("/api")
	RegisterStorageRoutes(api, handler)

	return testConfigSetup{
		database: database,
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
	assert.Equal(t, "SECRET456", config["secret_access_key"])

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.NotContains(t, stored.RcloneConfig, "SECRET456")
	assert.NotContains(t, stored.RcloneConfig, "AKID123")
	assert.NotEqual(t, "", stored.RcloneConfig)
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
	assert.Equal(t, "value", config["secret"])
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
	assert.Equal(t, "new", config["secret"])
	assert.Equal(t, "backup.example.com", config["host"])

	var stored db.StorageConfig
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.NotContains(t, stored.RcloneConfig, "new")
	assert.NotContains(t, stored.RcloneConfig, "backup.example.com")
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
