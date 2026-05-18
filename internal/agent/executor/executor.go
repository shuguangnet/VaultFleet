package executor

import (
	"context"
	"path/filepath"
	"time"
)

type TaskResult struct {
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	DurationMs int64          `json:"duration_ms"`
	SnapshotID string         `json:"snapshot_id,omitempty"`
	RepoSize   int64          `json:"repo_size,omitempty"`
	Snapshots  []SnapshotInfo `json:"snapshots,omitempty"`
	ErrorLog   string         `json:"error_log,omitempty"`
}

type ExecutorConfig struct {
	ConfigDir  string
	RepoPath   string
	BackupDirs []string
	Excludes   []string
	Retention  RetentionPolicy
}

type resticExecutor interface {
	InitRepo(ctx context.Context) error
	RunBackup(ctx context.Context, dirs []string, excludes []string) (string, error)
	RunForget(ctx context.Context, retention RetentionPolicy) error
	ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
	RestoreSnapshot(ctx context.Context, snapshotID, targetPath string) error
}

type Executor struct {
	restic     resticExecutor
	backupDirs []string
	excludes   []string
	retention  RetentionPolicy
}

func NewExecutor(cfg ExecutorConfig) *Executor {
	return &Executor{
		restic: ResticRunner{
			RcloneConfPath: filepath.Join(cfg.ConfigDir, "rclone.conf"),
			PasswordFile:   filepath.Join(cfg.ConfigDir, ".restic-password"),
			RepoPath:       cfg.RepoPath,
		},
		backupDirs: append([]string(nil), cfg.BackupDirs...),
		excludes:   append([]string(nil), cfg.Excludes...),
		retention:  cfg.Retention,
	}
}

func (e *Executor) RunBackupJob(ctx context.Context) (result TaskResult) {
	start := time.Now()
	result = TaskResult{
		Type:   "backup",
		Status: "failed",
	}
	defer func() {
		result.DurationMs = time.Since(start).Milliseconds()
	}()

	if err := e.restic.InitRepo(ctx); err != nil {
		result.ErrorLog = "init: " + err.Error()
		return result
	}
	if _, err := e.restic.RunBackup(ctx, e.backupDirs, e.excludes); err != nil {
		result.ErrorLog = "backup: " + err.Error()
		return result
	}
	if err := e.restic.RunForget(ctx, e.retention); err != nil {
		result.ErrorLog = "forget: " + err.Error()
		return result
	}

	snapshots, err := e.restic.ListSnapshots(ctx)
	if err != nil {
		result.ErrorLog = "snapshots: " + err.Error()
		return result
	}

	result.Status = "success"
	result.ErrorLog = ""
	result.Snapshots = append([]SnapshotInfo(nil), snapshots...)
	if len(snapshots) > 0 {
		latest := snapshots[0]
		for _, snapshot := range snapshots[1:] {
			if snapshot.Time.After(latest.Time) {
				latest = snapshot
			}
		}
		result.SnapshotID = latest.ID
	}
	return result
}
