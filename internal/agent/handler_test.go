package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentdatabase "vaultfleet/internal/agent/database"
	agentdocker "vaultfleet/internal/agent/docker"
	"vaultfleet/internal/agent/executor"
	"vaultfleet/internal/agent/policy"
	agentscheduler "vaultfleet/internal/agent/scheduler"
	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/rcloneobscure"
)

func TestHandlePolicyPushSavesPolicyWritesConfigSchedulesBackupAndAcks(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := filepath.Join(t.TempDir(), "config")
	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	var runnerCalls atomic.Int32
	var runnerConfig executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerCalls.Add(1)
			runnerConfig = cfg
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 25, SnapshotID: "snap-1"}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "s3",
			RcloneConfig: map[string]string{
				"access_key_id": "key",
				"provider":      "Other",
			},
			RepoPath: "bucket/agent-1",
		},
		ResticPassword:  "secret-password",
		BackupDirs:      []string{"/srv"},
		ExcludePatterns: []string{"*.tmp"},
		PreBackupHook:   &protocol.PolicyHook{Command: "echo pre", TimeoutSeconds: 10},
		PostBackupHook:  &protocol.PolicyHook{Command: "echo post", TimeoutSeconds: 15},
		Schedule:        "0 4 * * *",
		Retention:       protocol.RetentionPolicy{KeepLast: 3, KeepDaily: 7},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	stored, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, "agent-1", stored.AgentID)
	assert.Equal(t, []string{"/srv"}, stored.BackupDirs)
	assert.Equal(t, "0 4 * * *", stored.Schedule)

	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	assert.Contains(t, string(rcloneConf), "[vaultfleet]\n")
	assert.Contains(t, string(rcloneConf), "type = s3\n")
	assert.Contains(t, string(rcloneConf), "provider = Other\n")
	assertFileMode(t, filepath.Join(configDir, "rclone.conf"), 0o600)

	password, err := os.ReadFile(filepath.Join(configDir, ".restic-password"))
	require.NoError(t, err)
	assert.Equal(t, "secret-password", string(password))
	assertFileMode(t, filepath.Join(configDir, ".restic-password"), 0o600)

	require.Len(t, scheduler.updates, 1)
	assert.Equal(t, "agent-1", scheduler.updates[0].agentID)
	assert.Equal(t, "0 4 * * *", scheduler.updates[0].schedule)
	require.NotNil(t, scheduler.updates[0].fn)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypePolicyAck, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, protocol.PolicyAckPayload{AgentID: "agent-1", Success: true}, *ack)

	scheduler.updates[0].fn()
	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, int32(1), runnerCalls.Load())
	assert.Equal(t, executor.ExecutorConfig{
		ConfigDir:      configDir,
		RepoPath:       "bucket/agent-1",
		BackupDirs:     []string{"/srv"},
		Excludes:       []string{"*.tmp"},
		Retention:      executor.RetentionPolicy{KeepLast: 3, KeepDaily: 7},
		PreBackupHook:  &protocol.PolicyHook{Command: "echo pre", TimeoutSeconds: 10},
		PostBackupHook: &protocol.PolicyHook{Command: "echo post", TimeoutSeconds: 15},
	}, withoutTaskLog(runnerConfig))
}

func TestRunBackupForPolicyFailsWhenPreHookFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	sent := &sentMessages{}
	var runnerCalls atomic.Int32
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, _ executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerCalls.Add(1)
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 5}
		},
	})

	handler.runBackupForPolicy(context.Background(), "msg-1", "agent-1", &protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}, RepoPath: "bucket/agent-1"},
		ResticPassword: "secret",
		BackupDirs:     []string{"/srv"},
		PreBackupHook:  &protocol.PolicyHook{Command: "exit 7", TimeoutSeconds: 5},
	})

	assert.Equal(t, int32(0), runnerCalls.Load())
	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.ErrorLog, "pre_backup_hook")
}

func TestRunBackupForPolicyFailsWhenPostHookFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, _ executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 5, SnapshotID: "snap-1"}
		},
	})

	handler.runBackupForPolicy(context.Background(), "msg-1", "agent-1", &protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}, RepoPath: "bucket/agent-1"},
		ResticPassword: "secret",
		BackupDirs:     []string{"/srv"},
		PostBackupHook: &protocol.PolicyHook{Command: "echo boom >&2; exit 3", TimeoutSeconds: 5},
	})

	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "snap-1", result.SnapshotID)
	assert.Contains(t, result.ErrorLog, "post_backup_hook")
	assert.Contains(t, result.ErrorLog, "boom")
}

func TestHandlePolicyPushPreservesLegacyObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := filepath.Join(t.TempDir(), "config")
	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	legacyPass, err := rcloneobscure.ObscurePass("clear-sftp-password")
	require.NoError(t, err)

	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, _ executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 25, SnapshotID: "snap-1"}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType:         "sftp",
			RclonePassObscured: true,
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": legacyPass,
			},
			RepoPath: "vaultfleet/agent-1",
		},
		BackupDirs: []string{"/srv"},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	passValue := rcloneConfValue(t, string(rcloneConf), "pass")
	assert.Equal(t, legacyPass, passValue)
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandlePolicyPushPlainBackupRemovesExistingPasswordFile(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".restic-password"), []byte("old-secret"), 0o600))

	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   &recordingScheduler{},
		SendFunc:    (&sentMessages{}).send,
	})

	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		PlainBackup:    true,
		ResticPassword: "",
		Storage: protocol.StorageConfig{
			RcloneType: "s3",
			RcloneConfig: map[string]string{
				"provider": "Other",
			},
			RepoPath: "bucket/plain-agent",
		},
		BackupDirs: []string{"/opt/backup"},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	_, statErr := os.Stat(filepath.Join(configDir, ".restic-password"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestNewHandlerRestoresSavedPolicySchedule(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "bucket/agent-1",
		},
		BackupDirs: []string{"/srv"},
		Schedule:   "0 2 * * *",
	}))

	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	var runnerCalls atomic.Int32
	NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		Scheduler:   scheduler,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerCalls.Add(1)
			assert.Equal(t, []string{"/srv"}, filterManifestBackupDirs(cfg.BackupDirs))
			assert.Equal(t, "bucket/agent-1", cfg.RepoPath)
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10, SnapshotID: "snap-1"}
		},
	})

	require.Len(t, scheduler.updates, 1)
	assert.Equal(t, "agent-1", scheduler.updates[0].agentID)
	assert.Equal(t, "0 2 * * *", scheduler.updates[0].schedule)

	scheduler.updates[0].fn()
	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, int32(1), runnerCalls.Load())
}

func TestHandlePolicyPushPassesRcloneArgs(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	var runnerConfigs []executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerConfigs = append(runnerConfigs, cfg)
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "s3",
			RcloneConfig: map[string]string{
				"provider": "Other",
			},
			RepoPath: "bucket/agent-1",
			RcloneArgs: map[string]string{
				"transfers":        "8",
				"checkers":         "16",
				"s3-upload-cutoff": "128M",
			},
		},
		ResticPassword: "secret-password",
		BackupDirs:     []string{"/srv"},
		Schedule:       "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	require.Len(t, scheduler.updates, 1)
	require.NotNil(t, scheduler.updates[0].fn)

	scheduler.updates[0].fn()
	waitForMessageTypeCount(t, sent, protocol.TypeTaskResult, 1, time.Second)
	wantRcloneArgs := map[string]string{
		"transfers":        "8",
		"checkers":         "16",
		"s3-upload-cutoff": "128M",
	}
	require.Equal(t, wantRcloneArgs, runnerConfigs[0].RcloneArgs)

	runnerConfigs[0].RcloneArgs["transfers"] = "99"
	scheduler.updates[0].fn()

	waitForMessageTypeCount(t, sent, protocol.TypeTaskResult, 2, time.Second)
	require.Len(t, runnerConfigs, 2)
	assert.Equal(t, wantRcloneArgs, runnerConfigs[1].RcloneArgs)
}

func TestFlushPendingResultsSendsStoredResultsAndClearsOnSuccess(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	startedAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.SavePendingResults([]policy.PendingTaskResult{
		{
			MessageID: "backup-message-1",
			Payload: protocol.TaskResultPayload{
				AgentID:    "agent-1",
				TaskType:   "backup",
				Status:     "success",
				SnapshotID: "snap-1",
				StartedAt:  startedAt,
				FinishedAt: startedAt.Add(time.Second),
			},
		},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		SendFunc:    sent.send,
	})

	handler.FlushPendingResults()

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeTaskResult, messages[0].Type)
	assert.Equal(t, "backup-message-1", messages[0].ID)
	payload, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "snap-1", payload.SnapshotID)
	pending, err := store.LoadPendingResults()
	require.NoError(t, err)
	assert.Nil(t, pending)
}

func TestFlushPendingResultsKeepsUnsentResults(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePendingResults([]policy.PendingTaskResult{
		{MessageID: "msg-1", Payload: protocol.TaskResultPayload{AgentID: "agent-1", TaskType: "backup", Status: "success"}},
		{MessageID: "msg-2", Payload: protocol.TaskResultPayload{AgentID: "agent-1", TaskType: "backup", Status: "success"}},
	}))
	var calls int
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		SendFunc: func(protocol.Message) error {
			calls++
			if calls == 2 {
				return errors.New("not connected")
			}
			return nil
		},
	})

	handler.FlushPendingResults()

	pending, err := store.LoadPendingResults()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "msg-2", pending[0].MessageID)
}

