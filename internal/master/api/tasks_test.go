package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestBackupNowSendsAgentCommand(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data := requireMap(t, body["data"])
	assert.NotEmpty(t, data["message_id"])
	assert.NotEmpty(t, data["command_id"])
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, agent.ID, setup.hub.sent[0].agentID)
	assert.Equal(t, protocol.TypeBackupNow, setup.hub.sent[0].message.Type)
	assert.Equal(t, data["message_id"], setup.hub.sent[0].message.ID)
	payload, err := protocol.ParsePayload[protocol.BackupNowPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, payload.AgentID)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeBackupNow, command.Type)
	assert.Equal(t, commands.CommandStatusRunning, command.Status)
	assert.Equal(t, data["message_id"], command.MessageID)
	assert.Equal(t, 1, command.Attempts)
	assert.NotNil(t, command.DispatchedAt)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, agent.ID, history.AgentID)
	assert.Equal(t, "backup", history.Type)
	assert.Equal(t, commands.TaskStatusRunning, history.Status)
	assert.Equal(t, command.ID, history.CommandID)
	assert.Equal(t, data["message_id"], history.MessageID)
}

func TestBackupNowDispatchesWhenOlderPendingCommandHasInvalidPayload(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	poisonCreatedAt := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	poison := db.AgentCommand{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Status:    commands.CommandStatusPending,
		MessageID: "poison-backup-now",
		Payload:   "legacy-invalid-payload",
		CreatedAt: poisonCreatedAt,
		UpdatedAt: poisonCreatedAt,
	}
	require.NoError(t, setup.database.DB.Create(&poison).Error)
	require.NoError(t, setup.database.DB.Create(&db.TaskHistory{
		AgentID:   agent.ID,
		Type:      "backup",
		Status:    commands.TaskStatusPending,
		MessageID: poison.MessageID,
		CommandID: poison.ID,
		CreatedAt: poisonCreatedAt,
		UpdatedAt: poisonCreatedAt,
	}).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data := requireMap(t, body["data"])
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, agent.ID, setup.hub.sent[0].agentID)
	assert.Equal(t, protocol.TypeBackupNow, setup.hub.sent[0].message.Type)
	assert.Equal(t, data["message_id"], setup.hub.sent[0].message.ID)

	var failed db.AgentCommand
	require.NoError(t, setup.database.DB.First(&failed, "id = ?", poison.ID).Error)
	assert.Equal(t, commands.CommandStatusFailed, failed.Status)
	assert.Contains(t, failed.ErrorMessage, "invalid command payload")

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, commands.CommandStatusRunning, command.Status)
	assert.Equal(t, 1, command.Attempts)
}

func TestBackupNowQueuesOfflineAgentCommand(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data := requireMap(t, body["data"])
	assert.NotEmpty(t, data["message_id"])
	assert.NotEmpty(t, data["command_id"])
	assert.Empty(t, setup.hub.sent)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeBackupNow, command.Type)
	assert.Equal(t, commands.CommandStatusPending, command.Status)
	assert.Equal(t, data["message_id"], command.MessageID)
	assert.Equal(t, 0, command.Attempts)
	assert.Nil(t, command.DispatchedAt)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, agent.ID, history.AgentID)
	assert.Equal(t, "backup", history.Type)
	assert.Equal(t, commands.TaskStatusPending, history.Status)
	assert.Equal(t, command.ID, history.CommandID)
	assert.Equal(t, data["message_id"], history.MessageID)
}

