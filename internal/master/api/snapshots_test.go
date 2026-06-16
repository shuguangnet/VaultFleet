package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestUpsertSnapshotsFromTaskResult(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	firstSeen := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	updatedSeen := firstSeen.Add(time.Hour)

	require.NoError(t, recordTaskResult(database, agent.ID, "", protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-1",
		DurationMs: 1200,
		RepoSize:   4096,
		StartedAt:  firstSeen.Add(-1200 * time.Millisecond),
		FinishedAt: firstSeen,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-1", Time: firstSeen, Paths: []string{"/etc"}, Size: 4096},
		},
	}))
	require.NoError(t, recordTaskResult(database, agent.ID, "", protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-1",
		DurationMs: 1300,
		RepoSize:   8192,
		StartedAt:  updatedSeen.Add(-1300 * time.Millisecond),
		FinishedAt: updatedSeen,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-1", Time: updatedSeen, Paths: []string{"/etc", "/home"}, Size: 8192},
		},
	}))

	var histories []db.TaskHistory
	require.NoError(t, database.DB.Order("created_at ASC").Find(&histories).Error)
	require.Len(t, histories, 2)
	assert.Equal(t, "backup", histories[0].Type)
	assert.Equal(t, "success", histories[0].Status)
	assert.Equal(t, "snap-1", histories[0].SnapshotID)
	assert.Equal(t, int64(1200), histories[0].DurationMs)

	var snapshots []db.Snapshot
	require.NoError(t, database.DB.Find(&snapshots, "agent_id = ?", agent.ID).Error)
	require.Len(t, snapshots, 1)
	assert.Equal(t, "snap-1", snapshots[0].SnapshotID)
	assert.True(t, snapshots[0].Timestamp.Equal(updatedSeen))
	assert.Equal(t, int64(8192), snapshots[0].Size)
	assert.JSONEq(t, `["/etc","/home"]`, snapshots[0].Paths)
}

func TestTaskResultProcessorRecordsHistoryAndSnapshots(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	finishedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-processor",
		DurationMs: 1500,
		StartedAt:  finishedAt.Add(-1500 * time.Millisecond),
		FinishedAt: finishedAt,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-processor", Time: finishedAt, Paths: []string{"/srv"}, Size: 1024},
		},
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-processor").Error)
	assert.Equal(t, "backup", history.Type)
	assert.Equal(t, "success", history.Status)

	var snapshot db.Snapshot
	require.NoError(t, database.DB.First(&snapshot, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-processor").Error)
	assert.True(t, snapshot.Timestamp.Equal(finishedAt))
	assert.JSONEq(t, `["/srv"]`, snapshot.Paths)
}

func TestTaskResultProcessorCopiesArchiveArtifactIntoMasterDataDir(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	finishedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "archive.tar.gz")
	require.NoError(t, os.WriteFile(sourcePath, []byte("archive-payload"), 0o644))

	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:             agent.ID,
		TaskType:            "backup",
		Status:              "success",
		BackupMode:          protocol.BackupModeArchive,
		ArchiveFormat:       protocol.ArchiveFormatTarGz,
		ArtifactPath:        sourcePath,
		ArtifactName:        "archive.tar.gz",
		ArtifactContentType: "application/gzip",
		DurationMs:          1500,
		StartedAt:           finishedAt.Add(-1500 * time.Millisecond),
		FinishedAt:          finishedAt,
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "agent_id = ? AND artifact_name = ?", agent.ID, "archive.tar.gz").Error)
	assert.Equal(t, filepath.ToSlash(filepath.Join("artifacts", agent.ID, "archive.tar.gz")), history.ArtifactPath)
	assert.Equal(t, int64(len("archive-payload")), history.ArtifactSize)

	storedPath := filepath.Join(database.DataDir, filepath.FromSlash(history.ArtifactPath))
	storedBytes, err := os.ReadFile(storedPath)
	require.NoError(t, err)
	assert.Equal(t, "archive-payload", string(storedBytes))
}