func TestPersistPendingResultConcurrentWritesKeepAllResults(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	handler := NewHandler(HandlerConfig{PolicyStore: store})
	const count = 100
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			handler.persistPendingResult(
				"msg-"+strconv.Itoa(i),
				protocol.TaskResultPayload{AgentID: "agent-1", TaskType: "backup", Status: "success", SnapshotID: "snap-" + strconv.Itoa(i)},
			)
		}()
	}
	wg.Wait()

	pending, err := store.LoadPendingResults()
	require.NoError(t, err)
	require.Len(t, pending, count)
	seen := make(map[string]bool, count)
	for _, result := range pending {
		seen[result.MessageID] = true
	}
	for i := 0; i < count; i++ {
		assert.True(t, seen["msg-"+strconv.Itoa(i)], "missing pending result %d", i)
	}
}

func TestHandlePolicyPushSendsFailureAckAndDoesNotScheduleWhenConfigWriteFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := filepath.Join(t.TempDir(), "config-file")
	require.NoError(t, os.WriteFile(configDir, []byte("not a directory"), 0o600))
	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}},
		ResticPassword: "secret-password",
		Schedule:       "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	require.Empty(t, scheduler.updates)
	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypePolicyAck, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", ack.AgentID)
	assert.False(t, ack.Success)
	assert.NotEmpty(t, ack.Error)
	_, err = store.LoadPolicy()
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestHandlePolicyPushSendsFailureAckWhenScheduleUpdateFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	scheduler := &recordingScheduler{err: errors.New("invalid cron")}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}},
		ResticPassword: "secret-password",
		Schedule:       "not a cron",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	require.Empty(t, scheduler.updates)
	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypePolicyAck, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", ack.AgentID)
	assert.False(t, ack.Success)
	assert.Equal(t, "invalid cron", ack.Error)
}

func TestHandlePolicyPushScheduleUpdateFailureRestoresExistingPolicyAndConfig(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	oldPolicy := &protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Old"}, RepoPath: "old-repo"},
		ResticPassword: "old-secret",
		BackupDirs:     []string{"/old"},
		Schedule:       "0 1 * * *",
	}
	require.NoError(t, store.SavePolicy(oldPolicy))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("old rclone config"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".restic-password"), []byte("old-secret"), 0o600))
	scheduler := &recordingScheduler{updateErr: errors.New("schedule unavailable")}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "New"}, RepoPath: "new-repo"},
		ResticPassword: "new-secret",
		BackupDirs:     []string{"/new"},
		Schedule:       "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", ack.AgentID)
	assert.False(t, ack.Success)
	assert.Equal(t, "schedule unavailable", ack.Error)

	stored, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, oldPolicy, stored)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	assert.Equal(t, "old rclone config", string(rcloneConf))
	password, err := os.ReadFile(filepath.Join(configDir, ".restic-password"))
	require.NoError(t, err)
	assert.Equal(t, "old-secret", string(password))
}

func TestHandlePolicyPushScheduleUpdateFailureRemovesNewPolicyAndConfig(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	scheduler := &recordingScheduler{updateErr: errors.New("schedule unavailable")}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "New"}, RepoPath: "new-repo"},
		ResticPassword: "new-secret",
		Schedule:       "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.False(t, ack.Success)
	assert.Equal(t, "schedule unavailable", ack.Error)

	_, err = store.LoadPolicy()
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(configDir, "rclone.conf"))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(configDir, ".restic-password"))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestHandlePolicyPushInvalidSchedulePreservesExistingPolicyAndConfig(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	oldPolicy := &protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Old"}, RepoPath: "old-repo"},
		ResticPassword: "old-secret",
		BackupDirs:     []string{"/old"},
		Schedule:       "0 1 * * *",
	}
	require.NoError(t, store.SavePolicy(oldPolicy))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("old rclone config"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".restic-password"), []byte("old-secret"), 0o600))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   agentscheduler.New(),
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "New"}, RepoPath: "new-repo"},
		ResticPassword: "new-secret",
		BackupDirs:     []string{"/new"},
		Schedule:       "not a cron",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", ack.AgentID)
	assert.False(t, ack.Success)
	assert.NotEmpty(t, ack.Error)

	stored, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, oldPolicy, stored)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	assert.Equal(t, "old rclone config", string(rcloneConf))
	password, err := os.ReadFile(filepath.Join(configDir, ".restic-password"))
	require.NoError(t, err)
	assert.Equal(t, "old-secret", string(password))
}

func TestHandlePolicyPushReplaceFailurePreservesExistingPolicyAndConfig(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	oldPolicy := &protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Old"}, RepoPath: "old-repo"},
		ResticPassword: "old-secret",
		Schedule:       "0 1 * * *",
	}
	require.NoError(t, store.SavePolicy(oldPolicy))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("old rclone config"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(configDir, ".restic-password"), 0o700))
	scheduler := &recordingScheduler{}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   scheduler,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "New"}, RepoPath: "new-repo"},
		ResticPassword: "new-secret",
		Schedule:       "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	assert.False(t, ack.Success)
	assert.NotEmpty(t, ack.Error)
	assert.Len(t, scheduler.updates, 1)

	stored, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, oldPolicy, stored)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	assert.Equal(t, "old rclone config", string(rcloneConf))
}

func TestHandlePolicyPushReplacesLooseResticPasswordWithSecureFile(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	passwordPath := filepath.Join(configDir, ".restic-password")
	require.NoError(t, os.WriteFile(passwordPath, []byte("old-secret"), 0o644))
	oldInfo := fileInfo(t, passwordPath)
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   &recordingScheduler{},
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:        "agent-1",
		Storage:        protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}},
		ResticPassword: "new-secret",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&messages[0])
	require.NoError(t, err)
	require.True(t, ack.Success, ack.Error)
	assert.False(t, os.SameFile(oldInfo, fileInfo(t, passwordPath)))
	password, err := os.ReadFile(passwordPath)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", string(password))
	assertFileMode(t, passwordPath, 0o600)
}

func TestHandleBackupNowLoadsPolicyRunsBackupAndSendsTaskResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:         "agent-1",
		Storage:         protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupDirs:      []string{"/srv", "/home"},
		ExcludePatterns: []string{"*.tmp"},
		Retention:       protocol.RetentionPolicy{KeepLast: 4, KeepWeekly: 2},
	}))
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerConfig = cfg
			return executor.TaskResult{
				Type:       "backup",
				Status:     "success",
				SnapshotID: "snap-1",
				DurationMs: 1500,
				RepoSize:   4096,
			}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		BackupDirs: []string{"/srv", "/home"},
		Excludes:   []string{"*.tmp"},
		Retention:  executor.RetentionPolicy{KeepLast: 4, KeepWeekly: 2},
	}, withoutTaskLog(runnerConfig))
	assert.Equal(t, msg.ID, resultMsg.ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "backup", result.TaskType)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "snap-1", result.SnapshotID)
	assert.Equal(t, int64(1500), result.DurationMs)
	assert.Equal(t, int64(4096), result.RepoSize)
	assert.False(t, result.StartedAt.IsZero())
	assert.Equal(t, result.StartedAt.Add(1500*time.Millisecond), result.FinishedAt)
}

func TestRunBackupForPolicyStagesManifestForSnapshotAndCleansUp(t *testing.T) {
	configDir := t.TempDir()
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	var manifestPath string
	var manifestBytes []byte
	handler := NewHandler(HandlerConfig{
		ConfigDir:    configDir,
		SendFunc:     sent.send,
		AgentVersion: "test-version",
		PolicyStore:  policy.NewStore(t.TempDir()),
		DatabasePrepare: func(context.Context, agentdatabase.Config) (agentdatabase.Result, func(), error) {
			return agentdatabase.Result{}, func() {}, nil
		},
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerConfig = cfg
			for _, path := range cfg.BackupDirs {
				if filepath.Base(path) == protocol.BackupContentManifestName {
					manifestPath = path
				}
			}
			var err error
			manifestBytes, err = os.ReadFile(manifestPath)
			require.NoError(t, err)
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10, SnapshotID: "snap-1"}
		},
	})

	handler.runBackupForPolicy(context.Background(), "msg-1", "agent-1", &protocol.PolicyPushPayload{
		AgentID:         "agent-1",
		Storage:         protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}, RepoPath: "repo/agent-1"},
		BackupMode:      protocol.BackupModeSnapshot,
		BackupDirs:      []string{"/srv/site"},
		ExcludePatterns: []string{"*.log"},
	})

	require.NotEmpty(t, manifestPath)
	assert.Contains(t, manifestPath, "backup-manifest-")
	assert.Contains(t, runnerConfig.BackupDirs, manifestPath)
	assert.NoFileExists(t, manifestPath)

	var staged protocol.BackupContentManifest
	require.NoError(t, json.Unmarshal(manifestBytes, &staged))
	assert.Equal(t, protocol.BackupContentManifestVersion, staged.Version)
	assert.Equal(t, "agent-1", staged.Agent.ID)
	assert.Equal(t, "test-version", staged.Agent.Version)
	assert.Contains(t, staged.ExcludePatterns, "*.log")
	require.Len(t, staged.Sources.Paths, 2)
	assert.Equal(t, protocol.BackupContentManifestName, filepath.Base(staged.Sources.Paths[1].Path))
	assert.Equal(t, "manifest", staged.Sources.Paths[1].Kind)

	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	require.NotNil(t, result.Manifest)
	assert.Equal(t, protocol.BackupModeSnapshot, result.Manifest.BackupMode)
	assert.Equal(t, protocol.BackupModeSnapshot, result.Manifest.Artifact.Format)
}