func TestVerifyPolicyNowCreatesAndDispatchesCommand(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	markTasksAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityBackupVerification})
	storage := db.StorageConfig{Name: "Verify Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	verificationRaw, err := json.Marshal(protocol.BackupVerificationSettings{
		Enabled:        true,
		SampleCount:    4,
		TimeoutMinutes: 30,
	})
	require.NoError(t, err)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		BackupMode:      protocol.BackupModeSnapshot,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3}`,
		Verification:    string(verificationRaw),
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	w := postAnyJSON(t, setup.router, "/api/policies/"+policy.ID+"/verify-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, protocol.TypeBackupVerifyReq, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.BackupVerifyReqPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, payload.AgentID)
	require.NotNil(t, payload.Verification)
	assert.Equal(t, 4, payload.Verification.SampleCount)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, protocol.TypeBackupVerifyReq, command.Type)
	assert.Equal(t, commands.CommandStatusRunning, command.Status)
	assert.Equal(t, policy.ID, command.PolicyID)
	assert.Equal(t, storage.ID, command.StorageID)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, "verify", history.Type)
	assert.Equal(t, commands.TaskStatusRunning, history.Status)
}

func TestVerifyPolicyNowRejectsArchivePolicy(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	markTasksAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityBackupVerification})
	storage := db.StorageConfig{Name: "Archive Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		BackupMode:      protocol.BackupModeArchive,
		ArchiveFormat:   protocol.ArchiveFormatZip,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3}`,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	w := postAnyJSON(t, setup.router, "/api/policies/"+policy.ID+"/verify-now", map[string]any{})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Empty(t, setup.hub.sent)
}

func TestBackupNowUsesPolicyTimeoutHours(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "offline")
	storage := db.StorageConfig{Name: "Timeout Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3}`,
		TimeoutHours:    2,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)
	now := time.Date(2026, 5, 27, 9, 0, 0, 0, time.UTC)
	setup.handler.Commands.Now = func() time.Time { return now }

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	require.NotNil(t, command.DeadlineAt)
	assert.Equal(t, now.Add(2*time.Hour), command.DeadlineAt.UTC())
	assert.Equal(t, policy.ID, command.PolicyID)
	assert.Equal(t, storage.ID, command.StorageID)
}

func TestBackupNowUsesLatestPolicyForAgent(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "offline")
	snapshotStorage := db.StorageConfig{Name: "Snapshot Storage", RcloneType: "s3"}
	archiveStorage := db.StorageConfig{Name: "Archive Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&snapshotStorage).Error)
	require.NoError(t, setup.database.DB.Create(&archiveStorage).Error)

	older := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       snapshotStorage.ID,
		BackupMode:      protocol.BackupModeSnapshot,
		ArchiveFormat:   protocol.ArchiveFormatTarGz,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 2 * * *",
		Retention:       `{"keep_last":3}`,
		TimeoutHours:    2,
	}
	require.NoError(t, setup.database.DB.Create(&older).Error)
	newer := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       archiveStorage.ID,
		BackupMode:      protocol.BackupModeArchive,
		ArchiveFormat:   protocol.ArchiveFormatZip,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc","/var/lib/app"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":5}`,
		TimeoutHours:    4,
	}
	require.NoError(t, setup.database.DB.Create(&newer).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireMap(t, body["data"])
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", data["command_id"]).Error)
	assert.Equal(t, newer.ID, command.PolicyID)
	assert.Equal(t, archiveStorage.ID, command.StorageID)

	var history db.TaskHistory
	require.NoError(t, setup.database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, newer.ID, history.PolicyID)
	assert.Equal(t, archiveStorage.ID, history.StorageID)
}

func TestBackupNowIncludesLatestPolicyPayload(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true

	storage := db.StorageConfig{Name: "Archive Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		BackupMode:      protocol.BackupModeArchive,
		ArchiveFormat:   protocol.ArchiveFormatZip,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":5}`,
		TimeoutHours:    4,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	payload, err := protocol.ParsePayload[protocol.BackupNowPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	require.NotNil(t, payload.Policy)
	assert.Equal(t, agent.ID, payload.Policy.AgentID)
	assert.Equal(t, protocol.BackupModeArchive, payload.Policy.BackupMode)
	assert.Equal(t, protocol.ArchiveFormatZip, payload.Policy.ArchiveFormat)

	var backupDirs []string
	require.NoError(t, json.Unmarshal([]byte(policy.BackupDirs), &backupDirs))
	assert.Equal(t, backupDirs, payload.Policy.BackupDirs)
}

