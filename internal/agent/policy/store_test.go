package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestStore_SaveAndLoadPolicy(t *testing.T) {
	store := NewStore(t.TempDir())
	want := &protocol.PolicyPushPayload{
		AgentID: "agent-001",
		Storage: protocol.StorageConfig{
			RcloneType: "s3",
			RcloneConfig: map[string]string{
				"provider": "Cloudflare",
				"bucket":   "backups",
			},
			RepoPath: "vaultfleet/agent-001",
		},
		ResticPassword:  "secret",
		BackupDirs:      []string{"/etc", "/home"},
		ExcludePatterns: []string{"*.log", "node_modules"},
		Schedule:        "0 3 * * *",
		Retention: protocol.RetentionPolicy{
			KeepLast:   3,
			KeepDaily:  7,
			KeepWeekly: 4,
		},
	}

	require.NoError(t, store.SavePolicy(want))
	got, err := store.LoadPolicy()

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStore_LoadPolicyMissingReturnsError(t *testing.T) {
	store := NewStore(t.TempDir())

	policy, err := store.LoadPolicy()

	assert.Nil(t, policy)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestStore_SavePolicyFileAndDirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vaultfleet")
	store := NewStore(dir)

	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{AgentID: "agent-001"}))

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(filepath.Join(dir, PolicyFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestStore_SaveAndLoadPendingResults(t *testing.T) {
	store := NewStore(t.TempDir())
	startedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	results := []protocol.TaskResultPayload{
		{
			AgentID:    "agent-001",
			TaskType:   "backup",
			Status:     "success",
			SnapshotID: "snap-001",
			DurationMs: 1234,
			RepoSize:   4096,
			StartedAt:  startedAt,
			FinishedAt: startedAt.Add(1234 * time.Millisecond),
		},
		{
			AgentID:    "agent-001",
			TaskType:   "restore",
			Status:     "failed",
			DurationMs: 5678,
			ErrorLog:   "restore failed",
			StartedAt:  startedAt.Add(time.Hour),
			FinishedAt: startedAt.Add(time.Hour + 5678*time.Millisecond),
		},
	}

	require.NoError(t, store.SavePendingResults(results))
	got, err := store.LoadPendingResults()

	require.NoError(t, err)
	assert.Equal(t, results, got)

	fileInfo, err := os.Stat(filepath.Join(store.dir, PendingResultsFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestStore_LoadPendingResultsMissingReturnsNil(t *testing.T) {
	store := NewStore(t.TempDir())

	results, err := store.LoadPendingResults()

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestStore_ClearPendingResults(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.SavePendingResults([]protocol.TaskResultPayload{
		{AgentID: "agent-001", TaskType: "backup", Status: "success", DurationMs: 10},
	}))

	require.NoError(t, store.ClearPendingResults())
	results, err := store.LoadPendingResults()

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestStore_ClearPendingResultsMissingIsOK(t *testing.T) {
	store := NewStore(t.TempDir())

	assert.NoError(t, store.ClearPendingResults())
}

func TestNewStoreDefaultsEmptyDir(t *testing.T) {
	store := NewStore("")

	assert.Equal(t, DefaultDir, store.dir)
}
