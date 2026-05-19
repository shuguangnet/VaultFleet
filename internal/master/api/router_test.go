package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/ws"
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
