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
	raw := parseJSON(t, w)
	assert.Equal(t, true, raw["ok"])
	data, err := json.Marshal(raw["data"])
	require.NoError(t, err)
	var body protocol.DirBrowseRespPayload
	require.NoError(t, json.Unmarshal(data, &body))
	assert.Equal(t, "/etc", raw["path"])
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
	assert.Equal(t, false, body["ok"])
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

func TestDirSizeHappyRelay(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	setup.handler.dirSizeTimeout = time.Second
	setup.handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeDirSizeReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.DirSizeReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, "/home/data", req.Path)

		resp, err := protocol.NewMessage(protocol.TypeDirSizeResp, protocol.DirSizeRespPayload{
			Path: "/home/data",
			Size: 1073741824,
		})
		require.NoError(t, err)
		resp.ID = msg.ID

		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/dir-size", map[string]any{
		"path": "/home/data",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	raw := parseJSON(t, w)
	assert.Equal(t, true, raw["ok"])
	data, err := json.Marshal(raw["data"])
	require.NoError(t, err)
	var body protocol.DirSizeRespPayload
	require.NoError(t, json.Unmarshal(data, &body))
	assert.Equal(t, "/home/data", body.Path)
	assert.Equal(t, int64(1073741824), body.Size)
}

func TestDirSizeAgentOffline(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "offline")

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/dir-size", map[string]any{
		"path": "/home",
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "agent offline", body["error"])
}

func TestDirSizeRequiresPath(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/dir-size", map[string]any{})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDiscoverDockerHappyRelay(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	markBrowseAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityDockerWorkloadBackups})
	setup.hub.Add(agent.ID, nil)
	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeDockerDiscoveryReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		resp, err := protocol.NewMessage(protocol.TypeDockerDiscoveryResp, protocol.DockerDiscoveryRespPayload{
			Available: true,
			Containers: []protocol.DockerContainer{
				{ID: "container-1", Names: []string{"db"}, Image: "postgres:16", State: "running", Selectable: true},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/docker/discover", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	raw := parseJSON(t, w)
	data, err := json.Marshal(raw["data"])
	require.NoError(t, err)
	var body protocol.DockerDiscoveryRespPayload
	require.NoError(t, json.Unmarshal(data, &body))
	assert.True(t, body.Available)
	require.Len(t, body.Containers, 1)
	assert.Equal(t, "container-1", body.Containers[0].ID)
}

func TestDiscoverDockerRejectsUnsupportedAgent(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/docker/discover", nil)

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent does not support Docker workload backups", body["error"])
}

func TestDiscoverDockerRejectsOfflineAgent(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "offline")
	markBrowseAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityDockerWorkloadBackups})

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/docker/discover", nil)

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent offline", body["error"])
}

func TestDiscoverDatabasesHappyRelay(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	markBrowseAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityDatabaseBackups})
	setup.hub.Add(agent.ID, nil)
	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeDatabaseDiscoveryReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.DatabaseDiscoveryReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, protocol.DatabaseEngineMySQL, req.Source.Engine)
		assert.Equal(t, protocol.DatabaseExecutionDocker, req.Source.ExecutionMode)
		assert.Equal(t, "root", req.Source.Username)
		assert.Equal(t, "secret", req.Source.Password)
		assert.True(t, req.Source.AllDatabases)
		assert.Empty(t, req.Source.Database)
		require.NotNil(t, req.Source.DockerContainer)
		assert.Equal(t, "mysql", req.Source.DockerContainer.Name)

		resp, err := protocol.NewMessage(protocol.TypeDatabaseDiscoveryResp, protocol.DatabaseDiscoveryRespPayload{
			Available: true,
			Databases: []string{"app", "logs"},
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/database/discover", map[string]any{
		"source": map[string]any{
			"engine":         "mysql",
			"execution_mode": "docker",
			"username":       "root",
			"password":       "secret",
			"docker_container": map[string]any{
				"name": "mysql",
			},
		},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	raw := parseJSON(t, w)
	data, err := json.Marshal(raw["data"])
	require.NoError(t, err)
	var body protocol.DatabaseDiscoveryRespPayload
	require.NoError(t, json.Unmarshal(data, &body))
	assert.True(t, body.Available)
	assert.Equal(t, []string{"app", "logs"}, body.Databases)
}

func TestDiscoverDatabasesRejectsUnsupportedAgent(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/database/discover", map[string]any{
		"source": map[string]any{
			"engine":         "postgresql",
			"execution_mode": "host",
			"username":       "postgres",
		},
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent does not support database backups", body["error"])
}

func markBrowseAgentCapabilities(t *testing.T, database *db.Database, agentID string, capabilities []string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"capabilities": capabilities})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Update("system_info", string(data)).Error)
}

func TestDirSizeTimeout(t *testing.T) {
	setup := setupBrowseAPI(t)
	agent := createBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)
	setup.handler.dirSizeTimeout = time.Second
	setup.handler.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	w := postBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/dir-size", map[string]any{
		"path": "/home",
	})

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
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
