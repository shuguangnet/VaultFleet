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

func TestSnapshotBrowseHappyPath(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "online")
	markSnapshotBrowseCapability(t, setup.database, agent.ID)
	setup.hub.Add(agent.ID, nil)

	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotBrowseReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.SnapshotBrowseReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, "snap-1", req.SnapshotID)

		resp, err := protocol.NewMessage(protocol.TypeSnapshotBrowseResp, protocol.SnapshotBrowseRespPayload{
			SnapshotID: "snap-1",
			Entries: []protocol.SnapshotFileEntry{
				{Path: "/etc/hosts", Type: "file", Size: 64},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID

		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	raw := parseJSON(t, w)
	assert.Equal(t, true, raw["ok"])
	data, err := json.Marshal(raw["data"])
	require.NoError(t, err)
	var body protocol.SnapshotBrowseRespPayload
	require.NoError(t, json.Unmarshal(data, &body))
	assert.Equal(t, "snap-1", body.SnapshotID)
	assert.Equal(t, []protocol.SnapshotFileEntry{{Path: "/etc/hosts", Type: "file", Size: 64}}, body.Entries)
}

func TestSnapshotBrowseOffline(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "offline")

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "agent offline", body["error"])
}

func TestSnapshotBrowseMissingAgent(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/missing/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSnapshotBrowseRequiresSnapshotID(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "online")
	markSnapshotBrowseCapability(t, setup.database, agent.ID)
	setup.hub.Add(agent.ID, nil)

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSnapshotBrowseTimeout(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "online")
	markSnapshotBrowseCapability(t, setup.database, agent.ID)
	setup.hub.Add(agent.ID, nil)
	setup.handler.timeout = time.Second
	setup.handler.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "timeout waiting for agent response", body["error"])
}

func TestSnapshotBrowseAgentErrorPayload(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "online")
	markSnapshotBrowseCapability(t, setup.database, agent.ID)
	setup.hub.Add(agent.ID, nil)
	setup.handler.sendAndWait = func(_ string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
		resp, err := protocol.NewMessage(protocol.TypeSnapshotBrowseResp, protocol.SnapshotBrowseRespPayload{
			SnapshotID: "snap-1",
			Error:      "snapshot not found",
		})
		require.NoError(t, err)
		resp.ID = msg.ID

		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "snapshot not found", body["error"])
}

func TestSnapshotBrowseRejectsAgentWithoutCapability(t *testing.T) {
	setup := setupSnapshotBrowseAPI(t)
	agent := createSnapshotBrowseAgent(t, setup.database, "online")
	setup.hub.Add(agent.ID, nil)
	setup.handler.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		t.Fatal("sendAndWait should not be called for unsupported agents")
		return nil, nil
	}

	w := postSnapshotBrowseJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshot-browse", map[string]any{
		"snapshot_id": "snap-1",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent does not support snapshot browse", body["error"])
}

type snapshotBrowseAPISetup struct {
	database *db.Database
	hub      *ws.Hub
	handler  *SnapshotBrowseHandler
	router   *gin.Engine
}

func setupSnapshotBrowseAPI(t *testing.T) snapshotBrowseAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := ws.NewHub()
	handler := NewSnapshotBrowseHandler(database, hub)
	router := gin.New()
	RegisterSnapshotBrowseRoutes(router.Group("/api"), handler)

	return snapshotBrowseAPISetup{
		database: database,
		hub:      hub,
		handler:  handler,
		router:   router,
	}
}

func createSnapshotBrowseAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Snapshot Browse Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func postSnapshotBrowseJSON(t *testing.T, router http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func markSnapshotBrowseCapability(t *testing.T, database *db.Database, agentID string) {
	t.Helper()
	markAgentCapabilities(t, database, agentID, []string{protocol.CapabilitySnapshotBrowse})
}

func markRestoreCapability(t *testing.T, database *db.Database, agentID string) {
	t.Helper()
	markAgentCapabilities(t, database, agentID, []string{protocol.CapabilityRestoreIncludePaths})
}

func markAgentCapabilities(t *testing.T, database *db.Database, agentID string, capabilities []string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"capabilities": capabilities})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Update("system_info", string(data)).Error)
}