func TestRunBackupForPolicyAddsManifestToArchiveAndRecordsArtifact(t *testing.T) {
	configDir := t.TempDir()
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		ConfigDir:    configDir,
		SendFunc:     sent.send,
		AgentVersion: "test-version",
		PolicyStore:  policy.NewStore(t.TempDir()),
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerConfig = cfg
			return executor.TaskResult{
				Type:                "backup",
				Status:              "success",
				DurationMs:          10,
				BackupMode:          protocol.BackupModeArchive,
				ArchiveFormat:       protocol.ArchiveFormatZip,
				ArtifactName:        "backup.zip",
				ArtifactPath:        "artifacts/backup.zip",
				ArtifactSize:        2048,
				ArtifactContentType: "application/zip",
			}
		},
	})

	handler.runBackupForPolicy(context.Background(), "msg-1", "agent-1", &protocol.PolicyPushPayload{
		AgentID:       "agent-1",
		Storage:       protocol.StorageConfig{RcloneType: "s3", RcloneConfig: map[string]string{"provider": "Other"}, RepoPath: "repo/agent-1"},
		BackupMode:    protocol.BackupModeArchive,
		ArchiveFormat: protocol.ArchiveFormatZip,
		BackupDirs:    []string{"/srv/site"},
	})

	require.Len(t, runnerConfig.ExtraArchiveFiles, 1)
	assert.Equal(t, protocol.BackupContentManifestName, runnerConfig.ExtraArchiveFiles[0].Name)
	assert.Contains(t, string(runnerConfig.ExtraArchiveFiles[0].Data), `"version": 1`)
	assert.NotContains(t, string(runnerConfig.ExtraArchiveFiles[0].Data), "artifacts/backup.zip")

	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	require.NotNil(t, result.Manifest)
	require.NotNil(t, result.Manifest.Artifact)
	assert.Equal(t, "backup.zip", result.Manifest.Artifact.Name)
	assert.Equal(t, "artifacts/backup.zip", result.Manifest.Artifact.Path)
	assert.Equal(t, int64(2048), result.Manifest.Artifact.Size)
	assert.Equal(t, "application/zip", result.Manifest.Artifact.ContentType)
}

func TestHandleBackupNowUsesInlinePolicyPayloadForArchive(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "rclone.log")
	binDir := t.TempDir()
	rclonePath := filepath.Join(binDir, "rclone")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuoteForShTest(logPath) + "\nexit 0\n"
	require.NoError(t, os.WriteFile(rclonePath, []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	backupDir := filepath.Join(t.TempDir(), "backup-src")
	require.NoError(t, os.MkdirAll(backupDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "hello.txt"), []byte("vaultfleet"), 0o644))
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:       "agent-1",
		Storage:       protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupMode:    protocol.BackupModeSnapshot,
		ArchiveFormat: protocol.ArchiveFormatTarGz,
		BackupDirs:    []string{backupDir},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{
		AgentID: "agent-1",
		Policy: &protocol.PolicyPushPayload{
			AgentID:         "agent-1",
			PlainBackup:     true,
			Storage:         protocol.StorageConfig{RepoPath: "repo/agent-1"},
			BackupMode:      protocol.BackupModeArchive,
			ArchiveFormat:   protocol.ArchiveFormatZip,
			BackupDirs:      []string{backupDir},
			ExcludePatterns: []string{},
		},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, protocol.BackupModeArchive, result.BackupMode)
	assert.Equal(t, protocol.ArchiveFormatZip, result.ArchiveFormat)
	assert.Equal(t, "application/zip", result.ArtifactContentType)
	assert.NotEmpty(t, result.ArtifactPath)

	data, err := os.ReadFile(filepath.Join(configDir, result.ArtifactPath))
	require.NoError(t, err)
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	assert.Contains(t, names, strings.TrimPrefix(filepath.ToSlash(filepath.Join(backupDir, "hello.txt")), "/"))
}

func shellQuoteForShTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestHandleBackupNowRefreshesRcloneConfWithObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "sftp",
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": "clear-sftp-password",
			},
			RepoPath: "vaultfleet/agent-1",
		},
		BackupDirs: []string{"/srv"},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, _ executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	assert.NotContains(t, string(rcloneConf), "clear-sftp-password")
	passValue := strings.TrimPrefix(strings.Split(strings.Split(string(rcloneConf), "pass = ")[1], "\n")[0], "")
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandleBackupNowPreservesLegacyObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	legacyPass, err := rcloneobscure.ObscurePass("clear-sftp-password")
	require.NoError(t, err)

	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType:         "sftp",
			RclonePassObscured: true,
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": legacyPass,
			},
			RepoPath: "vaultfleet/agent-1",
		},
		BackupDirs: []string{"/srv"},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, _ executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	passValue := rcloneConfValue(t, string(rcloneConf), "pass")
	assert.Equal(t, legacyPass, passValue)
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandleBackupNowUsesLegacyBackupRunnerWhenProgressRunnerUnset(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:    "agent-1",
		Storage:    protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupDirs: []string{"/srv"},
	}))
	sent := &sentMessages{}
	var calls atomic.Int32
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunner: func(_ context.Context, cfg executor.ExecutorConfig) executor.TaskResult {
			calls.Add(1)
			assert.Equal(t, executor.ExecutorConfig{
				ConfigDir:  configDir,
				RepoPath:   "repo/agent-1",
				BackupDirs: []string{"/srv"},
			}, withoutTaskLog(cfg))
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, msg.ID, resultMsg.ID)
}

func TestHandleBackupNowIncludesDatabaseDumpPaths(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	dumpPath := filepath.Join(t.TempDir(), "app.sql.gz")
	require.NoError(t, os.WriteFile(dumpPath, []byte("SQL"), 0o600))
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:    "agent-1",
		Storage:    protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupDirs: []string{"/srv"},
		BackupSources: []protocol.BackupSource{
			{
				Type: protocol.BackupSourceTypeDatabase,
				Database: &protocol.DatabaseBackupSource{
					Engine:        protocol.DatabaseEnginePostgreSQL,
					ExecutionMode: protocol.DatabaseExecutionHost,
					Username:      "postgres",
					Database:      "app",
				},
			},
		},
	}))
	var cleanupCalled atomic.Bool
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		DatabasePrepare: func(_ context.Context, cfg agentdatabase.Config) (agentdatabase.Result, func(), error) {
			require.Equal(t, configDir, cfg.ConfigDir)
			require.Len(t, cfg.Sources, 1)
			return agentdatabase.Result{
				Paths: []string{dumpPath},
				Metadata: &protocol.DatabaseBackupMetadata{Dumps: []protocol.DatabaseDumpMetadata{{
					Engine:        protocol.DatabaseEnginePostgreSQL,
					ExecutionMode: protocol.DatabaseExecutionHost,
					Database:      "app",
					OutputPath:    dumpPath,
					OutputName:    "app.sql.gz",
					Size:          3,
					Compressed:    true,
				}}},
			}, func() { cleanupCalled.Store(true) }, nil
		},
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			assert.Equal(t, []string{"/srv", dumpPath}, filterManifestBackupDirs(cfg.BackupDirs))
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	require.NotNil(t, result.Database)
	require.Len(t, result.Database.Dumps, 1)
	assert.Equal(t, "app.sql.gz", result.Database.Dumps[0].OutputName)
	assert.Eventually(t, cleanupCalled.Load, time.Second, 10*time.Millisecond)
}

func TestBackupNowSendsProgressMessages(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:    "agent-1",
		Storage:    protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupDirs: []string{"/srv"},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, progressFn executor.ProgressCallback) executor.TaskResult {
			assert.Equal(t, executor.ExecutorConfig{
				ConfigDir:  configDir,
				RepoPath:   "repo/agent-1",
				BackupDirs: []string{"/srv"},
			}, withoutTaskLog(cfg))
			progressFn("init", nil)
			progressFn("backup", &executor.BackupProgress{
				PercentDone: 50,
				TotalFiles:  4,
				FilesDone:   2,
				TotalBytes:  1000,
				BytesDone:   500,
				CurrentFile: "/srv/db.sqlite",
			})
			progressFn("backup", &executor.BackupProgress{
				PercentDone: 75,
				TotalFiles:  4,
				FilesDone:   3,
				TotalBytes:  1000,
				BytesDone:   750,
				CurrentFile: "/srv/cache.db",
			})
			time.Sleep(10 * time.Millisecond)
			progressFn("stats", &executor.BackupProgress{
				PercentDone: 100,
				TotalFiles:  4,
				FilesDone:   4,
				TotalBytes:  1000,
				BytesDone:   1000,
				CurrentFile: "/srv/final.db",
			})
			return executor.TaskResult{
				Type:       "backup",
				Status:     "success",
				SnapshotID: "snap-1",
				DurationMs: 1500,
			}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	progressMessages := sent.messagesOfType(protocol.TypeBackupProgress)
	require.Len(t, progressMessages, 3)
	assert.Equal(t, msg.ID, progressMessages[0].ID)
	initProgress, err := protocol.ParsePayload[protocol.BackupProgressPayload](&progressMessages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", initProgress.AgentID)
	assert.Equal(t, "init", initProgress.Phase)

	assert.Equal(t, msg.ID, progressMessages[1].ID)
	backupProgress, err := protocol.ParsePayload[protocol.BackupProgressPayload](&progressMessages[1])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", backupProgress.AgentID)
	assert.Equal(t, "backup", backupProgress.Phase)
	assert.Equal(t, float64(50), backupProgress.PercentDone)
	assert.Equal(t, int64(4), backupProgress.TotalFiles)
	assert.Equal(t, int64(2), backupProgress.FilesDone)
	assert.Equal(t, int64(1000), backupProgress.TotalBytes)
	assert.Equal(t, int64(500), backupProgress.BytesDone)
	assert.Equal(t, "/srv/db.sqlite", backupProgress.CurrentFile)

	assert.Equal(t, msg.ID, progressMessages[2].ID)
	statsProgress, err := protocol.ParsePayload[protocol.BackupProgressPayload](&progressMessages[2])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", statsProgress.AgentID)
	assert.Equal(t, "stats", statsProgress.Phase)
	assert.Equal(t, float64(100), statsProgress.PercentDone)
	assert.Equal(t, int64(4), statsProgress.TotalFiles)
	assert.Equal(t, int64(4), statsProgress.FilesDone)
	assert.Equal(t, int64(1000), statsProgress.TotalBytes)
	assert.Equal(t, int64(1000), statsProgress.BytesDone)
	assert.Positive(t, statsProgress.BytesPerSec)
	assert.Equal(t, "/srv/final.db", statsProgress.CurrentFile)

	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	assert.Equal(t, msg.ID, resultMessages[0].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "backup", result.TaskType)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "snap-1", result.SnapshotID)
}