func TestTaskResultProcessorFailsArchiveResultWhenArtifactMissing(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:       agent.ID,
		TaskType:      "backup",
		Status:        "success",
		BackupMode:    protocol.BackupModeArchive,
		ArchiveFormat: protocol.ArchiveFormatTarGz,
		ArtifactPath:  filepath.Join(t.TempDir(), "missing.tar.gz"),
		ArtifactName:  "missing.tar.gz",
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.Error(t, processor(agent.ID, *msg))

	var historyCount int64
	require.NoError(t, database.DB.Model(&db.TaskHistory{}).Where("agent_id = ?", agent.ID).Count(&historyCount).Error)
	assert.Equal(t, int64(0), historyCount)
}

func TestTaskResultProcessorRollsBackBackupHistoryWhenSnapshotPersistenceFails(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	require.NoError(t, database.DB.Exec(`
		CREATE TRIGGER fail_snapshot_insert
		BEFORE INSERT ON snapshots
		BEGIN
			SELECT RAISE(FAIL, 'forced snapshot failure');
		END;
	`).Error)
	finishedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-fail",
		DurationMs: 1500,
		StartedAt:  finishedAt.Add(-1500 * time.Millisecond),
		FinishedAt: finishedAt,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-fail", Time: finishedAt, Paths: []string{"/srv"}, Size: 1024},
		},
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.Error(t, processor(agent.ID, *msg))

	var historyCount int64
	require.NoError(t, database.DB.Model(&db.TaskHistory{}).Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-fail").Count(&historyCount).Error)
	assert.Equal(t, int64(0), historyCount)
}

func TestTaskResultProcessorCompletesRunningRestoreHistory(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	masterStartedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	agentStartedAt := masterStartedAt.Add(2 * time.Second)
	finishedAt := agentStartedAt.Add(45 * time.Second)
	running := db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "restore",
		Status:     "running",
		SnapshotID: "snap-restore",
		MessageID:  "restore-msg-1",
		StartedAt:  &masterStartedAt,
	}
	require.NoError(t, database.DB.Create(&running).Error)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "restore",
		Status:     "success",
		SnapshotID: "snap-restore",
		DurationMs: 45000,
		StartedAt:  agentStartedAt,
		FinishedAt: finishedAt,
	})
	require.NoError(t, err)
	msg.ID = "restore-msg-1"

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var histories []db.TaskHistory
	require.NoError(t, database.DB.Find(&histories, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-restore").Error)
	require.Len(t, histories, 1)
	assert.Equal(t, running.ID, histories[0].ID)
	assert.Equal(t, "success", histories[0].Status)
	assert.Equal(t, int64(45000), histories[0].DurationMs)
	require.NotNil(t, histories[0].FinishedAt)
	assert.True(t, histories[0].FinishedAt.Equal(finishedAt))
}

func TestTaskResultProcessorCompletesRestoreHistoryByMessageID(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	startedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	first := db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "restore",
		Status:     "running",
		SnapshotID: "same-snapshot",
		MessageID:  "restore-msg-1",
		StartedAt:  &startedAt,
	}
	second := db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "restore",
		Status:     "running",
		SnapshotID: "same-snapshot",
		MessageID:  "restore-msg-2",
		StartedAt:  &startedAt,
	}
	require.NoError(t, database.DB.Create(&first).Error)
	require.NoError(t, database.DB.Create(&second).Error)
	finishedAt := startedAt.Add(time.Minute)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "restore",
		Status:     "success",
		SnapshotID: "same-snapshot",
		DurationMs: 60000,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
	require.NoError(t, err)
	msg.ID = "restore-msg-1"

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var firstAfter db.TaskHistory
	require.NoError(t, database.DB.First(&firstAfter, "id = ?", first.ID).Error)
	assert.Equal(t, "success", firstAfter.Status)
	assert.Equal(t, int64(60000), firstAfter.DurationMs)
	require.NotNil(t, firstAfter.FinishedAt)
	assert.True(t, firstAfter.FinishedAt.Equal(finishedAt))

	var secondAfter db.TaskHistory
	require.NoError(t, database.DB.First(&secondAfter, "id = ?", second.ID).Error)
	assert.Equal(t, "running", secondAfter.Status)
	assert.Nil(t, secondAfter.FinishedAt)
	assert.Equal(t, int64(0), secondAfter.DurationMs)
}

