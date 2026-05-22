package api

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/commands"
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

func TestRestoreOfflineQueuesCommandAndTask(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "restore queued", body["message"])
	data := requireMap(t, body["data"])
	assert.Equal(t, "restore queued", data["message"])
	assert.NotEmpty(t, data["command_id"])
	assert.NotEmpty(t, data["message_id"])
	assert.Empty(t, setup.hub.sent)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeRestoreReq, command.Type)
	assert.Equal(t, commands.CommandStatusPending, command.Status)
	assert.Equal(t, data["message_id"], command.MessageID)
	assert.Equal(t, 0, command.Attempts)
	assert.Nil(t, command.DispatchedAt)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, agent.ID, history.AgentID)
	assert.Equal(t, "restore", history.Type)
	assert.Equal(t, commands.TaskStatusPending, history.Status)
	assert.Equal(t, "snap-1", history.SnapshotID)
	assert.Equal(t, command.ID, history.CommandID)
	assert.Equal(t, data["message_id"], history.MessageID)
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
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "restore started", body["message"])
	data := requireMap(t, body["data"])
	assert.Equal(t, "restore started", data["message"])
	assert.NotEmpty(t, data["command_id"])
	messageID, ok := body["message_id"].(string)
	require.True(t, ok)
	assert.Equal(t, messageID, data["message_id"])
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

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeRestoreReq, command.Type)
	assert.Equal(t, commands.CommandStatusRunning, command.Status)
	assert.Equal(t, messageID, command.MessageID)
	assert.Equal(t, 1, command.Attempts)
	assert.NotNil(t, command.DispatchedAt)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, "restore", history.Type)
	assert.Equal(t, commands.TaskStatusRunning, history.Status)
	assert.Equal(t, messageID, history.MessageID)
	assert.Equal(t, "snap-1", history.SnapshotID)
	assert.Equal(t, command.ID, history.CommandID)
	assert.Nil(t, history.StartedAt)
	assert.Nil(t, history.FinishedAt)
}

func TestRestoreResolvesDatabaseSnapshotIDToResticSnapshotID(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	pathsJSON := `["/etc"]`
	snapshot := db.Snapshot{
		AgentID:    agent.ID,
		SnapshotID: "restic-snap-1",
		Timestamp:  snapshotTime,
		Paths:      pathsJSON,
		Size:       512,
	}
	require.NoError(t, setup.database.DB.Create(&snapshot).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": snapshot.ID,
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, "restic-snap-1", payload.SnapshotID)

	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", data["command_id"]).Error)
	assert.Equal(t, "restic-snap-1", history.SnapshotID)
}

func TestRestoreAcceptsAcceptanceTargetAlias(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target":      "/opt/vaultfleet-restore",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, "/opt/vaultfleet-restore", payload.Target)
}

func TestRestoreWithIncludePathsPassesThemInPayload(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markRestoreCapability(t, setup.database, agent.ID)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id":   "snap-1",
		"target_path":   "/restore/target",
		"include_paths": []string{"/etc/hosts", "/var/log/app.log"},
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, protocol.TypeSelectiveRestoreReq, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, []string{"/etc/hosts", "/var/log/app.log"}, payload.IncludePaths)
}

func TestRestoreWithIncludePathsRejectsAgentWithoutCapability(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id":   "snap-1",
		"target_path":   "/restore/target",
		"include_paths": []string{"/etc/hosts"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent does not support selective restore", body["error"])
	assert.Empty(t, setup.hub.sent)

	var count int64
	require.NoError(t, setup.database.DB.Model(&db.AgentCommand{}).Where("agent_id = ?", agent.ID).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestRestoreRecordsPendingCommandAndTaskBeforeSendingMessage(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.beforeSend = func() {
		var history db.TaskHistory
		require.NoError(t, setup.database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
		assert.Equal(t, "restore", history.Type)
		assert.Equal(t, commands.TaskStatusRunning, history.Status)

		var command db.AgentCommand
		require.NoError(t, setup.database.DB.First(&command, "id = ?", history.CommandID).Error)
		assert.Equal(t, protocol.TypeRestoreReq, command.Type)
		assert.Equal(t, commands.CommandStatusRunning, command.Status)
		assert.Equal(t, 1, command.Attempts)
		assert.NotNil(t, command.DispatchedAt)
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.True(t, setup.hub.beforeSendCalled)
}

func TestRestoreSendFailureLeavesCommandQueued(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendErr = errors.New("websocket write failed")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "restore queued", body["message"])
	data := requireMap(t, body["data"])
	assert.Equal(t, "restore queued", data["message"])

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, commands.CommandStatusPending, command.Status)
	assert.Equal(t, 1, command.Attempts)
	assert.Nil(t, command.DispatchedAt)
	assert.Contains(t, command.ErrorMessage, "websocket write failed")

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, "restore", history.Type)
	assert.Equal(t, commands.TaskStatusPending, history.Status)
	assert.Nil(t, history.StartedAt)
	assert.Nil(t, history.FinishedAt)
	assert.Empty(t, history.ErrorLog)
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
	commandService := commands.NewService(database, hub)
	handler := NewRestoreHandler(database, hub)
	handler.Commands = commandService
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