func TestBackupProgressCallbackSendsFirstMeasuredProgressAfterPhaseMarker(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{SendFunc: sent.send})
	callback := handler.backupProgressCallback("backup-msg-1", "agent-1", nil)

	callback("backup", nil)
	callback("backup", &executor.BackupProgress{
		PercentDone: 25,
		TotalFiles:  4,
		FilesDone:   1,
		TotalBytes:  1000,
		BytesDone:   250,
		CurrentFile: "/srv/first.db",
	})
	callback("backup", &executor.BackupProgress{
		PercentDone: 50,
		TotalFiles:  4,
		FilesDone:   2,
		TotalBytes:  1000,
		BytesDone:   500,
		CurrentFile: "/srv/second.db",
	})

	progressMessages := sent.messagesOfType(protocol.TypeBackupProgress)
	require.Len(t, progressMessages, 2)
	firstMeasured, err := protocol.ParsePayload[protocol.BackupProgressPayload](&progressMessages[1])
	require.NoError(t, err)
	assert.Equal(t, "backup", firstMeasured.Phase)
	assert.Equal(t, float64(25), firstMeasured.PercentDone)
	assert.Equal(t, int64(250), firstMeasured.BytesDone)
	assert.Equal(t, "/srv/first.db", firstMeasured.CurrentFile)
}

func TestHandleBackupNowUsesConfiguredAgentIDWhenRequestAndPolicyOmitIt(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		AgentID:     "agent-1",
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
}

func TestHandleBackupNowPreventsOverlappingRuns(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	sent := &sentMessages{}
	var calls atomic.Int32
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	go func() {
		defer close(done)
		handler.Handle(*msg)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	handler.Handle(*msg)

	resultMessages := sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 1)
	assert.Equal(t, msg.ID, resultMessages[0].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[0])
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "backup already running", result.ErrorLog)
	assert.Equal(t, int32(1), calls.Load())

	close(release)
	<-done

	waitForMessageTypeCount(t, sent, protocol.TypeTaskResult, 2, time.Second)
	resultMessages = sent.messagesOfType(protocol.TypeTaskResult)
	require.Len(t, resultMessages, 2)
	assert.Equal(t, msg.ID, resultMessages[1].ID)
	result, err = protocol.ParsePayload[protocol.TaskResultPayload](&resultMessages[1])
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, int32(1), calls.Load())
}

func TestHandleBackupNowStartsAsyncAndDoesNotBlockHandle(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseRunner()
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			close(started)
			<-release
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backup runner did not start")
	}
	assert.Empty(t, sent.messagesOfType(protocol.TypeTaskResult))

	releaseRunner()
	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
}

func TestRunBackupForPolicyWarmsSnapshotCacheAndPrunesForgottenSnapshots(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	policyPayload := &protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}
	require.NoError(t, store.SavePolicy(policyPayload))

	cache := newSnapshotCache(configDir)
	require.NoError(t, cache.Put("snap-old", []executor.SnapshotFileEntry{{Path: "/old", Type: "dir"}}))
	require.NoError(t, cache.Put("snap-dead", []executor.SnapshotFileEntry{{Path: "/dead", Type: "dir"}}))

	var browseMu sync.Mutex
	var browsedSnapshots []string
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{
				Type:       "backup",
				Status:     "success",
				DurationMs: 25,
				SnapshotID: "snap-new",
				Snapshots: []executor.SnapshotInfo{
					{ID: "snap-old"},
					{ID: "snap-new"},
				},
			}
		},
		SnapshotBrowseRunner: func(_ context.Context, _ executor.ExecutorConfig, snapshotID string, path string) ([]executor.SnapshotFileEntry, error) {
			browseMu.Lock()
			browsedSnapshots = append(browsedSnapshots, snapshotID+":"+path)
			browseMu.Unlock()
			if snapshotID != "snap-new" {
				return nil, errors.New("unexpected browse for " + snapshotID)
			}
			return []executor.SnapshotFileEntry{{Path: "/new", Type: "dir"}}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "snap-new", result.SnapshotID)

	require.Eventually(t, func() bool {
		return cache.Has("snap-new") && !cache.Has("snap-dead")
	}, time.Second, 10*time.Millisecond)
	assert.True(t, cache.Has("snap-old"))

	cachedNew, ok, err := cache.Get("snap-new")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []executor.SnapshotFileEntry{{Path: "/new", Type: "dir"}}, cachedNew)

	browseMu.Lock()
	assert.Equal(t, []string{"snap-new:"}, browsedSnapshots)
	browseMu.Unlock()
}

func TestRunBackupForPolicyCacheWarmFailureKeepsSuccessResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))

	var browseCalls atomic.Int32
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{
				Type:       "backup",
				Status:     "success",
				DurationMs: 25,
				SnapshotID: "snap-new",
				Snapshots:  []executor.SnapshotInfo{{ID: "snap-new"}},
			}
		},
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			browseCalls.Add(1)
			return nil, errors.New("ls failed")
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Empty(t, result.ErrorLog)

	require.Eventually(t, func() bool {
		return browseCalls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	assert.False(t, newSnapshotCache(configDir).Has("snap-new"))
}

func TestCancelTaskStopsRunningBackup(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID:    "agent-1",
		Storage:    protocol.StorageConfig{RepoPath: "repo/agent-1"},
		BackupDirs: []string{"/data"},
		Retention:  protocol.RetentionPolicy{KeepLast: 3},
	}))
	started := make(chan struct{})
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		BackupRunnerWithProgress: func(ctx context.Context, cfg executor.ExecutorConfig, progressFn executor.ProgressCallback) executor.TaskResult {
			close(started)
			<-ctx.Done()
			return executor.TaskResult{Type: "backup", Status: "failed", ErrorLog: "context canceled"}
		},
	})
	backupMsg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*backupMsg)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backup runner did not start")
	}

	cancelMsg, err := protocol.NewMessage(protocol.TypeCancelTask, protocol.CancelTaskPayload{
		AgentID:   "agent-1",
		MessageID: backupMsg.ID,
	})
	require.NoError(t, err)
	handler.Handle(*cancelMsg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, 2*time.Second)
	payload, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", payload.Status)
	assert.Equal(t, "backup", payload.TaskType)
}

func TestCancelTaskIgnoresMismatchedAgentID(t *testing.T) {
	handler := NewHandler(HandlerConfig{AgentID: "agent-1"})
	cancelled := make(chan struct{})
	require.NoError(t, handler.tasks.Start("msg-1", taskTypeBackup, func(ctx context.Context) {
		<-ctx.Done()
		close(cancelled)
	}))
	defer handler.tasks.Cancel("msg-1")
	cancelMsg, err := protocol.NewMessage(protocol.TypeCancelTask, protocol.CancelTaskPayload{
		AgentID:   "agent-2",
		MessageID: "msg-1",
	})
	require.NoError(t, err)

	handler.Handle(*cancelMsg)

	assertNotClosed(t, cancelled, 20*time.Millisecond)
}

func TestCancelTaskIgnoresEmptyMessageID(t *testing.T) {
	handler := NewHandler(HandlerConfig{AgentID: "agent-1"})
	cancelled := make(chan struct{})
	require.NoError(t, handler.tasks.Start("", taskTypeBackup, func(ctx context.Context) {
		<-ctx.Done()
		close(cancelled)
	}))
	defer handler.tasks.Cancel("")
	cancelMsg, err := protocol.NewMessage(protocol.TypeCancelTask, protocol.CancelTaskPayload{
		AgentID: "agent-1",
	})
	require.NoError(t, err)

	handler.Handle(*cancelMsg)

	assertNotClosed(t, cancelled, 20*time.Millisecond)
}

func TestHandleBackupNowParseFailureUsesRequestMessageID(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		AgentID:  "agent-1",
		SendFunc: sent.send,
	})
	msg := protocol.Message{ID: "backup-msg-1", Type: protocol.TypeBackupNow, Payload: []byte("{")}

	handler.Handle(msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeTaskResult, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.ErrorLog, "parse backup_now")
}

func TestHandleBackupNowPersistsPendingResultWhenSendFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc: func(protocol.Message) error {
			return errors.New("offline")
		},
		BackupRunnerWithProgress: func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult {
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 10, SnapshotID: "snap-1"}
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	require.Eventually(t, func() bool {
		pending, err := store.LoadPendingResults()
		return err == nil && len(pending) == 1
	}, time.Second, 10*time.Millisecond)
	pending, err := store.LoadPendingResults()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, msg.ID, pending[0].MessageID)
	assert.Equal(t, "agent-1", pending[0].Payload.AgentID)
	assert.Equal(t, "backup", pending[0].Payload.TaskType)
	assert.Equal(t, "success", pending[0].Payload.Status)
	assert.Equal(t, "snap-1", pending[0].Payload.SnapshotID)
}

func TestHandleRestorePersistsPendingResultWithRequestMessageIDWhenSendFails(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc: func(msg protocol.Message) error {
			if msg.Type == protocol.TypeTaskResult {
				return errors.New("offline")
			}
			return nil
		},
		RestoreRunner: func(context.Context, executor.ExecutorConfig, string, string, []string) error {
			return nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: "snap-restore",
		Target:     "/restore/target",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	require.Eventually(t, func() bool {
		pending, err := store.LoadPendingResults()
		return err == nil && len(pending) == 1
	}, time.Second, 10*time.Millisecond)
	pending, err := store.LoadPendingResults()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, msg.ID, pending[0].MessageID)
	assert.Equal(t, "agent-1", pending[0].Payload.AgentID)
	assert.Equal(t, "restore", pending[0].Payload.TaskType)
	assert.Equal(t, "success", pending[0].Payload.Status)
	assert.Equal(t, "snap-restore", pending[0].Payload.SnapshotID)
}

