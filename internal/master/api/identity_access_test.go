package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

func TestIdentityAccessSessionRoleAndDisabledUser(t *testing.T) {
	setup := setupRouterAssembly(t)
	adminCookie := loginRouterUser(t, setup.router, "admin", "secret123")

	createResp := authedJSON(t, setup.router, http.MethodPost, "/api/users", adminCookie, map[string]any{
		"username": "viewer",
		"password": "secret123",
		"role":     RoleViewer,
	})
	require.Equal(t, http.StatusCreated, createResp.Code, createResp.Body.String())
	user := dataMap(t, createResp)
	viewerID := user["id"].(string)

	viewerCookie := loginExistingRouterUser(t, setup.router, "viewer", "secret123")
	checkResp := authedJSON(t, setup.router, http.MethodGet, "/api/auth/check", viewerCookie, nil)
	require.Equal(t, http.StatusOK, checkResp.Code, checkResp.Body.String())
	data := dataMap(t, checkResp)
	assert.Equal(t, RoleViewer, data["role"])

	disableResp := authedJSON(t, setup.router, http.MethodPost, "/api/users/"+viewerID+"/disable", adminCookie, nil)
	require.Equal(t, http.StatusOK, disableResp.Code, disableResp.Body.String())

	afterDisable := authedJSON(t, setup.router, http.MethodGet, "/api/agents", viewerCookie, nil)
	require.Equal(t, http.StatusUnauthorized, afterDisable.Code, afterDisable.Body.String())
	loginResp := postJSON(t, setup.router, "/api/auth/login", map[string]string{"username": "viewer", "password": "secret123"})
	require.Equal(t, http.StatusUnauthorized, loginResp.Code, loginResp.Body.String())
}

func TestIdentityAccessRBACAndBearerTokenScopes(t *testing.T) {
	setup := setupRouterAssembly(t)
	adminCookie := loginRouterUser(t, setup.router, "admin", "secret123")

	createViewer := authedJSON(t, setup.router, http.MethodPost, "/api/users", adminCookie, map[string]any{
		"username": "viewer",
		"password": "secret123",
		"role":     RoleViewer,
	})
	require.Equal(t, http.StatusCreated, createViewer.Code, createViewer.Body.String())
	viewerCookie := loginExistingRouterUser(t, setup.router, "viewer", "secret123")

	readResp := authedJSON(t, setup.router, http.MethodGet, "/api/agents", viewerCookie, nil)
	require.Equal(t, http.StatusOK, readResp.Code, readResp.Body.String())
	writeResp := authedJSON(t, setup.router, http.MethodPost, "/api/agents", viewerCookie, map[string]any{"name": "blocked"})
	require.Equal(t, http.StatusForbidden, writeResp.Code, writeResp.Body.String())

	tokenResp := authedJSON(t, setup.router, http.MethodPost, "/api/api-tokens", adminCookie, map[string]any{
		"name":   "reader",
		"role":   RoleOperator,
		"scopes": []string{PermissionReadOperational},
	})
	require.Equal(t, http.StatusCreated, tokenResp.Code, tokenResp.Body.String())
	tokenData := dataMap(t, tokenResp)
	plainToken := tokenData["token"].(string)
	assert.NotEmpty(t, plainToken)

	tokenRead := bearerJSON(t, setup.router, http.MethodGet, "/api/agents", plainToken, nil)
	require.Equal(t, http.StatusOK, tokenRead.Code, tokenRead.Body.String())
	tokenWrite := bearerJSON(t, setup.router, http.MethodPost, "/api/agents", plainToken, map[string]any{"name": "blocked"})
	require.Equal(t, http.StatusForbidden, tokenWrite.Code, tokenWrite.Body.String())

	listTokens := authedJSON(t, setup.router, http.MethodGet, "/api/api-tokens", adminCookie, nil)
	require.Equal(t, http.StatusOK, listTokens.Code, listTokens.Body.String())
	listData := dataList(t, listTokens)
	require.NotEmpty(t, listData)
	assert.NotContains(t, listData[0].(map[string]any), "token")
}

func TestIdentityAccessAPITokenRejectsRevokedExpiredAndAgentTokens(t *testing.T) {
	setup := setupRouterAssembly(t)
	adminCookie := loginRouterUser(t, setup.router, "admin", "secret123")
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	expiredResp := authedJSON(t, setup.router, http.MethodPost, "/api/api-tokens", adminCookie, map[string]any{
		"name":       "expired",
		"role":       RoleOperator,
		"scopes":     []string{PermissionReadOperational},
		"expires_at": expired,
	})
	require.Equal(t, http.StatusCreated, expiredResp.Code, expiredResp.Body.String())
	expiredToken := dataMap(t, expiredResp)["token"].(string)
	expiredRead := bearerJSON(t, setup.router, http.MethodGet, "/api/agents", expiredToken, nil)
	require.Equal(t, http.StatusUnauthorized, expiredRead.Code, expiredRead.Body.String())

	validResp := authedJSON(t, setup.router, http.MethodPost, "/api/api-tokens", adminCookie, map[string]any{
		"name":   "revoked",
		"role":   RoleOperator,
		"scopes": []string{PermissionReadOperational},
	})
	require.Equal(t, http.StatusCreated, validResp.Code, validResp.Body.String())
	validData := dataMap(t, validResp)
	revokeResp := authedJSON(t, setup.router, http.MethodPost, "/api/api-tokens/"+validData["id"].(string)+"/revoke", adminCookie, nil)
	require.Equal(t, http.StatusOK, revokeResp.Code, revokeResp.Body.String())
	revokedRead := bearerJSON(t, setup.router, http.MethodGet, "/api/agents", validData["token"].(string), nil)
	require.Equal(t, http.StatusUnauthorized, revokedRead.Code, revokedRead.Body.String())

	agent := db.Agent{Name: "agent", AgentToken: "agent-token", Status: "online"}
	require.NoError(t, setup.database.DB.Create(&agent).Error)
	agentTokenRead := bearerJSON(t, setup.router, http.MethodGet, "/api/agents", "agent-token", nil)
	require.Equal(t, http.StatusUnauthorized, agentTokenRead.Code, agentTokenRead.Body.String())
}