func TestBackupNowIncludesDockerPolicySources(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	markTasksAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityDockerWorkloadBackups})
	setup.hub.online[agent.ID] = true

	storage := db.StorageConfig{Name: "Docker Storage", RcloneType: "s3"}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		BackupMode:      protocol.BackupModeSnapshot,
		RepoPath:        "vaultfleet/" + agent.ID,
		BackupDirs:      `["/etc"]`,
		BackupSources:   `[{"type":"path","path":"/etc"},{"type":"docker_container","docker_container":{"container_id":"container-1","name":"db","include_bind_mounts":true,"include_volumes":true,"include_compose_files":true}}]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":5}`,
		TimeoutHours:    4,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/backup-now", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	payload, err := protocol.ParsePayload[protocol.BackupNowPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	require.NotNil(t, payload.Policy)
	require.Len(t, payload.Policy.BackupSources, 2)
	assert.Equal(t, protocol.BackupSourceTypeDockerContainer, payload.Policy.BackupSources[1].Type)
	require.NotNil(t, payload.Policy.BackupSources[1].DockerContainer)
	assert.Equal(t, "container-1", payload.Policy.BackupSources[1].DockerContainer.ContainerID)
}

func TestCancelPendingTaskMarksCancelled(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Now()

	command := db.AgentCommand{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Status:    commands.CommandStatusPending,
		MessageID: "msg-1",
	}
	require.NoError(t, setup.database.DB.Create(&command).Error)

	history := db.TaskHistory{
		AgentID:   agent.ID,
		Type:      "backup",
		Status:    commands.TaskStatusPending,
		MessageID: "msg-1",
		CommandID: command.ID,
		CreatedAt: now,
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	var updated db.TaskHistory
	require.NoError(t, setup.database.DB.First(&updated, "id = ?", history.ID).Error)
	assert.Equal(t, commands.TaskStatusCancelled, updated.Status)
	assert.NotNil(t, updated.FinishedAt)
	assert.Empty(t, setup.hub.sent)
}

func TestCancelRunningTaskSendsWSMessage(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	now := time.Now()

	command := db.AgentCommand{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Status:    commands.CommandStatusRunning,
		MessageID: "msg-2",
	}
	require.NoError(t, setup.database.DB.Create(&command).Error)

	history := db.TaskHistory{
		AgentID:   agent.ID,
		Type:      "backup",
		Status:    commands.TaskStatusRunning,
		MessageID: "msg-2",
		CommandID: command.ID,
		CreatedAt: now,
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, setup.hub.sent, 1)
	assert.Equal(t, agent.ID, setup.hub.sent[0].agentID)
	assert.Equal(t, protocol.TypeCancelTask, setup.hub.sent[0].message.Type)
	payload, err := protocol.ParsePayload[protocol.CancelTaskPayload](&setup.hub.sent[0].message)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, payload.AgentID)
	assert.Equal(t, command.MessageID, payload.MessageID)
}

func TestCancelRunningTaskReturnsUnavailableWhenHubMissing(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	history := seedCommandBackedTask(t, setup.database, agent.ID, commands.CommandStatusRunning, commands.TaskStatusRunning, "msg-no-hub")
	setup.handler.Hub = nil
	setup.handler.Commands.Hub = nil

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCancelRunningTaskReturnsUnavailableWhenAgentOffline(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	history := seedCommandBackedTask(t, setup.database, agent.ID, commands.CommandStatusRunning, commands.TaskStatusRunning, "msg-offline")

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, setup.hub.sent)
}

func TestCancelRunningTaskReturnsUnavailableWhenSendFails(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendErr = errors.New("websocket write failed")
	history := seedCommandBackedTask(t, setup.database, agent.ID, commands.CommandStatusRunning, commands.TaskStatusRunning, "msg-send-fails")

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, setup.hub.sent)
}