func TestHandleBackupNowMissingPolicySendsFailureResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		AgentID:     "agent-1",
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeTaskResult, messages[0].Type)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "backup", result.TaskType)
	assert.Equal(t, "failed", result.Status)
	assert.True(t, strings.Contains(result.ErrorLog, "load policy"))
}

func TestHandleRestoreInvokesRunnerAndSendsSuccessTaskResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	var runnerSnapshotID string
	var runnerTarget string
	var runnerIncludePaths []string
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		RestoreRunner: func(_ context.Context, cfg executor.ExecutorConfig, snapshotID string, target string, includePaths []string) error {
			runnerConfig = cfg
			runnerSnapshotID = snapshotID
			runnerTarget = target
			runnerIncludePaths = includePaths
			return nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID:   "snap-1",
		Target:       "/restore/target",
		IncludePaths: []string{"/etc/hosts", "/srv/app"},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	}, runnerConfig)
	assert.Equal(t, "snap-1", runnerSnapshotID)
	assert.Equal(t, "/restore/target", runnerTarget)
	assert.Equal(t, []string{"/etc/hosts", "/srv/app"}, runnerIncludePaths)
	messages := sent.snapshot()
	require.Len(t, messages, 2)
	assert.Equal(t, protocol.TypeRestoreProgress, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	progress, err := protocol.ParsePayload[protocol.RestoreProgressPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", progress.AgentID)
	assert.Equal(t, "snap-1", progress.SnapshotID)
	assert.Equal(t, float64(0), progress.Percent)

	assert.Equal(t, protocol.TypeTaskResult, messages[1].Type)
	assert.Equal(t, msg.ID, messages[1].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[1])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "restore", result.TaskType)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "snap-1", result.SnapshotID)
	assert.Empty(t, result.ErrorLog)
	assert.False(t, result.StartedAt.IsZero())
	assert.False(t, result.FinishedAt.Before(result.StartedAt))
}

func TestHandleRestoreDockerContainerRestoresOriginalPathsAndStartsContainer(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
		},
	}))
	sent := &sentMessages{}
	var runnerTarget string
	var runnerIncludePaths []string
	var dockerRequest protocol.DockerRestoreRequest
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		RestoreRunner: func(_ context.Context, _ executor.ExecutorConfig, _ string, target string, includePaths []string) error {
			runnerTarget = target
			runnerIncludePaths = append([]string(nil), includePaths...)
			return nil
		},
		DockerRestoreRunner: func(_ context.Context, request protocol.DockerRestoreRequest) error {
			dockerRequest = request
			return nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSelectiveRestoreReq, protocol.RestoreReqPayload{
		SnapshotID:  "snap-1",
		Target:      "/ignored",
		RestoreMode: protocol.RestoreModeDockerContainer,
		Docker: &protocol.DockerRestoreRequest{
			Sources: []protocol.DockerResolvedSource{
				{
					ContainerID:   "container-1",
					Name:          "db",
					ResolvedPaths: []string{"/srv/app/compose.yml", "/var/lib/docker/volumes/db/_data"},
				},
			},
		},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	resultMsg := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, "/", runnerTarget)
	assert.Equal(t, []string{"/srv/app/compose.yml", "/var/lib/docker/volumes/db/_data"}, runnerIncludePaths)
	require.Len(t, dockerRequest.Sources, 1)
	assert.Equal(t, "container-1", dockerRequest.Sources[0].ContainerID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&resultMsg)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
}

func TestHandleRestorePreflightFileSuccess(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		AgentID:  "agent-1",
		SendFunc: sent.send,
	})
	target := filepath.Join(t.TempDir(), "restore-target")
	msg, err := protocol.NewMessage(protocol.TypeRestorePreflightReq, protocol.RestorePreflightReqPayload{
		AgentID:      "agent-1",
		SnapshotID:   "snap-1",
		Target:       target,
		IncludePaths: []string{"/etc/hosts"},
		RestoreMode:  protocol.RestoreModeFiles,
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeRestorePreflightResp, time.Second)
	assert.Equal(t, msg.ID, respMsg.ID)
	resp, err := protocol.ParsePayload[protocol.RestorePreflightRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", resp.AgentID)
	assert.Equal(t, "snap-1", resp.SnapshotID)
	assert.Equal(t, protocol.RestorePreflightStatusPassed, resp.Status)
	assertPreflightCheck(t, resp.Checks, "target_path_writable", protocol.RestorePreflightSeverityInfo)
}

func TestHandleRestorePreflightFileRejectsNonDirectoryTarget(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		AgentID:  "agent-1",
		SendFunc: sent.send,
	})
	target := filepath.Join(t.TempDir(), "target-file")
	require.NoError(t, os.WriteFile(target, []byte("not a directory"), 0o600))
	msg, err := protocol.NewMessage(protocol.TypeRestorePreflightReq, protocol.RestorePreflightReqPayload{
		AgentID:     "agent-1",
		SnapshotID:  "snap-1",
		Target:      target,
		RestoreMode: protocol.RestoreModeFiles,
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeRestorePreflightResp, time.Second)
	resp, err := protocol.ParsePayload[protocol.RestorePreflightRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, protocol.RestorePreflightStatusFailed, resp.Status)
	assertPreflightCheck(t, resp.Checks, "target_path_writable", protocol.RestorePreflightSeverityError)
}

func TestHandleRestorePreflightDockerUnavailable(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		AgentID:   "agent-1",
		SendFunc:  sent.send,
		DockerAPI: fakeAgentDockerAPI{pingErr: errors.New("permission denied")},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestorePreflightReq, protocol.RestorePreflightReqPayload{
		AgentID:     "agent-1",
		SnapshotID:  "snap-1",
		RestoreMode: protocol.RestoreModeDockerContainer,
		Docker: &protocol.DockerRestoreRequest{
			Sources: []protocol.DockerResolvedSource{{Name: "db", Image: "postgres:16", ResolvedPaths: []string{"/srv/db"}}},
		},
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeRestorePreflightResp, time.Second)
	resp, err := protocol.ParsePayload[protocol.RestorePreflightRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, protocol.RestorePreflightStatusFailed, resp.Status)
	assertPreflightCheck(t, resp.Checks, "docker_available", protocol.RestorePreflightSeverityError)
}

func TestHandleRestoreRefreshesRcloneConfWithObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "sftp",
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": "clear-sftp-password",
			},
			RepoPath: "vaultfleet/agent-1",
		},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("[vaultfleet]\ntype = sftp\npass = stale-clear\n"), 0o600))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		RestoreRunner: func(_ context.Context, _ executor.ExecutorConfig, _ string, _ string, _ []string) error {
			return nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: "snap-1",
		Target:     "/restore/target",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	passValue := rcloneConfValue(t, string(rcloneConf), "pass")
	assert.NotEqual(t, "stale-clear", passValue)
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandleRestoreRunnerFailureSendsFailedTaskResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		RestoreRunner: func(context.Context, executor.ExecutorConfig, string, string, []string) error {
			return errors.New("restore failed")
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: "snap-1",
		Target:     "/restore/target",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	messages := sent.snapshot()
	require.Len(t, messages, 2)
	assert.Equal(t, protocol.TypeRestoreProgress, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	progress, err := protocol.ParsePayload[protocol.RestoreProgressPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", progress.AgentID)
	assert.Equal(t, "snap-1", progress.SnapshotID)
	assert.Equal(t, float64(0), progress.Percent)

	assert.Equal(t, protocol.TypeTaskResult, messages[1].Type)
	assert.Equal(t, msg.ID, messages[1].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[1])
	require.NoError(t, err)
	assert.Equal(t, "restore", result.TaskType)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "snap-1", result.SnapshotID)
	assert.Contains(t, result.ErrorLog, "restore failed")
}

func TestHandleRestoreStartsAsyncAndDoesNotBlockHandle(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseRunner()
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		RestoreRunner: func(context.Context, executor.ExecutorConfig, string, string, []string) error {
			close(started)
			<-release
			return nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: "snap-1",
		Target:     "/restore/target",
	})
	require.NoError(t, err)

	handler.Handle(*msg)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("restore runner did not start")
	}
	assert.NotContains(t, messageTypes(sent.snapshot()), protocol.TypeTaskResult)

	releaseRunner()
	waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
}

func TestHandleRestoreMissingPolicySendsFailedTaskResult(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		AgentID:     "agent-1",
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: "snap-1",
		Target:     "/restore/target",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, msg.ID, messages[0].ID)
	result, err := protocol.ParsePayload[protocol.TaskResultPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", result.AgentID)
	assert.Equal(t, "restore", result.TaskType)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.ErrorLog, "no backup policy configured")
}

func TestHandleSnapshotListInvokesRunnerAndSendsResponseWithSameID(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
	}))
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotListRunner: func(_ context.Context, cfg executor.ExecutorConfig) ([]executor.SnapshotInfo, error) {
			runnerConfig = cfg
			return []executor.SnapshotInfo{
				{ID: "snap-1", Time: snapshotTime, Paths: []string{"/etc"}, Size: 512},
			}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotListResp, time.Second)
	assert.Equal(t, executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	}, runnerConfig)
	assert.Equal(t, msg.ID, respMsg.ID)
	payload, err := protocol.ParsePayload[protocol.SnapshotListRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", payload.AgentID)
	assert.Empty(t, payload.Error)
	require.Len(t, payload.Snapshots, 1)
	assert.Equal(t, "snap-1", payload.Snapshots[0].ID)
	assert.True(t, payload.Snapshots[0].Time.Equal(snapshotTime))
	assert.Equal(t, []string{"/etc"}, payload.Snapshots[0].Paths)
	assert.Equal(t, int64(512), payload.Snapshots[0].Size)
}

