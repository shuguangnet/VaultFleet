package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	assert.NotNil(t, history.StartedAt)
	assert.Nil(t, history.FinishedAt)
}

func TestRestoreCarriesSourceSnapshotPolicy(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	storage := db.StorageConfig{Name: "source", RcloneType: "local"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		RepoPath:        "source/repository",
		BackupMode:      protocol.BackupModeSnapshot,
		BackupDirs:      `[]`,
		BackupSources:   `[]`,
		ExcludePatterns: `[]`,
		Retention:       `{}`,
		RcloneArgs:      `{}`,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)
	require.NoError(t, setup.database.DB.Create(&db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "backup",
		Status:     commands.TaskStatusSuccess,
		SnapshotID: "snap-source",
		PolicyID:   policy.ID,
		StorageID:  storage.ID,
		BackupMode: protocol.BackupModeSnapshot,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-source",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	require.NotNil(t, payload.Policy)
	assert.Equal(t, policy.ID, payload.Policy.PolicyID)
	assert.Equal(t, "source/repository", payload.Policy.Storage.RepoPath)
}

func TestRestoreUsesPolicyTimeoutHours(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "offline")
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	service := commands.NewService(setup.database, setup.hub)
	service.Now = func() time.Time { return now }
	setup.handler.Commands = service
	policy := db.BackupPolicy{
		AgentID:      agent.ID,
		StorageID:    "storage-1",
		RepoPath:     "repo/agent-1",
		TimeoutHours: 12,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	require.NotNil(t, command.DeadlineAt)
	assert.Equal(t, now.Add(12*time.Hour), *command.DeadlineAt)
	assert.Equal(t, policy.ID, command.PolicyID)
	assert.Equal(t, "storage-1", command.StorageID)
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

func TestRestoreToDifferentAgentUsesSourceSnapshotAndTargetsDestination(t *testing.T) {
	setup := setupRestoreAPI(t)
	sourceAgent := createRestoreTestAgent(t, setup.database, "online")
	targetAgent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[targetAgent.ID] = true
	snapshot := db.Snapshot{
		AgentID:    sourceAgent.ID,
		SnapshotID: "restic-source-snap",
		Timestamp:  time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC),
		Paths:      `["/srv/app"]`,
		Size:       1024,
	}
	require.NoError(t, setup.database.DB.Create(&snapshot).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+targetAgent.ID+"/restore", map[string]any{
		"source_agent_id": sourceAgent.ID,
		"snapshot_id":     snapshot.ID,
		"target_path":     "/restore/from-source",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, targetAgent.ID, setup.hub.sent[0].agentID)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, "restic-source-snap", payload.SnapshotID)
	assert.Equal(t, "/restore/from-source", payload.Target)

	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", data["command_id"]).Error)
	assert.Equal(t, targetAgent.ID, history.AgentID)
	assert.Equal(t, "restic-source-snap", history.SnapshotID)
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

func TestRestoreDockerContainerUsesBackupDockerMetadata(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{
		protocol.CapabilityRestoreIncludePaths,
		protocol.CapabilityDockerContainerRestore,
	})
	metadata := protocol.DockerBackupMetadata{
		Sources: []protocol.DockerResolvedSource{
			{
				ContainerID:   "container-1",
				Name:          "db",
				Image:         "postgres:16",
				ResolvedPaths: []string{"/srv/app/compose.yml", "/var/lib/docker/volumes/db/_data"},
				Compose:       protocol.DockerComposeInfo{Project: "app", Service: "db", WorkingDir: "/srv/app", ConfigFiles: []string{"compose.yml"}},
				Mounts:        []protocol.DockerMount{{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db/_data", Destination: "/var/lib/postgresql/data", RW: true}},
			},
		},
	}
	rawDocker, err := json.Marshal(metadata)
	require.NoError(t, err)
	finishedAt := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	require.NoError(t, setup.database.DB.Create(&db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "backup",
		Status:     commands.TaskStatusSuccess,
		SnapshotID: "snap-1",
		Docker:     string(rawDocker),
		FinishedAt: &finishedAt,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id":      "snap-1",
		"restore_mode":     protocol.RestoreModeDockerContainer,
		"docker_source_id": "container-1",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, protocol.TypeSelectiveRestoreReq, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, protocol.RestoreModeDockerContainer, payload.RestoreMode)
	assert.Equal(t, "/", payload.Target)
	assert.Equal(t, []string{"/srv/app/compose.yml", "/var/lib/docker/volumes/db/_data"}, payload.IncludePaths)
	require.NotNil(t, payload.Docker)
	require.Len(t, payload.Docker.Sources, 1)
	assert.Equal(t, "container-1", payload.Docker.Sources[0].ContainerID)
	assert.Equal(t, "postgres:16", payload.Docker.Sources[0].Image)
}

func TestRestoreDockerContainerToDifferentAgentUsesSourceMetadata(t *testing.T) {
	setup := setupRestoreAPI(t)
	sourceAgent := createRestoreTestAgent(t, setup.database, "online")
	targetAgent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[targetAgent.ID] = true
	markAgentCapabilities(t, setup.database, targetAgent.ID, []string{
		protocol.CapabilityRestoreIncludePaths,
		protocol.CapabilityDockerContainerRestore,
	})
	metadata := protocol.DockerBackupMetadata{
		Sources: []protocol.DockerResolvedSource{
			{
				ContainerID:   "container-1",
				Name:          "db",
				Image:         "postgres:16",
				ResolvedPaths: []string{"/var/lib/docker/volumes/db/_data"},
				Mounts:        []protocol.DockerMount{{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db/_data", Destination: "/var/lib/postgresql/data", RW: true}},
			},
		},
	}
	rawDocker, err := json.Marshal(metadata)
	require.NoError(t, err)
	finishedAt := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	require.NoError(t, setup.database.DB.Create(&db.TaskHistory{
		AgentID:    sourceAgent.ID,
		Type:       "backup",
		Status:     commands.TaskStatusSuccess,
		SnapshotID: "snap-1",
		Docker:     string(rawDocker),
		FinishedAt: &finishedAt,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+targetAgent.ID+"/restore", map[string]any{
		"source_agent_id":  sourceAgent.ID,
		"snapshot_id":      "snap-1",
		"restore_mode":     protocol.RestoreModeDockerContainer,
		"docker_source_id": "container-1",
	})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, targetAgent.ID, setup.hub.sent[0].agentID)
	assert.Equal(t, protocol.TypeSelectiveRestoreReq, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.RestoreReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, protocol.RestoreModeDockerContainer, payload.RestoreMode)
	assert.Equal(t, []string{"/var/lib/docker/volumes/db/_data"}, payload.IncludePaths)
	require.NotNil(t, payload.Docker)
	require.Len(t, payload.Docker.Sources, 1)
	assert.Equal(t, "container-1", payload.Docker.Sources[0].ContainerID)
}

func TestRestoreDockerContainerRejectsMissingMetadata(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{
		protocol.CapabilityRestoreIncludePaths,
		protocol.CapabilityDockerContainerRestore,
	})

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore", map[string]any{
		"snapshot_id":  "snap-1",
		"restore_mode": protocol.RestoreModeDockerContainer,
		"docker": map[string]any{
			"sources": []map[string]any{
				{"container_id": "client-supplied", "resolved_paths": []string{"/etc"}},
			},
		},
	})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "docker metadata not found for snapshot", body["error"])
	assert.Empty(t, setup.hub.sent)
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

func TestRestorePreflightSuccessDoesNotCreateCommandOrTask(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{
		protocol.CapabilityRestorePreflight,
		protocol.CapabilityRestoreIncludePaths,
	})
	snapshot := db.Snapshot{
		AgentID:    agent.ID,
		SnapshotID: "restic-snap-1",
		Timestamp:  time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC),
		Paths:      `["/data"]`,
	}
	require.NoError(t, setup.database.DB.Create(&snapshot).Error)
	resp, err := protocol.NewMessage(protocol.TypeRestorePreflightResp, protocol.RestorePreflightRespPayload{
		AgentID:    agent.ID,
		SnapshotID: "restic-snap-1",
		Status:     protocol.RestorePreflightStatusPassed,
		Checks:     []protocol.RestorePreflightCheck{{Code: "target_path_writable", Severity: protocol.RestorePreflightSeverityInfo, Message: "target path is writable"}},
	})
	require.NoError(t, err)
	setup.hub.waitResp = resp

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id":   snapshot.ID,
		"target_path":   "/restore/target",
		"include_paths": []string{"/data/file.txt"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	assert.Equal(t, protocol.RestorePreflightStatusPassed, data["status"])
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, protocol.TypeRestorePreflightReq, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.RestorePreflightReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, "restic-snap-1", payload.SnapshotID)
	assert.Equal(t, "/restore/target", payload.Target)
	assert.Equal(t, []string{"/data/file.txt"}, payload.IncludePaths)

	var commandCount int64
	require.NoError(t, setup.database.DB.Model(&db.AgentCommand{}).Count(&commandCount).Error)
	assert.Equal(t, int64(0), commandCount)
	var taskCount int64
	require.NoError(t, setup.database.DB.Model(&db.TaskHistory{}).Count(&taskCount).Error)
	assert.Equal(t, int64(0), taskCount)
}

func TestRestorePreflightRejectsMissingSnapshotWithoutDispatch(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityRestorePreflight})

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id": "missing-snap",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "source_snapshot_found", protocol.RestorePreflightSeverityError)
	assert.Empty(t, setup.hub.sent)
}

func TestRestorePreflightRejectsOfflineTarget(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "target_agent_online", protocol.RestorePreflightSeverityError)
	assert.Empty(t, setup.hub.sent)
}