func TestCancelCompletedTaskReturnsConflict(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Now()
	history := seedTaskHistory(t, setup.database, agent.ID, "backup", commands.TaskStatusSuccess, "snap-1", now)

	w := postAnyJSON(t, setup.router, "/api/tasks/"+history.ID+"/cancel", nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Empty(t, setup.hub.sent)
}

func TestDownloadArtifactResolvesRelativePathFromDataDir(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	artifactDir := filepath.Join(setup.database.DataDir, "artifacts")
	require.NoError(t, os.MkdirAll(artifactDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "archive.tar.gz"), []byte("archive-bytes"), 0o644))
	history := db.TaskHistory{
		AgentID:             agent.ID,
		Type:                "backup",
		Status:              commands.TaskStatusSuccess,
		ArtifactPath:        filepath.Join("artifacts", "archive.tar.gz"),
		ArtifactName:        "archive.tar.gz",
		ArtifactContentType: "application/gzip",
		CreatedAt:           time.Now(),
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := getJSON(t, setup.router, "/api/tasks/"+history.ID+"/download")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "archive-bytes", w.Body.String())
	assert.Equal(t, "application/gzip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), `filename="archive.tar.gz"`)
}

func TestDownloadArtifactReturnsNotFoundWhenResolvedFileMissing(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	history := db.TaskHistory{
		AgentID:      agent.ID,
		Type:         "backup",
		Status:       commands.TaskStatusSuccess,
		ArtifactPath: filepath.Join("artifacts", "missing.tar.gz"),
		ArtifactName: "missing.tar.gz",
		CreatedAt:    time.Now(),
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := getJSON(t, setup.router, "/api/tasks/"+history.ID+"/download")

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "task artifact not found", body["error"])
}

func TestDownloadArtifactRepairsLegacyFlatArtifactPath(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	legacyDir := filepath.Join(setup.database.DataDir, "artifacts")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	legacyPath := filepath.Join(legacyDir, "archive.tar.gz")
	require.NoError(t, os.WriteFile(legacyPath, []byte("legacy-archive"), 0o644))

	history := db.TaskHistory{
		AgentID:             agent.ID,
		Type:                "backup",
		Status:              commands.TaskStatusSuccess,
		ArtifactPath:        filepath.Join("artifacts", "archive.tar.gz"),
		ArtifactName:        "archive.tar.gz",
		ArtifactContentType: "application/gzip",
		CreatedAt:           time.Now(),
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := getJSON(t, setup.router, "/api/tasks/"+history.ID+"/download")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "legacy-archive", w.Body.String())

	var updated db.TaskHistory
	require.NoError(t, setup.database.DB.First(&updated, "id = ?", history.ID).Error)
	expectedRel := filepath.ToSlash(filepath.Join("artifacts", agent.ID, "archive.tar.gz"))
	assert.Equal(t, expectedRel, updated.ArtifactPath)
	storedBytes, err := os.ReadFile(filepath.Join(setup.database.DataDir, filepath.FromSlash(expectedRel)))
	require.NoError(t, err)
	assert.Equal(t, "legacy-archive", string(storedBytes))
}

func TestDownloadArtifactFetchesRemoteArchiveIntoStorageCache(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	storage := db.StorageConfig{
		Name:         "Archive Storage",
		RcloneType:   "s3",
		RcloneConfig: `{"provider":"Minio","access_key_id":"[REDACTED]","secret_access_key":"[REDACTED]","endpoint":"https://example.invalid"}`,
	}
	require.NoError(t, setup.database.DB.Create(&storage).Error)
	policy := db.BackupPolicy{
		AgentID:         agent.ID,
		StorageID:       storage.ID,
		BackupMode:      protocol.BackupModeArchive,
		ArchiveFormat:   protocol.ArchiveFormatZip,
		RepoPath:        "tenant/agent-remote",
		BackupDirs:      `["/etc"]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":5}`,
	}
	require.NoError(t, setup.database.DB.Create(&policy).Error)

	remotePayload := "remote-archive-bytes"
	logPath := filepath.Join(t.TempDir(), "rclone.log")
	binDir := t.TempDir()
	rclonePath := filepath.Join(binDir, "rclone")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shQuote(logPath) + "\n" +
		"if [ \"$3\" = copyto ]; then\n" +
		"  mkdir -p \"$(dirname \"$5\")\"\n" +
		"  printf %s " + shQuote(remotePayload) + " > \"$5\"\n" +
		"fi\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(rclonePath, []byte(script), 0o755))
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	history := db.TaskHistory{
		AgentID:             agent.ID,
		Type:                "backup",
		Status:              commands.TaskStatusSuccess,
		ArtifactPath:        "artifacts/backup-remote.zip",
		ArtifactName:        "backup-remote.zip",
		ArtifactContentType: "application/zip",
		StorageID:           storage.ID,
		PolicyID:            policy.ID,
		BackupMode:          protocol.BackupModeArchive,
		ArchiveFormat:       protocol.ArchiveFormatZip,
		CreatedAt:           time.Now(),
	}
	require.NoError(t, setup.database.DB.Create(&history).Error)

	w := getJSON(t, setup.router, "/api/tasks/"+history.ID+"/download")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, remotePayload, w.Body.String())
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))

	var updated db.TaskHistory
	require.NoError(t, setup.database.DB.First(&updated, "id = ?", history.ID).Error)
	expectedRel := filepath.ToSlash(filepath.Join("artifacts", agent.ID, "backup-remote.zip"))
	assert.Equal(t, expectedRel, updated.ArtifactPath)
	assert.Equal(t, int64(len(remotePayload)), updated.ArtifactSize)
	cachedPath := filepath.Join(setup.database.DataDir, filepath.FromSlash(expectedRel))
	cachedBytes, err := os.ReadFile(cachedPath)
	require.NoError(t, err)
	assert.Equal(t, remotePayload, string(cachedBytes))
	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(logged), "copyto vaultfleet:tenant/agent-remote/artifacts/backup-remote.zip ")
}