func TestTaskResultProcessorCompletesCommandLinkedTaskWithoutDuplicateHistory(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	startedAt := now.Add(-2 * time.Minute)
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusRunning,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).Where("id = ?", command.ID).Update("status", commands.CommandStatusRunning).Error)
	resultMsg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-command-linked",
		DurationMs: 120000,
		RepoSize:   4096,
		StartedAt:  startedAt,
		FinishedAt: now,
	})
	require.NoError(t, err)
	resultMsg.ID = msg.ID

	processor := NewTaskResultProcessor(database, service)
	require.NoError(t, processor(agent.ID, *resultMsg))

	var historyCount int64
	require.NoError(t, database.DB.Model(&db.TaskHistory{}).Where("agent_id = ? AND message_id = ?", agent.ID, msg.ID).Count(&historyCount).Error)
	assert.Equal(t, int64(1), historyCount)
	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, commands.TaskStatusSuccess, history.Status)
	assert.Equal(t, "snap-command-linked", history.SnapshotID)
	assert.Equal(t, int64(120000), history.DurationMs)
	assert.Equal(t, int64(4096), history.RepoSize)

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusSucceeded, found.Status)
	assert.Contains(t, found.Result, `"snapshot_id":"snap-command-linked"`)
}

func TestTaskResultProcessorCompletesCommandLinkedBackupAndPersistsSnapshots(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	finishedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return finishedAt }
	startedAt := finishedAt.Add(-2 * time.Minute)
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusRunning,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).Where("id = ?", command.ID).Update("status", commands.CommandStatusRunning).Error)
	resultMsg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-linked",
		DurationMs: 120000,
		RepoSize:   2048,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-linked", Time: finishedAt, Paths: []string{"/srv"}, Size: 2048},
		},
	})
	require.NoError(t, err)
	resultMsg.ID = msg.ID

	processor := NewTaskResultProcessor(database, service)
	require.NoError(t, processor(agent.ID, *resultMsg))

	var histories []db.TaskHistory
	require.NoError(t, database.DB.Find(&histories, "agent_id = ? AND message_id = ?", agent.ID, msg.ID).Error)
	require.Len(t, histories, 1)
	assert.Equal(t, commands.TaskStatusSuccess, histories[0].Status)

	var snapshot db.Snapshot
	require.NoError(t, database.DB.First(&snapshot, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-linked").Error)
	assert.True(t, snapshot.Timestamp.Equal(finishedAt))
	assert.JSONEq(t, `["/srv"]`, snapshot.Paths)
	assert.Equal(t, int64(2048), snapshot.Size)

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusSucceeded, found.Status)
}

func TestTaskResultProcessorDoesNotPersistSnapshotsForTerminalCommand(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusRunning,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).
		Where("id = ?", command.ID).
		Updates(map[string]any{
			"status":        commands.CommandStatusFailed,
			"error_message": "already failed",
			"completed_at":  &now,
		}).Error)
	resultMsg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-late-backup",
		DurationMs: 120000,
		RepoSize:   2048,
		StartedAt:  now.Add(-2 * time.Minute),
		FinishedAt: now.Add(time.Minute),
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-late-backup", Time: now.Add(time.Minute), Paths: []string{"/late"}, Size: 2048},
		},
	})
	require.NoError(t, err)
	resultMsg.ID = msg.ID

	processor := NewTaskResultProcessor(database, service)
	require.NoError(t, processor(agent.ID, *resultMsg))

	var snapshotCount int64
	require.NoError(t, database.DB.Model(&db.Snapshot{}).
		Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-late-backup").
		Count(&snapshotCount).Error)
	assert.Equal(t, int64(0), snapshotCount)

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusFailed, found.Status)
	assert.Equal(t, "already failed", found.ErrorMessage)
	require.NotNil(t, found.CompletedAt)
	assert.True(t, found.CompletedAt.Equal(now))

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, commands.TaskStatusRunning, history.Status)
	assert.Empty(t, history.SnapshotID)
	assert.Equal(t, int64(0), history.DurationMs)
	assert.Equal(t, int64(0), history.RepoSize)
	assert.Nil(t, history.FinishedAt)
}

