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
	"vaultfleet/internal/master/ws"
	"vaultfleet/pkg/protocol"
)

func TestBrowseAgentHappyRelay(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeDirBrowseReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.DirBrowseReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, "/etc", req.Path)
		assert.Equal(t, 2, req.Depth)

		resp, err := protocol.NewMessage(protocol.TypeDirBrowseResp, protocol.DirBrowseRespPayload{
			Path: "/etc",
			Entries: []protocol.DirEntry{
				{Path: "/etc/nginx.conf", Type: "file", Size: 10},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID

		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/browse", map[string]any{
		"path":  "/etc",
		"depth": 99,
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body protocol.DirBrowseRespPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "/etc", body.Path)
	assert.Equal(t, []protocol.DirEntry{{Path: "/etc/nginx.conf", Type: "file", Size: 10}}, body.Entries)
}

func TestBrowseAgentOffline(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "offline")

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/browse", map[string]any{
		"path":  "/",
		"depth": 2,
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent offline", body["error"])
}

func TestBrowseAgentMissing(t *testing.T) {
	setup := setupBrowseAPI(t)

	w := postBrowseJSON(t, setup.router, "/api/agents/missing/browse", map[string]any{
		"path": "/",
	})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestBrowseAgentRequiresPath(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/browse", map[string]any{
		"depth": 2,
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBrowseAgentTimeout(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)
	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/browse", map[string]any{
		"path":  "/",
		"depth": 2,
	})

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
}

type browseAPISetup struct {
	database *db.Database
	hub      *ws.Hub
	handler  *BrowseHandler
	router   *gin.Engine
}

func setupBrowseAPI(t *testing.T) browseAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	handler := NewBrowseHandler(database, hub)
	router := gin.New()
	RegisterBrowseRoutes(router.Group("/api"), handler)

	return browseAPISetup{
		database: database,
		hub:      hub,
		handler:  handler,
		router:   router,
	}
}

func createBrowseAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Browse Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func postBrowseJSON(t *testing.T, router http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
