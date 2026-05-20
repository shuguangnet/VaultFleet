package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestGetCommandRedactsPayload(t *testing.T) {
	setup := setupCommandsAPI(t)
	agent := createCommandsTestAgent(t, setup.database, "online")
	command := createAPICommand(t, setup.service, agent.ID, protocol.TypeBackupNow, commands.CommandStatusPending)

	w := getJSON(t, setup.router, "/api/commands/"+command.ID)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data := requireMap(t, body["data"])
	assert.Equal(t, command.ID, data["id"])
	assert.Equal(t, agent.ID, data["agent_id"])
	assert.Equal(t, protocol.TypeBackupNow, data["type"])
	assert.Equal(t, commands.CommandStatusPending, data["status"])
	assert.Equal(t, command.MessageID, data["message_id"])
	assert.NotContains(t, data, "payload")
}

func TestGetCommandReturnsNotFoundForMissingCommand(t *testing.T) {
	setup := setupCommandsAPI(t)

	w := getJSON(t, setup.router, "/api/commands/missing")

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "command not found", body["error"])
}

func TestListAgentCommandsFiltersStatusAndLimit(t *testing.T) {
	setup := setupCommandsAPI(t)
	agent := createCommandsTestAgent(t, setup.database, "online")
	first := createAPICommand(t, setup.service, agent.ID, protocol.TypeBackupNow, commands.CommandStatusPending)
	second := createAPICommand(t, setup.service, agent.ID, protocol.TypeRestoreReq, commands.CommandStatusPending)
	_ = createAPICommand(t, setup.service, agent.ID, protocol.TypePolicyPush, commands.CommandStatusDispatched)
	require.NoError(t, setup.database.DB.Model(&db.AgentCommand{}).Where("id = ?", first.ID).Update("created_at", first.CreatedAt.Add(-1)).Error)

	w := getJSON(t, setup.router, "/api/agents/"+agent.ID+"/commands?status=pending&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	item := requireMap(t, data[0])
	assert.Equal(t, second.ID, item["id"])
	assert.Equal(t, commands.CommandStatusPending, item["status"])
	assert.NotContains(t, item, "payload")
}

func TestListAgentCommandsValidatesAgentExists(t *testing.T) {
	setup := setupCommandsAPI(t)

	w := getJSON(t, setup.router, "/api/agents/missing/commands")

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "agent not found", body["error"])
}

func TestListAgentCommandsFiltersType(t *testing.T) {
	setup := setupCommandsAPI(t)
	agent := createCommandsTestAgent(t, setup.database, "online")
	_ = createAPICommand(t, setup.service, agent.ID, protocol.TypeBackupNow, commands.CommandStatusPending)
	restoreCommand := createAPICommand(t, setup.service, agent.ID, protocol.TypeRestoreReq, commands.CommandStatusPending)

	w := getJSON(t, setup.router, "/api/agents/"+agent.ID+"/commands?type="+protocol.TypeRestoreReq)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	item := requireMap(t, data[0])
	assert.Equal(t, restoreCommand.ID, item["id"])
	assert.Equal(t, protocol.TypeRestoreReq, item["type"])
}

type commandsAPISetup struct {
	database *db.Database
	service  *commands.Service
	router   *gin.Engine
}

func setupCommandsAPI(t *testing.T) commandsAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	service := commands.NewService(database, &fakeCommandHub{online: map[string]bool{}})
	handler := NewCommandHandler(database)
	router := gin.New()
	RegisterCommandRoutes(router.Group("/api"), handler)

	return commandsAPISetup{database: database, service: service, router: router}
}

func createCommandsTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Command Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func createAPICommand(t *testing.T, service *commands.Service, agentID string, msgType string, status string) db.AgentCommand {
	t.Helper()

	var payload any
	input := commands.CreateCommandInput{
		AgentID:   agentID,
		Type:      msgType,
		TaskState: commands.TaskStatusPending,
	}
	switch msgType {
	case protocol.TypeRestoreReq:
		payload = protocol.RestoreReqPayload{SnapshotID: "snap-1", Target: "/restore"}
		input.TaskType = "restore"
		input.SnapshotID = "snap-1"
	case protocol.TypePolicyPush:
		payload = protocol.PolicyPushPayload{AgentID: agentID}
	default:
		payload = protocol.BackupNowPayload{AgentID: agentID}
		input.TaskType = "backup"
	}
	msg, err := protocol.NewMessage(msgType, payload)
	require.NoError(t, err)
	input.Message = *msg

	command, err := service.CreateCommand(context.Background(), input)
	require.NoError(t, err)
	if status != command.Status {
		require.NoError(t, service.DB.DB.Model(&db.AgentCommand{}).Where("id = ?", command.ID).Update("status", status).Error)
		require.NoError(t, service.DB.DB.First(&command, "id = ?", command.ID).Error)
	}
	return command
}
