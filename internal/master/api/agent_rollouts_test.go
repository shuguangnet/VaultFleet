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

	"vaultfleet/internal/master/agentrollout"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestAgentRolloutCreateListDetailAndCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createRolloutAPIAgent(t, database, "node-a", "online", "v0.5.41", "amd64", []string{"prod", "web"})
	hub := &apiRolloutHub{online: map[string]bool{agent.ID: true}, accepted: map[string]bool{agent.ID: true}}
	service := agentrollout.NewService(database, hub)
	service.ACKTimeout = time.Second
	handler := NewAgentRolloutHandler(database, service)
	router := gin.New()
	RegisterAgentRolloutRoutes(router.Group("/api"), handler)

	w := postRolloutJSON(t, router, "/api/agent-upgrade-rollouts", map[string]any{
		"target_version": "v0.5.42",
		"target_tags":    []string{"prod", "web"},
		"canary_count":   1,
		"batch_size":     1,
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	rolloutID := data["id"].(string)
	assert.Equal(t, "v0.5.42", data["target_version"])
	assert.Contains(t, []string{"pending", "running"}, data["status"])
	items := data["items"].([]any)
	require.Len(t, items, 1)
	assert.Contains(t, []string{"pending", "running"}, requireMap(t, items[0])["status"])

	w = getJSON(t, router, "/api/agent-upgrade-rollouts")
	require.Equal(t, http.StatusOK, w.Code)
	list := parseJSON(t, w)["data"].([]any)
	require.Len(t, list, 1)

	w = getJSON(t, router, "/api/agent-upgrade-rollouts/"+rolloutID)
	require.Equal(t, http.StatusOK, w.Code)
	detail := requireMap(t, parseJSON(t, w)["data"])
	assert.Equal(t, rolloutID, detail["id"])
	assert.NotEmpty(t, detail["items"])

	w = postRolloutJSON(t, router, "/api/agent-upgrade-rollouts/"+rolloutID+"/cancel", map[string]any{"reason": "stop"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var rollout db.AgentUpgradeRollout
	require.NoError(t, database.DB.First(&rollout, "id = ?", rolloutID).Error)
	assert.Equal(t, agentrollout.RolloutStatusCancelled, rollout.Status)
}

func TestAgentRolloutCreateValidationAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	handler := NewAgentRolloutHandler(database, agentrollout.NewService(database, &apiRolloutHub{}))
	router := gin.New()
	RegisterAgentRolloutRoutes(router.Group("/api"), handler)

	w := postRolloutJSON(t, router, "/api/agent-upgrade-rollouts", map[string]any{"target_version": "v0.5.42"})
	require.Equal(t, http.StatusBadRequest, w.Code)

	createRolloutAPIAgent(t, database, "node-a", "online", "v0.5.42", "amd64", []string{"prod"})
	w = postRolloutJSON(t, router, "/api/agent-upgrade-rollouts", map[string]any{
		"target_version": "v0.5.42",
		"target_tags":    []string{"prod"},
	})
	require.Equal(t, http.StatusOK, w.Code)

	var audits []db.AuditEvent
	require.NoError(t, database.DB.Where("action = ?", "agent_rollout.create").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Equal(t, "success", audits[0].Result)
}

func TestAgentRolloutPermissionMapping(t *testing.T) {
	assert.Equal(t, PermissionWriteNodes, permissionForRoute(http.MethodPost, "/api/agent-upgrade-rollouts"))
	assert.Equal(t, PermissionReadOperational, permissionForRoute(http.MethodGet, "/api/agent-upgrade-rollouts"))
	assert.Equal(t, PermissionWriteNodes, permissionForRoute(http.MethodPost, "/api/agent-upgrade-rollouts/:id/cancel"))
}

func createRolloutAPIAgent(t *testing.T, database *db.Database, name string, status string, version string, arch string, tags []string) db.Agent {
	t.Helper()
	systemInfo := mustJSON(t, map[string]any{"version": version, "arch": arch})
	agentTags := mustJSON(t, tags)
	agent := db.Agent{Name: name, Status: status, SystemInfo: systemInfo, Tags: agentTags}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func postRolloutJSON(t *testing.T, router http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

type apiRolloutHub struct {
	online   map[string]bool
	accepted map[string]bool
}

func (h *apiRolloutHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *apiRolloutHub) SendAndWait(agentID string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
	resp, err := protocol.NewMessage(protocol.TypeUpdateAgentResp, protocol.UpdateAgentRespPayload{Accepted: h.accepted[agentID]})
	if err != nil {
		return nil, err
	}
	resp.ID = msg.ID
	ch := make(chan protocol.Message, 1)
	ch <- *resp
	close(ch)
	return ch, nil
}