func TestHandleSnapshotListStartsAsyncAndDoesNotBlockHandle(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseRunner()
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		SnapshotListRunner: func(context.Context, executor.ExecutorConfig) ([]executor.SnapshotInfo, error) {
			close(started)
			<-release
			return []executor.SnapshotInfo{{ID: "snap-1"}}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("snapshot list runner did not start")
	}
	assert.Empty(t, sent.snapshot())

	releaseRunner()
	waitForMessageType(t, sent, protocol.TypeSnapshotListResp, time.Second)
}

func TestHandleSnapshotListRefreshesRcloneConfWithObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "sftp",
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": "clear-sftp-password",
			},
			RepoPath: "vaultfleet/agent-1",
		},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("[vaultfleet]\ntype = sftp\npass = stale-clear\n"), 0o600))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotListRunner: func(_ context.Context, _ executor.ExecutorConfig) ([]executor.SnapshotInfo, error) {
			return []executor.SnapshotInfo{{ID: "snap-1"}}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeSnapshotListResp, time.Second)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	passValue := rcloneConfValue(t, string(rcloneConf), "pass")
	assert.NotEqual(t, "stale-clear", passValue)
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandleSnapshotListMissingPolicySendsErrorPayload(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		AgentID:     "agent-1",
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: "agent-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeSnapshotListResp, messages[0].Type)
	assert.Equal(t, msg.ID, messages[0].ID)
	payload, err := protocol.ParsePayload[protocol.SnapshotListRespPayload](&messages[0])
	require.NoError(t, err)
	assert.Equal(t, "agent-1", payload.AgentID)
	assert.Contains(t, payload.Error, "no backup policy configured")
	assert.Nil(t, payload.Snapshots)
}

func TestHandleSnapshotBrowseInvokesRunnerAndSendsResponseWithSameID(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
			RcloneArgs: map[string]string{
				"transfers": "2",
				"tpslimit":  "4",
			},
		},
		BackupDirs:      []string{"/srv"},
		ExcludePatterns: []string{"*.tmp"},
		Retention:       protocol.RetentionPolicy{KeepLast: 2},
	}))
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	var runnerSnapshotID string
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(_ context.Context, cfg executor.ExecutorConfig, snapshotID string, _ string) ([]executor.SnapshotFileEntry, error) {
			runnerConfig = cfg
			runnerSnapshotID = snapshotID
			return []executor.SnapshotFileEntry{
				{Path: "/srv", Type: "dir", Size: 0, Mtime: "2026-05-22T08:00:00Z"},
				{Path: "/srv/app.db", Type: "file", Size: 4096, Mtime: "2026-05-22T08:01:00Z"},
			}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{SnapshotID: "snap-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	assert.Equal(t, executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		BackupDirs: []string{"/srv"},
		Excludes:   []string{"*.tmp"},
		Retention:  executor.RetentionPolicy{KeepLast: 2},
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	}, runnerConfig)
	assert.Equal(t, "snap-1", runnerSnapshotID)
	assert.Equal(t, msg.ID, respMsg.ID)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, "snap-1", payload.SnapshotID)
	assert.Empty(t, payload.Error)
	require.Len(t, payload.Entries, 1, "empty path should return only top-level entries")
	assert.Equal(t, protocol.SnapshotFileEntry{Path: "/srv", Type: "dir", Size: 0, Mtime: "2026-05-22T08:00:00Z"}, payload.Entries[0])
}

func TestHandleSnapshotBrowseUsesCacheWhenAvailable(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	cache := newSnapshotCache(configDir)
	require.NoError(t, cache.Put("snap-1", []executor.SnapshotFileEntry{
		{Path: "/srv", Type: "dir", Size: 0},
		{Path: "/srv/app", Type: "dir", Size: 0},
		{Path: "/srv/app/main.go", Type: "file", Size: 100},
		{Path: "/srv/data.db", Type: "file", Size: 4096},
	}))

	var runnerCalls atomic.Int32
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			runnerCalls.Add(1)
			return nil, errors.New("runner should not be called")
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{
		SnapshotID: "snap-1",
		Path:       "/srv",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Empty(t, payload.Error)
	assert.Equal(t, int32(0), runnerCalls.Load())
	require.Len(t, payload.Entries, 2)
	assert.Equal(t, "/srv/app", payload.Entries[0].Path)
	assert.Equal(t, "/srv/data.db", payload.Entries[1].Path)
}

func TestHandleSnapshotBrowseFetchesFullTreeOnCacheMissAndStoresIt(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))

	fullEntries := []executor.SnapshotFileEntry{
		{Path: "/srv", Type: "dir", Size: 0},
		{Path: "/srv/app", Type: "dir", Size: 0},
		{Path: "/srv/app/main.go", Type: "file", Size: 100},
		{Path: "/srv/data.db", Type: "file", Size: 4096},
	}
	var runnerMu sync.Mutex
	var runnerPaths []string
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(_ context.Context, _ executor.ExecutorConfig, _ string, path string) ([]executor.SnapshotFileEntry, error) {
			runnerMu.Lock()
			runnerPaths = append(runnerPaths, path)
			runnerMu.Unlock()
			return append([]executor.SnapshotFileEntry(nil), fullEntries...), nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{
		SnapshotID: "snap-1",
		Path:       "/srv",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Empty(t, payload.Error)
	require.Len(t, payload.Entries, 2)
	assert.Equal(t, "/srv/app", payload.Entries[0].Path)
	assert.Equal(t, "/srv/data.db", payload.Entries[1].Path)

	runnerMu.Lock()
	assert.Equal(t, []string{""}, runnerPaths)
	runnerMu.Unlock()

	cached, ok, err := newSnapshotCache(configDir).Get("snap-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, fullEntries, cached)
}

func TestHandleSnapshotBrowseRunnerFailureSendsErrorPayload(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
		},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			return nil, errors.New("browse failed")
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{SnapshotID: "snap-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	assert.Equal(t, msg.ID, respMsg.ID)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Equal(t, "snap-1", payload.SnapshotID)
	assert.Equal(t, "browse failed", payload.Error)
	assert.Nil(t, payload.Entries)
}

func TestHandleSnapshotBrowseStartsAsyncAndDoesNotBlockHandle(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseRunner()
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			close(started)
			<-release
			return []executor.SnapshotFileEntry{{Path: "/srv", Type: "dir"}}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{SnapshotID: "snap-1"})
	require.NoError(t, err)

	handler.Handle(*msg)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("snapshot browse runner did not start")
	}
	assert.Empty(t, sent.snapshot())

	releaseRunner()
	waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
}

func TestHandleSnapshotBrowseRefreshesRcloneConfWithObscuredSFTPPassword(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := t.TempDir()
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType: "sftp",
			RcloneConfig: map[string]string{
				"host": "sftp.example.test",
				"user": "vaultfleet",
				"pass": "clear-sftp-password",
			},
			RepoPath: "vaultfleet/agent-1",
		},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("[vaultfleet]\ntype = sftp\npass = stale-clear\n"), 0o600))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(_ context.Context, _ executor.ExecutorConfig, _ string, _ string) ([]executor.SnapshotFileEntry, error) {
			return []executor.SnapshotFileEntry{{Path: "/srv", Type: "dir"}}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{SnapshotID: "snap-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	rcloneConf, err := os.ReadFile(filepath.Join(configDir, "rclone.conf"))
	require.NoError(t, err)
	passValue := rcloneConfValue(t, string(rcloneConf), "pass")
	assert.NotEqual(t, "stale-clear", passValue)
	revealed, err := rcloneobscure.RevealPass(passValue)
	require.NoError(t, err)
	assert.Equal(t, "clear-sftp-password", revealed)
}

func TestHandleSnapshotBrowseResponseTooLargeSendsErrorPayload(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RepoPath: "repo/agent-1",
		},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			return []executor.SnapshotFileEntry{
				{Path: "/" + strings.Repeat("a", maxSnapshotBrowseResponseBytes), Type: "file"},
			}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{SnapshotID: "snap-1"})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Contains(t, payload.Error, "snapshot browse response too large")
	assert.Nil(t, payload.Entries)
	assert.Less(t, len(respMsg.Payload), maxSnapshotBrowseResponseBytes)
}

func TestHandleSnapshotBrowseWithPathFiltersDirectChildren(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{RepoPath: "repo/agent-1"},
	}))
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   t.TempDir(),
		SendFunc:    sent.send,
		SnapshotBrowseRunner: func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error) {
			return []executor.SnapshotFileEntry{
				{Path: "/srv/app", Type: "dir", Size: 0},
				{Path: "/srv/app/main.go", Type: "file", Size: 100},
				{Path: "/srv/data.db", Type: "file", Size: 4096},
			}, nil
		},
	})
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseReq, protocol.SnapshotBrowseReqPayload{
		SnapshotID: "snap-1",
		Path:       "/srv",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	respMsg := waitForMessageType(t, sent, protocol.TypeSnapshotBrowseResp, time.Second)
	payload, err := protocol.ParsePayload[protocol.SnapshotBrowseRespPayload](&respMsg)
	require.NoError(t, err)
	assert.Empty(t, payload.Error)
	require.Len(t, payload.Entries, 2, "should return only direct children of /srv")
	assert.Equal(t, "/srv/app", payload.Entries[0].Path)
	assert.Equal(t, "/srv/data.db", payload.Entries[1].Path)
}

func TestFilterTopLevelEntries(t *testing.T) {
	entries := []executor.SnapshotFileEntry{
		{Path: "/root", Type: "dir", Size: 0},
		{Path: "/root/Docker", Type: "dir", Size: 0},
		{Path: "/root/Docker/compose.yml", Type: "file", Size: 512},
		{Path: "/etc", Type: "dir", Size: 0},
		{Path: "/etc/nginx", Type: "dir", Size: 0},
		{Path: "/etc/nginx/nginx.conf", Type: "file", Size: 256},
	}

	result := filterTopLevelEntries(entries)

	require.Len(t, result, 2)
	assert.Equal(t, "/root", result[0].Path)
	assert.Equal(t, "/etc", result[1].Path)
}

