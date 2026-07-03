package api

import (
	"encoding/json"
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

func TestDockerBackupProfileCreatesPolicyAndQueuesBackup(t *testing.T) {
	setup := setupDockerAPI(t)
	agent := createDockerTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	storage := db.StorageConfig{Name: "S3", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/docker/backup-profile", map[string]any{
		"storage_id": storage.ID,
		"run_now":    true,
		"containers": []map[string]any{{
			"id": "abc123", "name": "web", "image": "nginx", "status": "running",
			"mounts": []map[string]any{{"type": "bind", "source": "/srv/web", "destination": "/data", "rw": true}},
		}},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	assert.NotEmpty(t, data["policy_id"])
	assert.NotNil(t, data["backup_command"])

	var policy db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&policy, "id = ?", data["policy_id"]).Error)
	assert.Contains(t, policy.BackupDirs, "/srv/web")
	assert.Contains(t, policy.PreBackupHook, "manifest.json")

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeBackupNow).Error)
	assert.Equal(t, commands.CommandStatusRunning, command.Status)
	assert.Len(t, setup.hub.sent, 1)
}

func TestDockerRestoreQueuesDockerRestoreCommand(t *testing.T) {
	setup := setupDockerAPI(t)
	agent := createDockerTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/docker/restore", map[string]any{
		"snapshot_id":      "snap-1",
		"target_path":      "/restore/docker",
		"start_containers": false,
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ?", agent.ID).Error)
	assert.Equal(t, protocol.TypeDockerRestoreReq, command.Type)
	assert.Equal(t, commands.CommandStatusPending, command.Status)

	plain, err := db.Decrypt(command.Payload, setup.database.MasterKey)
	require.NoError(t, err)
	var msg protocol.Message
	require.NoError(t, json.Unmarshal([]byte(plain), &msg))
	payload, err := protocol.ParsePayload[protocol.DockerRestoreReqPayload](&msg)
	require.NoError(t, err)
	assert.Equal(t, "snap-1", payload.SnapshotID)
	assert.Equal(t, "/restore/docker", payload.Target)
}

type dockerAPISetup struct {
	database *db.Database
	hub      *fakeDockerHub
	router   *gin.Engine
}

func setupDockerAPI(t *testing.T) dockerAPISetup {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	hub := &fakeDockerHub{online: map[string]bool{}}
	commandService := commands.NewService(database, hub)
	handler := NewDockerHandler(database, hub, commandService, nil)
	router := gin.New()
	RegisterDockerRoutes(router.Group("/api"), handler)
	return dockerAPISetup{database: database, hub: hub, router: router}
}

func createDockerTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()
	agent := db.Agent{Name: "Docker Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	markAgentCapabilities(t, database, agent.ID, []string{protocol.CapabilityDockerBackup, protocol.CapabilityPolicyPlaintextRclonePass})
	return agent
}

type fakeDockerHub struct {
	online map[string]bool
	sent   []sentCommandMessage
}

func (h *fakeDockerHub) IsOnline(agentID string) bool { return h.online[agentID] }

func (h *fakeDockerHub) Send(agentID string, msg interface{}) error {
	message, _ := msg.(protocol.Message)
	h.sent = append(h.sent, sentCommandMessage{agentID: agentID, message: message})
	return nil
}

func (h *fakeDockerHub) SendAndWait(agentID string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
	ch := make(chan protocol.Message)
	close(ch)
	return ch, nil
}