func TestIdentityAccessAuditEvents(t *testing.T) {
	setup := setupRouterAssembly(t)
	adminCookie := loginRouterUser(t, setup.router, "admin", "secret123")

	resp := authedJSON(t, setup.router, http.MethodPost, "/api/agents", adminCookie, map[string]any{"name": "audit-agent"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	eventsResp := authedJSON(t, setup.router, http.MethodGet, "/api/audit-events?action=agent.create", adminCookie, nil)
	require.Equal(t, http.StatusOK, eventsResp.Code, eventsResp.Body.String())
	events := dataList(t, eventsResp)
	require.NotEmpty(t, events)
	event := events[0].(map[string]any)
	assert.Equal(t, "agent.create", event["action"])
	assert.Equal(t, AuditResultSuccess, event["result"])
	if message, ok := event["message"].(string); ok {
		assert.NotContains(t, message, "secret")
	}
}

func TestIdentityAccessSensitiveResponsesAreRestricted(t *testing.T) {
	setup := setupRouterAssembly(t)
	adminCookie := loginRouterUser(t, setup.router, "admin", "secret123")
	createViewer := authedJSON(t, setup.router, http.MethodPost, "/api/users", adminCookie, map[string]any{
		"username": "viewer",
		"password": "secret123",
		"role":     RoleViewer,
	})
	require.Equal(t, http.StatusCreated, createViewer.Code, createViewer.Body.String())
	viewerCookie := loginExistingRouterUser(t, setup.router, "viewer", "secret123")

	createAgent := authedJSON(t, setup.router, http.MethodPost, "/api/agents", adminCookie, map[string]any{"name": "node"})
	require.Equal(t, http.StatusOK, createAgent.Code, createAgent.Body.String())
	agentID := dataMap(t, createAgent)["id"].(string)
	installToken := authedJSON(t, setup.router, http.MethodGet, "/api/agents/"+agentID+"/install-token", viewerCookie, nil)
	require.Equal(t, http.StatusForbidden, installToken.Code, installToken.Body.String())

	createStorage := authedJSON(t, setup.router, http.MethodPost, "/api/storage", adminCookie, map[string]any{
		"name":        "s3",
		"rclone_type": "s3",
		"rclone_config": map[string]any{
			"provider":          "Other",
			"endpoint":          "https://s3.example.test",
			"bucket":            "backups",
			"access_key_id":     "AKIA",
			"secret_access_key": "SECRET",
		},
	})
	require.Equal(t, http.StatusCreated, createStorage.Code, createStorage.Body.String())
	storageID := dataMap(t, createStorage)["id"].(string)
	readStorage := authedJSON(t, setup.router, http.MethodGet, "/api/storage/"+storageID, viewerCookie, nil)
	require.Equal(t, http.StatusOK, readStorage.Code, readStorage.Body.String())
	config := requireMap(t, dataMap(t, readStorage)["rclone_config"])
	assert.Equal(t, redactedSecretValue, config["secret_access_key"])
}

func loginRouterUser(t *testing.T, router http.Handler, username string, password string) *http.Cookie {
	t.Helper()
	initResponse := postJSON(t, router, "/api/auth/init", map[string]string{"username": username, "password": password})
	require.Equal(t, http.StatusOK, initResponse.Code, initResponse.Body.String())
	return getSessionCookie(t, initResponse)
}

func loginExistingRouterUser(t *testing.T, router http.Handler, username string, password string) *http.Cookie {
	t.Helper()
	resp := postJSON(t, router, "/api/auth/login", map[string]string{"username": username, "password": password})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return getSessionCookie(t, resp)
}

func authedJSON(t *testing.T, router http.Handler, method string, path string, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := jsonRequest(t, router, method, path, body, func(req *http.Request) {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	})
	return w
}

func bearerJSON(t *testing.T, router http.Handler, method string, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return jsonRequest(t, router, method, path, body, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	})
}

func jsonRequest(t *testing.T, router http.Handler, method string, path string, body any, modify func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		payload = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if modify != nil {
		modify(req)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func dataMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	return requireMap(t, parseJSON(t, w)["data"])
}

func dataList(t *testing.T, w *httptest.ResponseRecorder) []any {
	t.Helper()
	return requireList(t, parseJSON(t, w)["data"])
}