func TestFilterTopLevelEntriesSkipsRootSlash(t *testing.T) {
	entries := []executor.SnapshotFileEntry{
		{Path: "/", Type: "dir", Size: 0},
		{Path: "/home", Type: "dir", Size: 0},
		{Path: "/home/user", Type: "dir", Size: 0},
	}

	result := filterTopLevelEntries(entries)

	require.Len(t, result, 1)
	assert.Equal(t, "/home", result[0].Path)
}

func TestHandlerDirBrowseReqSendsResponseWithSameID(t *testing.T) {
	var browsedPath string
	var browsedDepth int
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(fsRoot string, scanPath string, maxDepth int) ([]protocol.DirEntry, error) {
			assert.Equal(t, "/", fsRoot)
			browsedPath = scanPath
			browsedDepth = maxDepth
			return []protocol.DirEntry{{Path: "/etc", Type: "dir", Size: 4096}}, nil
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{Path: "/etc", Depth: 3})
	require.NoError(t, err)

	handler.Handle(*req)

	assert.Equal(t, "/etc", browsedPath)
	assert.Equal(t, 3, browsedDepth)
	resp := <-sent
	assert.Equal(t, protocol.TypeDirBrowseResp, resp.Type)
	assert.Equal(t, req.ID, resp.ID)
	payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/etc", payload.Path)
	assert.Empty(t, payload.Error)
	assert.Equal(t, []protocol.DirEntry{{Path: "/etc", Type: "dir", Size: 4096}}, payload.Entries)
}

func TestHandlerDockerDiscoveryReqSendsResponseWithSameID(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		SendFunc:  sent.send,
		DockerAPI: fakeAgentDockerAPI{containers: []agentdocker.ContainerSummary{{ID: "container-1", Names: []string{"/db"}, Image: "postgres:16", State: "running", Mounts: []agentdocker.Mount{{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db-data/_data", Destination: "/data", RW: true}}}}},
	})

	req, err := protocol.NewMessage(protocol.TypeDockerDiscoveryReq, protocol.DockerDiscoveryReqPayload{})
	require.NoError(t, err)
	handler.Handle(*req)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeDockerDiscoveryResp, messages[0].Type)
	assert.Equal(t, req.ID, messages[0].ID)
	payload, err := protocol.ParsePayload[protocol.DockerDiscoveryRespPayload](&messages[0])
	require.NoError(t, err)
	assert.True(t, payload.Available)
	require.Len(t, payload.Containers, 1)
	assert.Equal(t, "container-1", payload.Containers[0].ID)
}

func TestHandlerDatabaseDiscoveryReqSendsResponseWithSameID(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		ConfigDir: t.TempDir(),
		SendFunc:  sent.send,
		DatabaseList: func(_ context.Context, cfg agentdatabase.ListConfig) ([]string, error) {
			assert.NotEmpty(t, cfg.ConfigDir)
			assert.Equal(t, protocol.DatabaseEnginePostgreSQL, cfg.Source.Engine)
			assert.Equal(t, "postgres", cfg.Source.Username)
			return []string{"app", "analytics"}, nil
		},
	})

	req, err := protocol.NewMessage(protocol.TypeDatabaseDiscoveryReq, protocol.DatabaseDiscoveryReqPayload{
		Source: protocol.DatabaseBackupSource{
			Engine:        protocol.DatabaseEnginePostgreSQL,
			ExecutionMode: protocol.DatabaseExecutionHost,
			Username:      "postgres",
			Password:      "secret",
		},
	})
	require.NoError(t, err)
	handler.Handle(*req)

	messages := sent.snapshot()
	require.Len(t, messages, 1)
	assert.Equal(t, protocol.TypeDatabaseDiscoveryResp, messages[0].Type)
	assert.Equal(t, req.ID, messages[0].ID)
	payload, err := protocol.ParsePayload[protocol.DatabaseDiscoveryRespPayload](&messages[0])
	require.NoError(t, err)
	assert.True(t, payload.Available)
	assert.Equal(t, []string{"app", "analytics"}, payload.Databases)
}

func TestHandlerBackupResolvesDockerSourcesBeforeRunner(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	configDir := filepath.Join(t.TempDir(), "config")
	mountPath := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(mountPath, 0o755))
	sent := &sentMessages{}
	var runnerConfig executor.ExecutorConfig
	handler := NewHandler(HandlerConfig{
		PolicyStore: store,
		ConfigDir:   configDir,
		Scheduler:   &recordingScheduler{},
		SendFunc:    sent.send,
		DockerAPI: fakeAgentDockerAPI{
			containers: []agentdocker.ContainerSummary{{ID: "container-1", Names: []string{"/db"}}},
			inspects: map[string]agentdocker.ContainerInspect{
				"container-1": fakeInspect("container-1", "/db", "postgres:16", "running", []agentdocker.Mount{{Type: "bind", Source: mountPath, Destination: "/data", RW: true}}),
			},
		},
		BackupRunnerWithProgress: func(_ context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
			runnerConfig = cfg
			return executor.TaskResult{Type: "backup", Status: "success", DurationMs: 1}
		},
	})
	push, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID: "agent-1",
		Storage: protocol.StorageConfig{
			RcloneType:   "s3",
			RcloneConfig: map[string]string{"provider": "Other"},
			RepoPath:     "bucket/agent-1",
		},
		BackupDirs: []string{"/srv"},
		BackupSources: []protocol.BackupSource{
			{Type: protocol.BackupSourceTypePath, Path: "/srv"},
			{Type: protocol.BackupSourceTypeDockerContainer, DockerContainer: &protocol.DockerContainerBackupSource{ContainerID: "container-1", IncludeBindMounts: true}},
		},
	})
	require.NoError(t, err)
	handler.Handle(*push)

	backup, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: "agent-1"})
	require.NoError(t, err)
	handler.Handle(*backup)
	result := waitForMessageType(t, sent, protocol.TypeTaskResult, time.Second)
	assert.Equal(t, []string{"/srv", mountPath}, filterManifestBackupDirs(runnerConfig.BackupDirs))
	payload, err := protocol.ParsePayload[protocol.TaskResultPayload](&result)
	require.NoError(t, err)
	require.NotNil(t, payload.Docker)
	require.Len(t, payload.Docker.Sources, 1)
	assert.Equal(t, []string{mountPath}, payload.Docker.Sources[0].ResolvedPaths)
}

func TestHandlerDirBrowseReqNormalizesInvalidDepth(t *testing.T) {
	var browsedDepth int
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(_ string, _ string, maxDepth int) ([]protocol.DirEntry, error) {
			browsedDepth = maxDepth
			return nil, nil
		},
		SendFunc: func(protocol.Message) error {
			return nil
		},
	})
	rawPayload, err := json.Marshal(protocol.DirBrowseReqPayload{Path: "/var", Depth: 99})
	require.NoError(t, err)

	handler.Handle(protocol.Message{Type: protocol.TypeDirBrowseReq, ID: "browse-1", Payload: rawPayload})

	assert.Equal(t, 2, browsedDepth)
}

func TestHandlerDirBrowseReqSendsErrorPayload(t *testing.T) {
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(string, string, int) ([]protocol.DirEntry, error) {
			return nil, errors.New("permission denied")
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{Path: "/root", Depth: 2})
	require.NoError(t, err)

	handler.Handle(*req)

	resp := <-sent
	payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/root", payload.Path)
	assert.Equal(t, "permission denied", payload.Error)
	assert.Nil(t, payload.Entries)
}

func TestRunRestoreAppliesRcloneArgs(t *testing.T) {
	configDir := t.TempDir()
	writeResticPassword(t, configDir, "test-password")
	argsFile := writeAgentFakeRestic(t, configDir, "")

	err := runRestore(context.Background(), executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	}, "snap-1", "/restore", nil)

	require.NoError(t, err)
	assertAgentResticArgsContain(t, argsFile, "--tpslimit 4 --transfers 2")
}

func TestRunSnapshotListAppliesRcloneArgs(t *testing.T) {
	configDir := t.TempDir()
	writeResticPassword(t, configDir, "test-password")
	argsFile := writeAgentFakeRestic(t, configDir, "[]\n")

	_, err := runSnapshotList(context.Background(), executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	})

	require.NoError(t, err)
	assertAgentResticArgsContain(t, argsFile, "--tpslimit 4 --transfers 2")
}

func TestRunSnapshotBrowseAppliesRcloneArgs(t *testing.T) {
	configDir := t.TempDir()
	writeResticPassword(t, configDir, "test-password")
	argsFile := writeAgentFakeRestic(t, configDir, "")

	_, err := runSnapshotBrowse(context.Background(), executor.ExecutorConfig{
		ConfigDir:  configDir,
		RepoPath:   "repo/agent-1",
		RcloneArgs: map[string]string{"transfers": "2", "tpslimit": "4"},
	}, "snap-1", "/srv")

	require.NoError(t, err)
	assertAgentResticArgsContain(t, argsFile, "--tpslimit 4 --transfers 2")
}