func TestListTasksFiltersAndLimitsHistory(t *testing.T) {
	setup := setupTasksAPI(t)
	agentA := createTasksTestAgent(t, setup.database, "online")
	agentB := createTasksTestAgent(t, setup.database, "online")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	seedTaskHistory(t, setup.database, agentA.ID, "backup", "success", "snap-a-old", now.Add(-2*time.Hour))
	expected := seedTaskHistory(t, setup.database, agentA.ID, "backup", "failed", "snap-a-new", now)
	seedTaskHistory(t, setup.database, agentA.ID, "restore", "success", "snap-restore", now.Add(-time.Hour))
	seedTaskHistory(t, setup.database, agentB.ID, "backup", "success", "snap-b", now.Add(time.Hour))

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agentA.ID+"&type=backup&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data, ok := body["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	assert.Equal(t, agentA.ID, task["agent_id"])
	assert.Equal(t, "backup", task["type"])
	assert.Equal(t, "snap-a-new", task["snapshot_id"])
	assert.Equal(t, expected.CommandID, task["command_id"])
	assert.Equal(t, expected.PolicyID, task["policy_id"])
	assert.Equal(t, expected.StorageID, task["storage_id"])
	assert.NotEmpty(t, task["updated_at"])
}

func TestListTasksExposesRepositorySizeAndErrorAliases(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	finishedAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	history := seedTaskHistory(t, setup.database, agent.ID, "backup", "failed", "snap-failed", finishedAt)
	require.NoError(t, setup.database.DB.Model(&db.TaskHistory{}).
		Where("id = ?", history.ID).
		Updates(map[string]any{
			"repo_size": 4096,
			"error_log": "restic failed",
		}).Error)

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	assert.Equal(t, float64(4096), task["repo_size"])
	assert.Equal(t, float64(4096), task["repository_size_bytes"])
	assert.Equal(t, "restic failed", task["error_log"])
	assert.Equal(t, "restic failed", task["error"])
}

