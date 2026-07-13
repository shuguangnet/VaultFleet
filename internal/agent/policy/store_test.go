package policy

import (
	"encoding/json"
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

func TestStore_SaveAndLoadMultiplePolicies(t *testing.T) {
	store := NewStore(t.TempDir())
	first := &protocol.PolicyPushPayload{PolicyID: "policy-a", AgentID: "agent-001", Schedule: "0 2 * * *"}
	second := &protocol.PolicyPushPayload{PolicyID: "policy-b", AgentID: "agent-001", Schedule: "30 2 * * *"}

	require.NoError(t, store.SavePolicy(first))
	require.NoError(t, store.SavePolicy(second))

	policies, err := store.LoadPolicies()
	require.NoError(t, err)
	require.Len(t, policies, 2)
	assert.Equal(t, first, policies[0])
	assert.Equal(t, second, policies[1])

	loaded, err := store.LoadPolicyByID("policy-a")
	require.NoError(t, err)
	assert.Equal(t, first, loaded)
}

func TestStore_LoadPoliciesDeduplicatesLegacyPolicy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	legacy := &protocol.PolicyPushPayload{PolicyID: "policy-a", AgentID: "agent-001", Schedule: "0 1 * * *"}
	legacyData, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, PolicyFileName), legacyData, 0o600))
	updated := &protocol.PolicyPushPayload{PolicyID: "policy-a", AgentID: "agent-001", Schedule: "0 2 * * *"}
	require.NoError(t, store.SavePolicy(updated))

	policies, err := store.LoadPolicies()
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, updated, policies[0])
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

func TestStore_SavePolicyTightensExistingFilePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vaultfleet")
	store := NewStore(dir)
	path := filepath.Join(dir, PolicyFileName)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"agent_id":"old"}`), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{AgentID: "agent-001"}))

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestStore_SaveAndLoadPendingResults(t *testing.T) {
	store := NewStore(t.TempDir())
	startedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	results := []PendingTaskResult{
		{
			Payload: protocol.TaskResultPayload{
				AgentID:    "agent-001",
				TaskType:   "backup",
				Status:     "success",
				SnapshotID: "snap-001",
				DurationMs: 1234,
				RepoSize:   4096,
				StartedAt:  startedAt,
				FinishedAt: startedAt.Add(1234 * time.Millisecond),
			},
		},
		{
			MessageID: "restore-message-1",
			Payload: protocol.TaskResultPayload{
				AgentID:    "agent-001",
				TaskType:   "restore",
				Status:     "failed",
				DurationMs: 5678,
				ErrorLog:   "restore failed",
				StartedAt:  startedAt.Add(time.Hour),
				FinishedAt: startedAt.Add(time.Hour + 5678*time.Millisecond),
			},
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

func TestStore_LoadPendingResultsAcceptsLegacyPayloadArray(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, PendingResultsFile), []byte(`[
		{"agent_id":"agent-001","task_type":"backup","status":"success","duration_ms":10,"repo_size":0,"started_at":"2026-05-18T10:00:00Z","finished_at":"2026-05-18T10:00:01Z"}
	]`), 0o600))

	got, err := store.LoadPendingResults()

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].MessageID)
	assert.Equal(t, "agent-001", got[0].Payload.AgentID)
	assert.Equal(t, "backup", got[0].Payload.TaskType)
}

func TestStore_SavePendingResultsTightensExistingFilePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vaultfleet")
	store := NewStore(dir)
	path := filepath.Join(dir, PendingResultsFile)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`[]`), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	require.NoError(t, store.SavePendingResults([]PendingTaskResult{
		{Payload: protocol.TaskResultPayload{AgentID: "agent-001", TaskType: "backup", Status: "success"}},
	}))

	fileInfo, err := os.Stat(path)
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
	require.NoError(t, store.SavePendingResults([]PendingTaskResult{
		{Payload: protocol.TaskResultPayload{AgentID: "agent-001", TaskType: "backup", Status: "success", DurationMs: 10}},
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
