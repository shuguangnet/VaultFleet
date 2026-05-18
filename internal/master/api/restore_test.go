package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestRestoreMissingAgent(t *testing.T) {
	setup := setupRestoreAPI(t)

	w := postAnyJSON(t, setup.router, "/api/agents/missing/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore",
	})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRestoreValidation(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"target_path": "/restore",
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRestoreOffline(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore",
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
}

func TestRestoreSendsMessageAndRecordsRunningTask(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "restore started", body["message"])
	messageID, ok := body["message_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, messageID)

	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, agent.ID, setup.hub.sent[0].agentID)
	sent := setup.hub.sent[0].message
	assert.Equal(t, protocol.TypeRestoreReq, sent.Type)
	assert.Equal(t, messageID, sent.ID)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&sent)
	require.NoError(t, err)
	assert.Equal(t, "snap-1", payload.SnapshotID)
	assert.Equal(t, "/restore/target", payload.Target)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
	assert.Equal(t, "restore", history.Type)
	assert.Equal(t, "running", history.Status)
	assert.NotNil(t, history.StartedAt)
	assert.Nil(t, history.FinishedAt)
}

func TestRestoreRecordsRunningTaskBeforeSendingMessage(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.beforeSend = func() {
		var history db.TaskHistory
		require.NoError(t, setup.database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
		assert.Equal(t, "restore", history.Type)
		assert.Equal(t, "running", history.Status)
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.True(t, setup.hub.beforeSendCalled)
}

func TestRestoreSendFailureMarksTaskFailed(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendErr = errors.New("websocket write failed")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusBadGateway, w.Code)
	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
	assert.Equal(t, "restore", history.Type)
	assert.Equal(t, "failed", history.Status)
	assert.NotNil(t, history.StartedAt)
	assert.NotNil(t, history.FinishedAt)
	assert.Contains(t, history.ErrorLog, "websocket write failed")
}

type restoreAPISetup struct {
	database *db.Database
	hub      *fakeRestoreHub
	handler  *RestoreHandler
	router   *gin.Engine
}

func setupRestoreAPI(t *testing.T) restoreAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := &fakeRestoreHub{online: map[string]bool{}}
	handler := NewRestoreHandler(database, hub)
	router := gin.New()
	RegisterRestoreRoutes(router.Group("/api"), handler)

	return restoreAPISetup{database: database, hub: hub, handler: handler, router: router}
}

type sentRestoreMessage struct {
	agentID string
	message protocol.Message
}

type fakeRestoreHub struct {
	online           map[string]bool
	sendErr          error
	beforeSend       func()
	beforeSendCalled bool
	sent             []sentRestoreMessage
}

func (h *fakeRestoreHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *fakeRestoreHub) Send(agentID string, msg interface{}) error {
	if h.beforeSend != nil {
		h.beforeSendCalled = true
		h.beforeSend()
	}
	if h.sendErr != nil {
		return h.sendErr
	}
	message, ok := msg.(protocol.Message)
	if !ok {
		return errors.New("message is not protocol.Message")
	}
	h.sent = append(h.sent, sentRestoreMessage{agentID: agentID, message: message})
	return nil
}

func createRestoreTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Restore Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}