func TestRestorePreflightRejectsMissingPreflightCapability(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityRestoreIncludePaths})

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "restore_preflight_capability", protocol.RestorePreflightSeverityError)
	assert.Empty(t, setup.hub.sent)
}

func TestRestorePreflightRejectsMissingSelectiveRestoreCapability(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityRestorePreflight})
	require.NoError(t, setup.database.DB.Create(&db.Snapshot{
		AgentID:    agent.ID,
		SnapshotID: "snap-1",
		Timestamp:  time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC),
		Paths:      `["/data"]`,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id":   "snap-1",
		"target_path":   "/restore/target",
		"include_paths": []string{"/data/file.txt"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "restore_include_paths_capability", protocol.RestorePreflightSeverityError)
	assert.Empty(t, setup.hub.sent)
}

func TestRestorePreflightRejectsMissingDockerMetadata(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{
		protocol.CapabilityRestorePreflight,
		protocol.CapabilityRestoreIncludePaths,
		protocol.CapabilityDockerContainerRestore,
	})
	require.NoError(t, setup.database.DB.Create(&db.Snapshot{
		AgentID:    agent.ID,
		SnapshotID: "snap-1",
		Timestamp:  time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC),
		Paths:      `["/var/lib/docker/volumes/db/_data"]`,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id":  "snap-1",
		"restore_mode": protocol.RestoreModeDockerContainer,
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "docker_metadata", protocol.RestorePreflightSeverityError)
	assert.Empty(t, setup.hub.sent)
}

func TestRestorePreflightMergesAgentFailure(t *testing.T) {
	setup := setupRestoreAPI(t)
	agent := createRestoreTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityRestorePreflight})
	require.NoError(t, setup.database.DB.Create(&db.Snapshot{
		AgentID:    agent.ID,
		SnapshotID: "snap-1",
		Timestamp:  time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC),
		Paths:      `["/data"]`,
	}).Error)
	resp, err := protocol.NewMessage(protocol.TypeRestorePreflightResp, protocol.RestorePreflightRespPayload{
		AgentID:    agent.ID,
		SnapshotID: "snap-1",
		Status:     protocol.RestorePreflightStatusFailed,
		Checks:     []protocol.RestorePreflightCheck{{Code: "target_path_writable", Severity: protocol.RestorePreflightSeverityError, Message: "target path is not writable"}},
	})
	require.NoError(t, err)
	setup.hub.waitResp = resp

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/restore/preflight", map[string]any{
		"snapshot_id": "snap-1",
		"target_path": "/restore/target",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertPreflightData(t, w, protocol.RestorePreflightStatusFailed, "target_path_writable", protocol.RestorePreflightSeverityError)
	require.Len(t, setup.hub.sent, 1)
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
	waitErr          error
	waitResp         *protocol.Message
	waitClosed       bool
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

func (h *fakeRestoreHub) SendAndWait(agentID string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
	if h.waitErr != nil {
		return nil, h.waitErr
	}
	if err := h.Send(agentID, msg); err != nil {
		return nil, err
	}
	ch := make(chan protocol.Message, 1)
	if h.waitClosed {
		close(ch)
		return ch, nil
	}
	if h.waitResp != nil {
		resp := *h.waitResp
		resp.ID = msg.ID
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func createRestoreTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Restore Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func assertPreflightData(t *testing.T, w *httptest.ResponseRecorder, status string, code string, severity string) {
	t.Helper()
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	assert.Equal(t, status, data["status"])
	checks, ok := data["checks"].([]any)
	require.True(t, ok, "checks missing from preflight response: %#v", data)
	for _, rawCheck := range checks {
		check := requireMap(t, rawCheck)
		if check["code"] == code && check["severity"] == severity {
			return
		}
	}
	t.Fatalf("preflight check %s with severity %s not found in %#v", code, severity, checks)
}