func TestHandlerDirSizeReqSendsResponse(t *testing.T) {
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(string, string, int) ([]protocol.DirEntry, error) {
			return nil, nil
		},
		DirSizeFunc: func(fsRoot string, path string) (int64, error) {
			assert.Equal(t, "/", fsRoot)
			assert.Equal(t, "/home/data", path)
			return 1073741824, nil
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirSizeReq, protocol.DirSizeReqPayload{Path: "/home/data"})
	require.NoError(t, err)

	handler.Handle(*req)

	resp := <-sent
	assert.Equal(t, protocol.TypeDirSizeResp, resp.Type)
	assert.Equal(t, req.ID, resp.ID)
	payload, err := protocol.ParsePayload[protocol.DirSizeRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/home/data", payload.Path)
	assert.Equal(t, int64(1073741824), payload.Size)
	assert.Empty(t, payload.Error)
}

func TestHandlerDirSizeReqSendsErrorPayload(t *testing.T) {
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		DirSizeFunc: func(string, string) (int64, error) {
			return 0, errors.New("permission denied")
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirSizeReq, protocol.DirSizeReqPayload{Path: "/root"})
	require.NoError(t, err)

	handler.Handle(*req)

	resp := <-sent
	payload, err := protocol.ParsePayload[protocol.DirSizeRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/root", payload.Path)
	assert.Equal(t, "permission denied", payload.Error)
	assert.Equal(t, int64(0), payload.Size)
}

type scheduledUpdate struct {
	agentID  string
	schedule string
	fn       func()
}

type recordingScheduler struct {
	updates     []scheduledUpdate
	removed     []string
	err         error
	validateErr error
	updateErr   error
}

func (s *recordingScheduler) Validate(string) error {
	if s.validateErr != nil {
		return s.validateErr
	}
	return s.err
}

func (s *recordingScheduler) UpdateSchedule(agentID string, schedule string, fn func()) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.err != nil {
		return s.err
	}
	s.updates = append(s.updates, scheduledUpdate{agentID: agentID, schedule: schedule, fn: fn})
	return nil
}

func (s *recordingScheduler) RemoveJob(agentID string) {
	s.removed = append(s.removed, agentID)
}

type sentMessages struct {
	mu       sync.Mutex
	messages []protocol.Message
}

func (s *sentMessages) send(msg protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	return nil
}

func (s *sentMessages) snapshot() []protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Message(nil), s.messages...)
}

func (s *sentMessages) messagesOfType(msgType string) []protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]protocol.Message, 0, len(s.messages))
	for _, msg := range s.messages {
		if msg.Type == msgType {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func withoutTaskLog(cfg executor.ExecutorConfig) executor.ExecutorConfig {
	cfg.TaskLog = nil
	cfg.BackupDirs = filterManifestBackupDirs(cfg.BackupDirs)
	cfg.ExtraArchiveFiles = filterManifestExtraArchiveFiles(cfg.ExtraArchiveFiles)
	return cfg
}

func filterManifestBackupDirs(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.Base(path) == protocol.BackupContentManifestName && strings.Contains(path, "backup-manifest-") {
			continue
		}
		filtered = append(filtered, path)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func filterManifestExtraArchiveFiles(files []executor.ArchiveExtraFile) []executor.ArchiveExtraFile {
	filtered := make([]executor.ArchiveExtraFile, 0, len(files))
	for _, file := range files {
		if file.Name == protocol.BackupContentManifestName {
			continue
		}
		filtered = append(filtered, file)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func waitForMessageType(t *testing.T, sent *sentMessages, msgType string, timeout time.Duration) protocol.Message {
	t.Helper()
	return waitForMessageTypeCount(t, sent, msgType, 1, timeout)
}

func waitForMessageTypeCount(t *testing.T, sent *sentMessages, msgType string, count int, timeout time.Duration) protocol.Message {
	t.Helper()
	deadline := time.After(timeout)
	for {
		msgs := sent.snapshot()
		seen := 0
		for _, msg := range msgs {
			if msg.Type != msgType {
				continue
			}
			seen++
			if seen == count {
				return msg
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for message type %s count %d, got %d messages: %v", msgType, count, len(msgs), messageTypes(msgs))
			return protocol.Message{}
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func messageTypes(msgs []protocol.Message) []string {
	types := make([]string, len(msgs))
	for i, msg := range msgs {
		types[i] = msg.Type
	}
	return types
}

func assertPreflightCheck(t *testing.T, checks []protocol.RestorePreflightCheck, code string, severity string) {
	t.Helper()
	for _, check := range checks {
		if check.Code == code && check.Severity == severity {
			return
		}
	}
	t.Fatalf("preflight check %s with severity %s not found in %#v", code, severity, checks)
}

type fakeAgentDockerAPI struct {
	pingErr    error
	listErr    error
	containers []agentdocker.ContainerSummary
	inspects   map[string]agentdocker.ContainerInspect
}

func (f fakeAgentDockerAPI) Ping(context.Context) error {
	return f.pingErr
}

func (f fakeAgentDockerAPI) ListContainers(context.Context) ([]agentdocker.ContainerSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.containers, nil
}

func (f fakeAgentDockerAPI) InspectContainer(_ context.Context, id string) (agentdocker.ContainerInspect, error) {
	inspect, ok := f.inspects[id]
	if !ok {
		return agentdocker.ContainerInspect{}, errors.New("missing inspect")
	}
	return inspect, nil
}

func fakeInspect(id string, name string, image string, state string, mounts []agentdocker.Mount) agentdocker.ContainerInspect {
	inspect := agentdocker.ContainerInspect{ID: id, Name: name, Mounts: mounts}
	inspect.Config.Image = image
	inspect.State.Status = state
	return inspect
}

func assertNotClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("channel closed unexpectedly")
	case <-time.After(timeout):
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm())
}

func rcloneConfValue(t *testing.T, config string, key string) string {
	t.Helper()
	for _, line := range strings.Split(config, "\n") {
		prefix := key + " = "
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("config key %q not found in %q", key, config)
	return ""
}

func writeResticPassword(t *testing.T, dir string, password string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".restic-password"), []byte(password), 0o600))
}

func writeAgentFakeRestic(t *testing.T, dir string, stdout string) string {
	t.Helper()
	argsFile := filepath.Join(dir, "restic.args")
	script := filepath.Join(dir, "restic")
	content := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > \"$VAULTFLEET_RESTIC_ARGS_FILE\"\n" +
		"printf '%s' \"$VAULTFLEET_RESTIC_STDOUT\"\n"
	require.NoError(t, os.WriteFile(script, []byte(content), 0o755))
	t.Setenv("VAULTFLEET_RESTIC_ARGS_FILE", argsFile)
	t.Setenv("VAULTFLEET_RESTIC_STDOUT", stdout)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func assertAgentResticArgsContain(t *testing.T, argsFile string, want string) {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), want)
}

func fileInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info
}

type recordingUpdater struct {
	calls []struct{ version, repo string }
	mu    sync.Mutex
}

func (u *recordingUpdater) Update(version, repo string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, struct{ version, repo string }{version, repo})
	return nil
}

func TestHandlerVersionInfoTriggersUpdate(t *testing.T) {
	updater := &recordingUpdater{}
	handler := NewHandler(HandlerConfig{
		PolicyStore:  policy.NewStore(""),
		AgentVersion: "v1.0.0",
		Updater:      updater,
	})
	msg, err := protocol.NewMessage(protocol.TypeVersionInfo, protocol.VersionInfoPayload{
		Version:    "v2.0.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	// handleVersionInfo launches a goroutine, give it time to complete
	require.Eventually(t, func() bool {
		updater.mu.Lock()
		defer updater.mu.Unlock()
		return len(updater.calls) == 1
	}, time.Second, 10*time.Millisecond)
	updater.mu.Lock()
	defer updater.mu.Unlock()
	assert.Equal(t, "v2.0.0", updater.calls[0].version)
	assert.Equal(t, "shuguangnet/VaultFleet", updater.calls[0].repo)
}

func TestHandlerVersionInfoSkipsWhenSameVersion(t *testing.T) {
	updater := &recordingUpdater{}
	handler := NewHandler(HandlerConfig{
		PolicyStore:  policy.NewStore(""),
		AgentVersion: "v2.0.0",
		Updater:      updater,
	})
	msg, err := protocol.NewMessage(protocol.TypeVersionInfo, protocol.VersionInfoPayload{
		Version:    "v2.0.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	time.Sleep(50 * time.Millisecond)
	updater.mu.Lock()
	defer updater.mu.Unlock()
	assert.Empty(t, updater.calls)
}

func TestHandlerVersionInfoSkipsWhenNoUpdater(t *testing.T) {
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
	})
	msg, err := protocol.NewMessage(protocol.TypeVersionInfo, protocol.VersionInfoPayload{
		Version: "v2.0.0",
	})
	require.NoError(t, err)

	handler.Handle(*msg)
}

func TestHandlerUpdateAgentTriggersUpdateEvenWhenSameVersion(t *testing.T) {
	updater := &recordingUpdater{}
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore:  policy.NewStore(""),
		AgentVersion: "v2.0.0",
		Updater:      updater,
		SendFunc:     sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeUpdateAgent, protocol.UpdateAgentPayload{
		Version:    "v2.0.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	ack := waitForMessageType(t, sent, protocol.TypeUpdateAgentResp, time.Second)
	assert.Equal(t, msg.ID, ack.ID)
	payload, err := protocol.ParsePayload[protocol.UpdateAgentRespPayload](&ack)
	require.NoError(t, err)
	assert.True(t, payload.Accepted)
	assert.Equal(t, "v2.0.0", payload.Version)

	require.Eventually(t, func() bool {
		updater.mu.Lock()
		defer updater.mu.Unlock()
		return len(updater.calls) == 1
	}, time.Second, 10*time.Millisecond)
	updater.mu.Lock()
	defer updater.mu.Unlock()
	assert.Equal(t, "v2.0.0", updater.calls[0].version)
}

func TestHandlerUpdateAgentRespondsWhenUpdaterDisabled(t *testing.T) {
	sent := &sentMessages{}
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		SendFunc:    sent.send,
	})
	msg, err := protocol.NewMessage(protocol.TypeUpdateAgent, protocol.UpdateAgentPayload{
		Version: "v2.0.0",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	ack := waitForMessageType(t, sent, protocol.TypeUpdateAgentResp, time.Second)
	assert.Equal(t, msg.ID, ack.ID)
	payload, err := protocol.ParsePayload[protocol.UpdateAgentRespPayload](&ack)
	require.NoError(t, err)
	assert.False(t, payload.Accepted)
	assert.Equal(t, "agent self-update is disabled", payload.Error)
}
