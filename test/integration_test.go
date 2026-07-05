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
	assert.Contains(t, resp2.Header.Get("Content-Type"), "text/html")
	assert.Contains(t, string(bodyBytes), "id=\"root\"")
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