func TestUpsertSnapshotsConcurrentSameSnapshotDoesNotDuplicate(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	snapshots := []protocol.SnapshotInfo{
		{ID: "snap-race", Time: snapshotTime, Paths: []string{"/srv"}, Size: 512},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- upsertSnapshots(database, agent.ID, snapshots)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var count int64
	require.NoError(t, database.DB.Model(&db.Snapshot{}).Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-race").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestListSnapshots(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	older := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	createSnapshotRecord(t, setup.database, agent.ID, "snap-old", older, []string{"/etc"}, 100)
	createSnapshotRecord(t, setup.database, agent.ID, "snap-new", newer, []string{"/home", "/srv"}, 200)

	w := getJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	envelope := parseJSON(t, w)
	assert.Equal(t, true, envelope["ok"])
	data, err := json.Marshal(envelope["data"])
	require.NoError(t, err)
	var body []snapshotAPITestResponse
	require.NoError(t, json.Unmarshal(data, &body))
	require.Len(t, body, 2)
	assert.Equal(t, "snap-new", body[0].ID)
	assert.Equal(t, "snap-new", body[0].SnapshotID)
	assert.True(t, body[0].Time.Equal(newer))
	assert.Equal(t, []string{"/home", "/srv"}, body[0].Paths)
	assert.Equal(t, int64(200), body[0].Size)
	assert.Equal(t, "snap-old", body[1].ID)
}

func TestListSnapshotsExposesSpecFields(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC)
	stored := createSnapshotRecord(t, setup.database, agent.ID, "restic-snap-1", snapshotTime, []string{"/etc"}, 512)

	w := getJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	data := requireList(t, body["data"])
	require.Len(t, data, 1)
	item := requireMap(t, data[0])
	assert.Equal(t, "restic-snap-1", item["id"])
	assert.NotEqual(t, stored.ID, item["id"])
	assert.Equal(t, snapshotTime.Format(time.RFC3339), item["time"])
	assertJSONList(t, item["paths"], []string{"/etc"})
	assert.Contains(t, item, "hostname")
	assert.Contains(t, item, "username")
	assert.Contains(t, item, "snapshot_id")
	assert.Contains(t, item, "timestamp")
}

func TestListSnapshotsMissingAgent(t *testing.T) {
	setup := setupSnapshotAPI(t)

	w := getJSON(t, setup.router, "/api/agents/missing/snapshots")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRefreshSnapshots(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.SnapshotListReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, agent.ID, req.AgentID)

		resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
			AgentID: agent.ID,
			Snapshots: []protocol.SnapshotInfo{
				{ID: "snap-1", Time: snapshotTime, Paths: []string{"/etc"}, Size: 512},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	envelope := parseJSON(t, w)
	assert.Equal(t, true, envelope["ok"])
	assert.Equal(t, float64(1), envelope["count"])
	data, err := json.Marshal(envelope["data"])
	require.NoError(t, err)
	var body snapshotRefreshAPITestResponse
	require.NoError(t, json.Unmarshal(data, &body))
	assert.Equal(t, 1, body.Count)
	require.Len(t, body.Snapshots, 1)
	assert.Equal(t, "snap-1", body.Snapshots[0].SnapshotID)

	var stored db.Snapshot
	require.NoError(t, setup.database.DB.First(&stored, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
	assert.True(t, stored.Timestamp.Equal(snapshotTime))
	assert.JSONEq(t, `["/etc"]`, stored.Paths)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusSucceeded, command.Status)
	assert.Equal(t, 1, command.Attempts)
	assert.NotNil(t, command.DispatchedAt)
	assert.Contains(t, command.Result, `"snap-1"`)
	assert.Empty(t, command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
}

func TestRefreshSnapshotsCancelledRequestStillCompletesFromAgentResponse(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	respCh := make(chan protocol.Message, 1)
	sentMessageID := make(chan string, 1)
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		assert.Equal(t, time.Minute, timeout)
		sentMessageID <- msg.ID
		cancel()
		return respCh, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/snapshots/refresh", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		setup.router.ServeHTTP(w, req)
	}()

	var messageID string
	require.Eventually(t, func() bool {
		select {
		case messageID = <-sentMessageID:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		select {
		case <-handlerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusGatewayTimeout, w.Code, w.Body.String())

	resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
		AgentID: agent.ID,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-cancelled-refresh", Time: snapshotTime, Paths: []string{"/etc"}, Size: 512},
		},
	})
	require.NoError(t, err)
	resp.ID = messageID
	respCh <- *resp
	close(respCh)

	require.Eventually(t, func() bool {
		var snapshot db.Snapshot
		if err := setup.database.DB.First(&snapshot, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-cancelled-refresh").Error; err != nil {
			return false
		}
		if !snapshot.Timestamp.Equal(snapshotTime) || snapshot.Paths != `["/etc"]` {
			return false
		}

		var command db.AgentCommand
		if err := setup.database.DB.First(&command, "agent_id = ? AND message_id = ?", agent.ID, messageID).Error; err != nil {
			return false
		}
		return command.Status == commands.CommandStatusSucceeded &&
			command.ErrorMessage == "" &&
			command.CompletedAt != nil &&
			json.Valid([]byte(command.Result)) &&
			strings.Contains(command.Result, "snap-cancelled-refresh")
	}, time.Second, 10*time.Millisecond)
}

func TestRefreshSnapshotsResponseReadyWithCancelledContextCompletesCommand(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	completedAt := snapshotTime.Add(time.Second)
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		assert.Equal(t, time.Second, timeout)

		resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
			AgentID: agent.ID,
			Snapshots: []protocol.SnapshotInfo{
				{ID: "snap-ready-cancelled", Time: snapshotTime, Paths: []string{"/var/lib"}, Size: 2048},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)

		setup.handler.Commands.Now = func() time.Time {
			cancel()
			return completedAt
		}
		return ch, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/snapshots/refresh", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	setup.router.ServeHTTP(w, req)

	assert.Contains(t, []int{http.StatusOK, http.StatusGatewayTimeout}, w.Code, w.Body.String())
	var snapshot db.Snapshot
	require.NoError(t, setup.database.DB.First(&snapshot, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-ready-cancelled").Error)
	assert.True(t, snapshot.Timestamp.Equal(snapshotTime))
	assert.JSONEq(t, `["/var/lib"]`, snapshot.Paths)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusSucceeded, command.Status)
	assert.Empty(t, command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
	assert.Contains(t, command.Result, "snap-ready-cancelled")
}

func TestRefreshSnapshotsOfflineQueuesCommand(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	commandID, ok := data["command_id"].(string)
	require.True(t, ok)
	messageID, ok := data["message_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, commandID)
	assert.NotEmpty(t, messageID)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", commandID).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeSnapshotListReq, command.Type)
	assert.Equal(t, commands.CommandStatusPending, command.Status)
	assert.Equal(t, messageID, command.MessageID)
}

func TestRefreshSnapshotsSendAndWaitFailureLeavesCommandQueued(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		return nil, errors.New("connection lost")
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	commandID, ok := data["command_id"].(string)
	require.True(t, ok)
	messageID, ok := data["message_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, commandID)
	assert.NotEmpty(t, messageID)

	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "id = ?", commandID).Error)
	assert.Equal(t, agent.ID, command.AgentID)
	assert.Equal(t, protocol.TypeSnapshotListReq, command.Type)
	assert.Equal(t, commands.CommandStatusPending, command.Status)
	assert.Equal(t, 1, command.Attempts)
	assert.Nil(t, command.DispatchedAt)
	assert.Contains(t, command.ErrorMessage, "connection lost")
	assert.Equal(t, messageID, command.MessageID)
	assert.Nil(t, command.CompletedAt)
}

func TestRefreshSnapshotsRecordsDispatchBeforeWaitingForResponse(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	respCh := make(chan protocol.Message)
	sentMessageID := make(chan string, 1)
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		sentMessageID <- msg.ID
		return respCh, nil
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})
	}()

	var messageID string
	require.Eventually(t, func() bool {
		select {
		case messageID = <-sentMessageID:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		var command db.AgentCommand
		if err := setup.database.DB.First(&command, "agent_id = ? AND message_id = ?", agent.ID, messageID).Error; err != nil {
			return false
		}
		return command.Status == commands.CommandStatusDispatched &&
			command.Attempts == 1 &&
			command.DispatchedAt != nil &&
			command.ErrorMessage == ""
	}, time.Second, 10*time.Millisecond)

	resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{AgentID: agent.ID})
	require.NoError(t, err)
	resp.ID = messageID
	respCh <- *resp
	close(respCh)

	w := <-done
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestRefreshSnapshotsTimeout(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second
	setup.hub.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusTimeout, command.Status)
	assert.Equal(t, "command timeout", command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
}

func TestSnapshotListResponseProcessorDoesNotPersistSnapshotsForTerminalCommand(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	request, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID: agent.ID,
		Type:    protocol.TypeSnapshotListReq,
		Message: *request,
	})
	require.NoError(t, err)
	completedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).
		Where("id = ?", command.ID).
		Updates(map[string]any{
			"status":        commands.CommandStatusTimeout,
			"error_message": "command timeout",
			"completed_at":  &completedAt,
		}).Error)
	response, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
		AgentID: agent.ID,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-late-list", Time: completedAt.Add(time.Minute), Paths: []string{"/late"}, Size: 512},
		},
	})
	require.NoError(t, err)
	response.ID = request.ID

	processor := NewSnapshotListResponseProcessor(database, service)
	require.NoError(t, processor(agent.ID, *response))

	var snapshotCount int64
	require.NoError(t, database.DB.Model(&db.Snapshot{}).
		Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-late-list").
		Count(&snapshotCount).Error)
	assert.Equal(t, int64(0), snapshotCount)

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusTimeout, found.Status)
	assert.Equal(t, "command timeout", found.ErrorMessage)
	require.NotNil(t, found.CompletedAt)
	assert.True(t, found.CompletedAt.Equal(completedAt))
}