func TestListTasksAttachesProgressForRunningTask(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	history := seedTaskHistory(t, setup.database, agent.ID, "backup", commands.TaskStatusRunning, "snap-running", now)
	setup.handler.ProgressGetter = func(agentID string, messageID string) *protocol.BackupProgressPayload {
		require.Equal(t, agent.ID, agentID)
		require.Equal(t, history.MessageID, messageID)
		return &protocol.BackupProgressPayload{
			AgentID:     agentID,
			Phase:       "backup",
			PercentDone: 64.5,
			FilesDone:   8,
			TotalFiles:  12,
			BytesDone:   2048,
			TotalBytes:  4096,
			BytesPerSec: 1024,
			CurrentFile: "/srv/current.db",
		}
	}

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	progress := requireMap(t, task["progress"])
	assert.Equal(t, agent.ID, progress["agent_id"])
	assert.Equal(t, "backup", progress["phase"])
	assert.Equal(t, 64.5, progress["percent_done"])
	assert.Equal(t, float64(2048), progress["bytes_done"])
	assert.Equal(t, "/srv/current.db", progress["current_file"])
}

func TestListTasksAttachesProgressForPendingTask(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "offline")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	history := seedTaskHistory(t, setup.database, agent.ID, "backup", commands.TaskStatusPending, "snap-pending", now)
	setup.handler.ProgressGetter = func(agentID string, messageID string) *protocol.BackupProgressPayload {
		require.Equal(t, agent.ID, agentID)
		require.Equal(t, history.MessageID, messageID)
		return &protocol.BackupProgressPayload{
			AgentID:     agentID,
			Phase:       "queued",
			PercentDone: 0,
		}
	}

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	progress := requireMap(t, task["progress"])
	assert.Equal(t, "queued", progress["phase"])
}

func TestListTasksOmitsBackupProgressForRestoreTask(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	seedTaskHistory(t, setup.database, agent.ID, "restore", commands.TaskStatusRunning, "snap-restore", now)
	progressGetterCalled := false
	setup.handler.ProgressGetter = func(agentID string, messageID string) *protocol.BackupProgressPayload {
		progressGetterCalled = true
		return &protocol.BackupProgressPayload{
			AgentID: agentID,
			Phase:   "backup",
		}
	}

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	assert.NotContains(t, task, "progress")
	assert.False(t, progressGetterCalled, "restore tasks should not look up backup progress")
}

func TestListTasksOmitsProgressForCompletedTask(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	history := seedTaskHistory(t, setup.database, agent.ID, "backup", commands.TaskStatusSuccess, "snap-success", now)
	setup.handler.ProgressGetter = func(agentID string, messageID string) *protocol.BackupProgressPayload {
		require.Equal(t, agent.ID, agentID)
		require.Equal(t, history.MessageID, messageID)
		return &protocol.BackupProgressPayload{
			AgentID:     agentID,
			Phase:       "backup",
			PercentDone: 100,
		}
	}

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=1")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	task := requireMap(t, data[0])
	assert.NotContains(t, task, "progress")
}

func TestListTasksUsesMessageIDWhenAttachingBackupProgress(t *testing.T) {
	setup := setupTasksAPI(t)
	agent := createTasksTestAgent(t, setup.database, "online")
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	first := seedTaskHistoryWithMessageID(t, setup.database, agent.ID, "backup", commands.TaskStatusRunning, "snap-first", now.Add(time.Second), "backup-msg-1")
	second := seedTaskHistoryWithMessageID(t, setup.database, agent.ID, "backup", commands.TaskStatusRunning, "snap-second", now, "backup-msg-2")
	calls := map[string]int{}
	setup.handler.ProgressGetter = func(agentID string, messageID string) *protocol.BackupProgressPayload {
		require.Equal(t, agent.ID, agentID)
		calls[messageID]++
		if messageID != first.MessageID {
			return nil
		}
		return &protocol.BackupProgressPayload{
			AgentID:     agentID,
			Phase:       "backup",
			PercentDone: 25,
		}
	}

	w := getJSON(t, setup.router, "/api/tasks?agent_id="+agent.ID+"&limit=2")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 2)
	for _, item := range data {
		task := requireMap(t, item)
		switch task["message_id"] {
		case first.MessageID:
			progress := requireMap(t, task["progress"])
			assert.Equal(t, "backup", progress["phase"])
		case second.MessageID:
			assert.NotContains(t, task, "progress")
		default:
			t.Fatalf("unexpected task message_id %v", task["message_id"])
		}
	}
	assert.Equal(t, map[string]int{first.MessageID: 1, second.MessageID: 1}, calls)
}

