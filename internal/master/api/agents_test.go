package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

type testAgentsSetup struct {
	database *db.Database
	handler  *AgentHandler
	router   *gin.Engine
}

func setupTestAgents(t *testing.T) testAgentsSetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	handler := NewAgentHandler(database)
	router := gin.New()

	router.POST("/api/agents", handler.Create)
	router.GET("/api/agents", handler.List)
	router.GET("/api/agents/:id", handler.Get)
	router.DELETE("/api/agents/:id", handler.Delete)
	router.POST("/api/agents/:id/regenerate-token", handler.RegenerateToken)
	router.POST("/api/agent/enroll", handler.Enroll)

	return testAgentsSetup{
		database: database,
		handler:  handler,
		router:   router,
	}
}

func createTestAgent(t *testing.T, router http.Handler, name string) map[string]any {
	t.Helper()

	w := postJSON(t, router, "/api/agents", map[string]string{"name": name})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	body := parseJSON(t, w)
	require.Equal(t, true, body["ok"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	return data
}

func getJSON(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func deleteJSON(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodDelete, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestCreateAgent(t *testing.T) {
	setup := setupTestAgents(t)

	data := createTestAgent(t, setup.router, "Tokyo-1")

	id, ok := data["id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, id)
	assert.Equal(t, "Tokyo-1", data["name"])

	enrollToken, ok := data["enroll_token"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(enrollToken, "ek_"))

	var agent db.Agent
	require.NoError(t, setup.database.DB.First(&agent, "id = ?", id).Error)
	assert.Equal(t, "Tokyo-1", agent.Name)
	assert.Equal(t, enrollToken, agent.EnrollToken)
	assert.Empty(t, agent.AgentToken)
	assert.Equal(t, "offline", agent.Status)
}

func TestCreateAgent_MissingName(t *testing.T) {
	setup := setupTestAgents(t)

	w := postJSON(t, setup.router, "/api/agents", map[string]string{})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListAgents(t *testing.T) {
	setup := setupTestAgents(t)

	first := createTestAgent(t, setup.router, "Tokyo-1")
	second := createTestAgent(t, setup.router, "Tokyo-2")

	w := getJSON(t, setup.router, "/api/agents")

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])

	data, ok := body["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 2)

	seen := map[string]bool{}
	for _, item := range data {
		agent, ok := item.(map[string]any)
		require.True(t, ok)
		seen[agent["id"].(string)] = true
		assert.NotContains(t, agent, "agent_token")
	}
	assert.True(t, seen[first["id"].(string)])
	assert.True(t, seen[second["id"].(string)])
}

func TestListAgents_Empty(t *testing.T) {
	setup := setupTestAgents(t)

	w := getJSON(t, setup.router, "/api/agents")

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data, ok := body["data"].([]any)
	require.True(t, ok)
	assert.Empty(t, data)
}

func TestGetAgent(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	id := created["id"].(string)

	w := getJSON(t, setup.router, "/api/agents/"+id)

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, id, data["id"])
	assert.Equal(t, "Tokyo-1", data["name"])
	assert.NotContains(t, data, "agent_token")
}

func TestGetAgent_NotFound(t *testing.T) {
	setup := setupTestAgents(t)

	w := getJSON(t, setup.router, "/api/agents/nonexistent-id")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteAgent(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	id := created["id"].(string)

	w := deleteJSON(t, setup.router, "/api/agents/"+id)

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])

	w = getJSON(t, setup.router, "/api/agents/"+id)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteAgent_NotFound(t *testing.T) {
	setup := setupTestAgents(t)

	w := deleteJSON(t, setup.router, "/api/agents/nonexistent-id")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegenerateToken(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	id := created["id"].(string)
	oldToken := created["enroll_token"].(string)

	w := postJSON(t, setup.router, "/api/agents/"+id+"/regenerate-token", map[string]string{})

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, id, data["id"])

	newToken, ok := data["enroll_token"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(newToken, "ek_"))
	assert.NotEqual(t, oldToken, newToken)

	var agent db.Agent
	require.NoError(t, setup.database.DB.First(&agent, "id = ?", id).Error)
	assert.Equal(t, newToken, agent.EnrollToken)
	assert.Empty(t, agent.AgentToken)
}

func TestRegenerateToken_NotFound(t *testing.T) {
	setup := setupTestAgents(t)

	w := postJSON(t, setup.router, "/api/agents/nonexistent-id/regenerate-token", map[string]string{})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestEnrollAgent(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	id := created["id"].(string)
	enrollToken := created["enroll_token"].(string)
	systemInfo := `{"os":"linux","arch":"amd64"}`

	w := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
		"system_info":  systemInfo,
	})

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, id, data["agent_id"])

	agentToken, ok := data["agent_token"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(agentToken, "ak_"))

	var agent db.Agent
	require.NoError(t, setup.database.DB.First(&agent, "id = ?", id).Error)
	assert.Empty(t, agent.EnrollToken)
	assert.Equal(t, agentToken, agent.AgentToken)
	assert.Equal(t, systemInfo, agent.SystemInfo)
}

func TestEnrollAgent_InvalidToken(t *testing.T) {
	setup := setupTestAgents(t)

	w := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": "ek_invalid",
	})

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestEnrollAgent_TokenConsumedAfterUse(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	enrollToken := created["enroll_token"].(string)

	first := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
	})
	require.Equal(t, http.StatusOK, first.Code)

	second := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
	})
	require.Equal(t, http.StatusUnauthorized, second.Code)
}

func TestEnrollAgent_RegenerateAndReEnroll(t *testing.T) {
	setup := setupTestAgents(t)
	created := createTestAgent(t, setup.router, "Tokyo-1")
	id := created["id"].(string)
	enrollToken := created["enroll_token"].(string)

	first := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": enrollToken,
	})
	require.Equal(t, http.StatusOK, first.Code)
	firstBody := parseJSON(t, first)
	firstData := firstBody["data"].(map[string]any)
	firstAgentToken := firstData["agent_token"].(string)
	assert.Equal(t, id, firstData["agent_id"])

	regenerated := postJSON(t, setup.router, "/api/agents/"+id+"/regenerate-token", map[string]string{})
	require.Equal(t, http.StatusOK, regenerated.Code)
	regeneratedBody := parseJSON(t, regenerated)
	regeneratedData := regeneratedBody["data"].(map[string]any)
	newEnrollToken := regeneratedData["enroll_token"].(string)
	require.NotEqual(t, enrollToken, newEnrollToken)

	second := postJSON(t, setup.router, "/api/agent/enroll", map[string]string{
		"enroll_token": newEnrollToken,
	})
	require.Equal(t, http.StatusOK, second.Code)
	secondBody := parseJSON(t, second)
	secondData := secondBody["data"].(map[string]any)
	assert.Equal(t, id, secondData["agent_id"])

	secondAgentToken := secondData["agent_token"].(string)
	assert.True(t, strings.HasPrefix(secondAgentToken, "ak_"))
	assert.NotEqual(t, firstAgentToken, secondAgentToken)
}

func TestEnrollAgent_MissingToken(t *testing.T) {
	setup := setupTestAgents(t)

	body, err := json.Marshal(map[string]string{"system_info": "{}"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	setup.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