func TestTaskResultProcessorDoesNotCompleteWrongCommandType(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID: agent.ID,
		Type:    protocol.TypePolicyPush,
		Message: *msg,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).
		Where("id = ?", command.ID).
		Update("status", commands.CommandStatusDispatched).Error)
	resultMsg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-wrong-command-type",
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-wrong-command-type", Time: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC), Paths: []string{"/srv"}, Size: 1024},
		},
	})
	require.NoError(t, err)
	resultMsg.ID = msg.ID

	processor := NewTaskResultProcessor(database, service)
	require.NoError(t, processor(agent.ID, *resultMsg))

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusDispatched, found.Status)
	assert.Empty(t, found.Result)
	assert.Nil(t, found.CompletedAt)

	var snapshotCount int64
	require.NoError(t, database.DB.Model(&db.Snapshot{}).
		Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-wrong-command-type").
		Count(&snapshotCount).Error)
	assert.Equal(t, int64(0), snapshotCount)
}

func TestTaskResultProcessorDoesNotCompleteMismatchedTaskType(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusRunning,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).
		Where("id = ?", command.ID).
		Update("status", commands.CommandStatusRunning).Error)
	resultMsg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "restore",
		Status:     "success",
		SnapshotID: "snap-mismatched-task",
	})
	require.NoError(t, err)
	resultMsg.ID = msg.ID

	processor := NewTaskResultProcessor(database, service)
	require.NoError(t, processor(agent.ID, *resultMsg))

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusRunning, found.Status)
	assert.Empty(t, found.Result)
	assert.Nil(t, found.CompletedAt)

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, commands.TaskStatusRunning, history.Status)
	assert.Empty(t, history.SnapshotID)
	assert.Nil(t, history.FinishedAt)
}