type tasksAPISetup struct {
	database *db.Database
	hub      *fakeCommandHub
	handler  *TaskHandler
	router   *gin.Engine
}

func setupTasksAPI(t *testing.T) tasksAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := &fakeCommandHub{online: map[string]bool{}}
	commandService := commands.NewService(database, hub)
	handler := NewTaskHandler(database, hub)
	handler.Commands = commandService
	router := gin.New()
	RegisterTaskRoutes(router.Group("/api"), handler)

	return tasksAPISetup{database: database, hub: hub, handler: handler, router: router}
}

type sentCommandMessage struct {
	agentID string
	message protocol.Message
}

type fakeCommandHub struct {
	online  map[string]bool
	sendErr error
	sent    []sentCommandMessage
}

func (h *fakeCommandHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *fakeCommandHub) Send(agentID string, msg interface{}) error {
	if h.sendErr != nil {
		return h.sendErr
	}
	message, ok := msg.(protocol.Message)
	if !ok {
		return errors.New("message is not protocol.Message")
	}
	h.sent = append(h.sent, sentCommandMessage{agentID: agentID, message: message})
	return nil
}

func createTasksTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Task Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func markTasksAgentCapabilities(t *testing.T, database *db.Database, agentID string, capabilities []string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"capabilities": capabilities})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Update("system_info", string(data)).Error)
}

func seedTaskHistory(t *testing.T, database *db.Database, agentID string, taskType string, status string, snapshotID string, createdAt time.Time) db.TaskHistory {
	t.Helper()
	return seedTaskHistoryWithMessageID(t, database, agentID, taskType, status, snapshotID, createdAt, "msg-"+snapshotID)
}

func seedTaskHistoryWithMessageID(t *testing.T, database *db.Database, agentID string, taskType string, status string, snapshotID string, createdAt time.Time, messageID string) db.TaskHistory {
	t.Helper()

	startedAt := createdAt.Add(-time.Minute)
	finishedAt := createdAt
	history := db.TaskHistory{
		AgentID:    agentID,
		Type:       taskType,
		Status:     status,
		SnapshotID: snapshotID,
		MessageID:  messageID,
		CommandID:  "cmd-" + snapshotID,
		PolicyID:   "policy-" + snapshotID,
		StorageID:  "storage-" + snapshotID,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
		DurationMs: 60000,
		CreatedAt:  createdAt,
	}
	require.NoError(t, database.DB.Create(&history).Error)
	return history
}

func seedCommandBackedTask(t *testing.T, database *db.Database, agentID string, commandStatus string, taskStatus string, messageID string) db.TaskHistory {
	t.Helper()

	command := db.AgentCommand{
		AgentID:   agentID,
		Type:      protocol.TypeBackupNow,
		Status:    commandStatus,
		MessageID: messageID,
	}
	require.NoError(t, database.DB.Create(&command).Error)
	history := db.TaskHistory{
		AgentID:   agentID,
		Type:      "backup",
		Status:    taskStatus,
		MessageID: messageID,
		CommandID: command.ID,
		CreatedAt: time.Now(),
	}
	require.NoError(t, database.DB.Create(&history).Error)
	return history
}

func shQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
