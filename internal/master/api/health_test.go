package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestHealthDoesNotRequireDatabase(t *testing.T) {
	router := newHealthTestRouter(nil, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseHealthTestJSON(t, w)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "healthy", body["status"])
}

func TestReadyChecksDatabase(t *testing.T) {
	database := newHealthTestDatabase(t)
	router := newHealthTestRouter(database, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseHealthTestJSON(t, w)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "ready", body["status"])
}

func TestReadyFailsWhenDatabaseClosed(t *testing.T) {
	database := newHealthTestDatabase(t)
	sqlDB, err := database.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	router := newHealthTestRouter(database, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	body := parseHealthTestJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "not ready", body["error"])
}

func TestMetricsOutputsPrometheusText(t *testing.T) {
	database := newHealthTestDatabase(t)
	agentOnline := db.Agent{Name: "Online Agent"}
	agentOffline := db.Agent{Name: "Offline Agent"}
	require.NoError(t, database.DB.Create(&agentOnline).Error)
	require.NoError(t, database.DB.Create(&agentOffline).Error)

	require.NoError(t, database.DB.Create(&db.AgentCommand{
		AgentID:   agentOnline.ID,
		Type:      protocol.TypeBackupNow,
		Status:    commands.CommandStatusSucceeded,
		MessageID: "backup-message",
	}).Error)
	require.NoError(t, database.DB.Create(&db.AgentCommand{
		AgentID:   agentOffline.ID,
		Type:      protocol.TypeSnapshotListReq,
		Status:    commands.CommandStatusPending,
		MessageID: "snapshot-message",
	}).Error)

	finishedAt := time.Date(2026, 5, 20, 14, 15, 16, 0, time.UTC)
	require.NoError(t, database.DB.Create(&db.TaskHistory{
		AgentID:    agentOnline.ID,
		Type:       "backup",
		Status:     commands.TaskStatusSuccess,
		FinishedAt: &finishedAt,
	}).Error)
	require.NoError(t, database.DB.Create(&db.TaskHistory{
		AgentID: agentOffline.ID,
		Type:    "restore",
		Status:  commands.TaskStatusFailed,
	}).Error)

	hub := fakeHealthAgentStatusProvider{online: 2}
	router := newHealthTestRouter(database, hub)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	body := w.Body.String()
	assert.Contains(t, body, "vaultfleet_agents_total 2")
	assert.Contains(t, body, "vaultfleet_agents_online 2")
	assert.Contains(t, body, `vaultfleet_agent_commands_total{status="succeeded",type="backup_now"} 1`)
	assert.Contains(t, body, `vaultfleet_agent_commands_total{status="pending",type="snapshot_list_req"} 1`)
	assert.Contains(t, body, `vaultfleet_tasks_total{status="success",type="backup"} 1`)
	assert.Contains(t, body, `vaultfleet_tasks_total{status="failed",type="restore"} 1`)
	assert.Contains(t, body, "vaultfleet_last_successful_backup_timestamp_seconds 1779286516")
}

func TestMetricsReturnsText500WhenDatabaseQueryFails(t *testing.T) {
	database := newHealthTestDatabase(t)
	sqlDB, err := database.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	router := newHealthTestRouter(database, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.True(t, strings.Contains(w.Body.String(), "metrics unavailable"))
}

type fakeHealthAgentStatusProvider struct {
	online int
}

func (p fakeHealthAgentStatusProvider) OnlineAgentCount() int {
	return p.online
}

func newHealthTestRouter(database *db.Database, provider AgentStatusProvider) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterHealthRoutes(router, NewHealthHandler(database, provider))
	return router
}

func newHealthTestDatabase(t *testing.T) *db.Database {
	t.Helper()

	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	return database
}

func parseHealthTestJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}