func TestSnapshotListResponseProcessorErrorDoesNotFailWrongCommandType(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	service := commands.NewService(database, nil)
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agent.ID})
	require.NoError(t, err)
	command, err := service.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID:   agent.ID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusRunning,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).
		Where("id = ?", command.ID).
		Update("status", commands.CommandStatusRunning).Error)
	response, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
		AgentID: agent.ID,
		Error:   "repository unavailable",
	})
	require.NoError(t, err)
	response.ID = msg.ID

	processor := NewSnapshotListResponseProcessor(database, service)
	require.NoError(t, processor(agent.ID, *response))

	var found db.AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, commands.CommandStatusRunning, found.Status)
	assert.Empty(t, found.ErrorMessage)
	assert.Nil(t, found.CompletedAt)
}

func TestRefreshSnapshotsClosedWaiterUsesDetachedContext(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		setup.handler.Commands.Now = func() time.Time {
			cancel()
			return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
		}
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/snapshots/refresh", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	setup.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusGatewayTimeout, w.Code, w.Body.String())
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusTimeout, command.Status)
	assert.Equal(t, "command timeout", command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
}

func TestRefreshSnapshotsAgentError(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendAndWait = func(_ string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
		resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
			AgentID: agent.ID,
			Error:   "restic repository locked",
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "restic repository locked", body["error"])
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusFailed, command.Status)
	assert.Equal(t, "restic repository locked", command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
}

