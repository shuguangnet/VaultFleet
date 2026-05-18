package executor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewExecutorBuildsRunnerAndCopiesConfig(t *testing.T) {
	cfg := ExecutorConfig{
		ConfigDir:  "/var/lib/vaultfleet",
		RepoPath:   "repo/agent-1",
		BackupDirs: []string{"/home/alice", "/etc"},
		Excludes:   []string{"*.tmp"},
		Retention: RetentionPolicy{
			KeepLast:  3,
			KeepDaily: 7,
		},
	}

	executor := NewExecutor(cfg)

	if executor.restic == nil {
		t.Fatal("NewExecutor() restic runner is nil")
	}
	runner, ok := executor.restic.(ResticRunner)
	if !ok {
		t.Fatalf("NewExecutor() restic runner type = %T, want ResticRunner", executor.restic)
	}
	if runner.RcloneConfPath != filepath.Join(cfg.ConfigDir, "rclone.conf") {
		t.Fatalf("RcloneConfPath = %q", runner.RcloneConfPath)
	}
	if runner.PasswordFile != filepath.Join(cfg.ConfigDir, ".restic-password") {
		t.Fatalf("PasswordFile = %q", runner.PasswordFile)
	}
	if runner.RepoPath != cfg.RepoPath {
		t.Fatalf("RepoPath = %q, want %q", runner.RepoPath, cfg.RepoPath)
	}

	cfg.BackupDirs[0] = "/mutated"
	cfg.Excludes[0] = "mutated"
	if executor.backupDirs[0] != "/home/alice" {
		t.Fatalf("backup dirs were not copied: %#v", executor.backupDirs)
	}
	if executor.excludes[0] != "*.tmp" {
		t.Fatalf("excludes were not copied: %#v", executor.excludes)
	}
	if executor.retention.KeepLast != 3 || executor.retention.KeepDaily != 7 {
		t.Fatalf("retention = %+v", executor.retention)
	}
}

func TestRunBackupJobSuccessReturnsLatestSnapshotAndSnapshots(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	runner := &recordingRunner{
		backupDelay: 10 * time.Millisecond,
		snapshots: []SnapshotInfo{
			{ID: "old", Time: now.Add(-time.Hour)},
			{ID: "new", Time: now},
		},
	}
	executor := &Executor{
		restic:     runner,
		backupDirs: []string{"/data"},
		excludes:   []string{"*.tmp"},
		retention:  RetentionPolicy{KeepLast: 2},
	}

	result := executor.RunBackupJob(context.Background())

	if result.Type != "backup" {
		t.Fatalf("Type = %q, want backup", result.Type)
	}
	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	if result.SnapshotID != "new" {
		t.Fatalf("SnapshotID = %q, want new", result.SnapshotID)
	}
	if len(result.Snapshots) != 2 {
		t.Fatalf("Snapshots length = %d, want 2", len(result.Snapshots))
	}
	if result.DurationMs <= 0 {
		t.Fatalf("DurationMs = %d, want positive duration", result.DurationMs)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup", "forget", "snapshots"})
}

func TestRunBackupJobFailureStopsAtStageAndReturnsErrorLog(t *testing.T) {
	runner := &recordingRunner{backupErr: errors.New("disk read failed")}
	executor := &Executor{
		restic:     runner,
		backupDirs: []string{"/data"},
		retention:  RetentionPolicy{KeepLast: 1},
	}

	result := executor.RunBackupJob(context.Background())

	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.ErrorLog, "backup: disk read failed") {
		t.Fatalf("ErrorLog = %q, want backup stage and error", result.ErrorLog)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup"})
}

func TestTaskResultJSONUsesSnakeCaseProtocolKeys(t *testing.T) {
	result := TaskResult{
		Type:       "backup",
		Status:     "success",
		DurationMs: 123,
		SnapshotID: "abc123",
		RepoSize:   4096,
		Snapshots: []SnapshotInfo{
			{ID: "abc123", Hostname: "agent-1", Paths: []string{"/data"}},
		},
		ErrorLog: "",
	}

	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(TaskResult) error = %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("Unmarshal(TaskResult JSON) error = %v", err)
	}

	wantKeys := []string{"type", "status", "duration_ms", "snapshot_id", "repo_size", "snapshots"}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("TaskResult JSON missing key %q: %s", key, payload)
		}
	}
	disallowedKeys := []string{"Type", "Status", "DurationMs", "SnapshotID", "RepoSize", "Snapshots", "ErrorLog"}
	for _, key := range disallowedKeys {
		if _, ok := got[key]; ok {
			t.Fatalf("TaskResult JSON uses Go field key %q: %s", key, payload)
		}
	}
	if _, ok := got["error_log"]; ok {
		t.Fatalf("TaskResult JSON included empty error_log despite omitempty: %s", payload)
	}
}

func TestTaskResultJSONOmitsEmptyOptionalFields(t *testing.T) {
	result := TaskResult{
		Type:       "backup",
		Status:     "failed",
		DurationMs: 123,
	}

	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(TaskResult) error = %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("Unmarshal(TaskResult JSON) error = %v", err)
	}

	requiredKeys := []string{"type", "status", "duration_ms"}
	for _, key := range requiredKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("TaskResult JSON missing required key %q: %s", key, payload)
		}
	}
	optionalKeys := []string{"snapshot_id", "repo_size", "snapshots", "error_log"}
	for _, key := range optionalKeys {
		if _, ok := got[key]; ok {
			t.Fatalf("TaskResult JSON included empty optional key %q: %s", key, payload)
		}
	}
}

type recordingRunner struct {
	calls       []string
	initErr     error
	backupOut   string
	backupErr   error
	backupDelay time.Duration
	forgetErr   error
	snapshots   []SnapshotInfo
	snapshotErr error
	restoreErr  error
}

func (r *recordingRunner) InitRepo(context.Context) error {
	r.calls = append(r.calls, "init")
	return r.initErr
}

func (r *recordingRunner) RunBackup(_ context.Context, dirs []string, excludes []string) (string, error) {
	r.calls = append(r.calls, "backup")
	if r.backupDelay > 0 {
		time.Sleep(r.backupDelay)
	}
	return r.backupOut, r.backupErr
}

func (r *recordingRunner) RunForget(_ context.Context, retention RetentionPolicy) error {
	r.calls = append(r.calls, "forget")
	return r.forgetErr
}

func (r *recordingRunner) ListSnapshots(context.Context) ([]SnapshotInfo, error) {
	r.calls = append(r.calls, "snapshots")
	return r.snapshots, r.snapshotErr
}

func (r *recordingRunner) RestoreSnapshot(context.Context, string, string) error {
	r.calls = append(r.calls, "restore")
	return r.restoreErr
}

func assertRunnerCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("runner calls = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("runner calls = %#v, want %#v", got, want)
		}
	}
}