func TestRefreshSnapshotsRejectsWrongResponseType(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendAndWait = func(_ string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
		resp, err := protocol.NewMessage(protocol.TypeDirBrowseResp, protocol.DirBrowseRespPayload{Path: "/", Entries: []protocol.DirEntry{}})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "invalid agent response", body["error"])
	var command db.AgentCommand
	require.NoError(t, setup.database.DB.First(&command, "agent_id = ? AND type = ?", agent.ID, protocol.TypeSnapshotListReq).Error)
	assert.Equal(t, commands.CommandStatusFailed, command.Status)
	assert.Equal(t, "invalid agent response", command.ErrorMessage)
	assert.NotNil(t, command.CompletedAt)
}

type snapshotAPISetup struct {
	database *db.Database
	hub      *fakeSnapshotHub
	handler  *SnapshotHandler
	router   *gin.Engine
}

func setupSnapshotAPI(t *testing.T) snapshotAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := &fakeSnapshotHub{online: map[string]bool{}}
	handler := NewSnapshotHandler(database, hub)
	handler.Commands = commands.NewService(database, hub)
	router := gin.New()
	RegisterSnapshotRoutes(router.Group("/api"), handler)

	return snapshotAPISetup{database: database, hub: hub, handler: handler, router: router}
}

type fakeSnapshotHub struct {
	online      map[string]bool
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

func (h *fakeSnapshotHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *fakeSnapshotHub) SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
	if h.sendAndWait == nil {
		return nil, errors.New("not connected")
	}
	return h.sendAndWait(agentID, msg, timeout)
}

func (h *fakeSnapshotHub) Send(agentID string, msg interface{}) error {
	message, ok := msg.(protocol.Message)
	if !ok {
		return errors.New("message is not protocol.Message")
	}
	respCh, err := h.SendAndWait(agentID, message, time.Second)
	if err != nil {
		return err
	}
	go func() {
		for range respCh {
		}
	}()
	return nil
}

type snapshotAPITestResponse struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	Time       time.Time `json:"time"`
	Paths      []string  `json:"paths"`
	Hostname   string    `json:"hostname"`
	Username   string    `json:"username"`
	Size       int64     `json:"size"`
}

type snapshotRefreshAPITestResponse struct {
	Count     int                       `json:"count"`
	Snapshots []snapshotAPITestResponse `json:"snapshots"`
}

func createSnapshotTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Snapshot Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func createSnapshotRecord(t *testing.T, database *db.Database, agentID string, snapshotID string, timestamp time.Time, paths []string, size int64) db.Snapshot {
	t.Helper()

	pathsJSON, err := json.Marshal(paths)
	require.NoError(t, err)
	snapshot := db.Snapshot{
		AgentID:    agentID,
		SnapshotID: snapshotID,
		Timestamp:  timestamp,
		Paths:      string(pathsJSON),
		Size:       size,
	}
	require.NoError(t, database.DB.Create(&snapshot).Error)
	return snapshot
}
