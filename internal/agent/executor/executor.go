package executor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

type TaskResult struct {
	Type                string         `json:"type"`
	Status              string         `json:"status"`
	DurationMs          int64          `json:"duration_ms"`
	SnapshotID          string         `json:"snapshot_id,omitempty"`
	BackupMode          string         `json:"backup_mode,omitempty"`
	ArchiveFormat       string         `json:"archive_format,omitempty"`
	ArtifactPath        string         `json:"artifact_path,omitempty"`
	ArtifactName        string         `json:"artifact_name,omitempty"`
	ArtifactSize        int64          `json:"artifact_size,omitempty"`
	ArtifactContentType string         `json:"artifact_content_type,omitempty"`
	RepoSize            int64          `json:"repo_size,omitempty"`
	Snapshots           []SnapshotInfo `json:"snapshots,omitempty"`
	ErrorLog            string         `json:"error_log,omitempty"`
}

func (r TaskResult) ToProtocol(agentID string, startedAt time.Time) protocol.TaskResultPayload {
	snapshots := make([]protocol.SnapshotInfo, 0, len(r.Snapshots))
	for _, snapshot := range r.Snapshots {
		snapshots = append(snapshots, protocol.SnapshotInfo{
			ID:    snapshot.ID,
			Time:  snapshot.Time,
			Paths: append([]string(nil), snapshot.Paths...),
			Size:  snapshot.Size,
		})
	}

	return protocol.TaskResultPayload{
		AgentID:             agentID,
		TaskType:            r.Type,
		Status:              r.Status,
		SnapshotID:          r.SnapshotID,
		BackupMode:          r.BackupMode,
		ArchiveFormat:       r.ArchiveFormat,
		ArtifactPath:        r.ArtifactPath,
		ArtifactName:        r.ArtifactName,
		ArtifactSize:        r.ArtifactSize,
		ArtifactContentType: r.ArtifactContentType,
		DurationMs:          r.DurationMs,
		RepoSize:            r.RepoSize,
		ErrorLog:            r.ErrorLog,
		StartedAt:           startedAt,
		FinishedAt:          startedAt.Add(time.Duration(r.DurationMs) * time.Millisecond),
		Snapshots:           snapshots,
	}
}

type ExecutorConfig struct {
	ConfigDir      string
	RepoPath       string
	BackupDirs     []string
	Excludes       []string
	Retention      RetentionPolicy
	RcloneArgs     map[string]string
	PlainBackup    bool
	BackupMode     string
	ArchiveFormat  string
	PreBackupHook  *protocol.PolicyHook
	PostBackupHook *protocol.PolicyHook
}

type BackupProgress struct {
	PercentDone float64
	TotalFiles  int64
	FilesDone   int64
	TotalBytes  int64
	BytesDone   int64
	CurrentFile string
}

type ProgressCallback func(phase string, progress *BackupProgress)

type resticExecutor interface {
	InitRepo(ctx context.Context) error
	RunBackup(ctx context.Context, dirs []string, excludes []string) (string, error)
	RunForget(ctx context.Context, retention RetentionPolicy) error
	ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
	RepositorySize(ctx context.Context) (int64, error)
	RestoreSnapshot(ctx context.Context, snapshotID, targetPath string, includePaths []string) error
}

type resticExecutorWithProgress interface {
	RunBackupWithProgress(ctx context.Context, dirs []string, excludes []string, progressFn func(BackupProgress)) (string, error)
}

type Executor struct {
	restic     resticExecutor
	backupDirs []string
	excludes   []string
	retention  RetentionPolicy
}

func NewExecutor(cfg ExecutorConfig) *Executor {
	rcloneConfPath := filepath.Join(cfg.ConfigDir, "rclone.conf")
	passwordFile := filepath.Join(cfg.ConfigDir, ".restic-password")
	rcloneArgs := copyStringMap(cfg.RcloneArgs)
	usePlain := cfg.PlainBackup || !HasPasswordFile(passwordFile)

	var runner resticExecutor
	if usePlain {
		log.Printf("using plain rclone backup (no encryption)")
		runner = PlainRunner{
			RcloneConfPath:  rcloneConfPath,
			RepoPath:        cfg.RepoPath,
			RcloneExtraArgs: rcloneArgs,
		}
	} else {
		log.Printf("using encrypted restic backup")
		runner = ResticRunner{
			RcloneConfPath:  rcloneConfPath,
			PasswordFile:    passwordFile,
			RepoPath:        cfg.RepoPath,
			RcloneExtraArgs: rcloneArgs,
		}
	}

	return &Executor{
		restic:     runner,
		backupDirs: append([]string(nil), cfg.BackupDirs...),
		excludes:   append([]string(nil), cfg.Excludes...),
		retention:  cfg.Retention,
	}
}

func HasPasswordFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(data))) > 0
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func (e *Executor) RunBackupJob(ctx context.Context) (result TaskResult) {
	start := time.Now()
	result = TaskResult{Type: "backup", Status: "failed", BackupMode: protocol.BackupModeSnapshot}
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()

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
	repoSize, err := e.restic.RepositorySize(ctx)
	if err != nil {
		result.ErrorLog = "stats: " + err.Error()
		return result
	}

	result.Status = "success"
	result.ErrorLog = ""
	result.Snapshots = append([]SnapshotInfo(nil), snapshots...)
	result.RepoSize = repoSize
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

func (e *Executor) RunBackupJobWithProgress(ctx context.Context, progressFn ProgressCallback) (result TaskResult) {
	start := time.Now()
	result = TaskResult{Type: "backup", Status: "failed", BackupMode: protocol.BackupModeSnapshot}
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()

	emitProgress(progressFn, "init", nil)
	if err := e.restic.InitRepo(ctx); err != nil {
		result.ErrorLog = "init: " + err.Error()
		return result
	}

	emitProgress(progressFn, "backup", nil)
	if progressRunner, ok := e.restic.(resticExecutorWithProgress); ok {
		var resticProgressFn func(BackupProgress)
		if progressFn != nil {
			resticProgressFn = func(progress BackupProgress) {
				progressCopy := progress
				emitProgress(progressFn, "backup", &progressCopy)
			}
		}
		if _, err := progressRunner.RunBackupWithProgress(ctx, e.backupDirs, e.excludes, resticProgressFn); err != nil {
			result.ErrorLog = "backup: " + err.Error()
			return result
		}
	} else if _, err := e.restic.RunBackup(ctx, e.backupDirs, e.excludes); err != nil {
		result.ErrorLog = "backup: " + err.Error()
		return result
	}

	emitProgress(progressFn, "forget", nil)
	if err := e.restic.RunForget(ctx, e.retention); err != nil {
		result.ErrorLog = "forget: " + err.Error()
		return result
	}

	emitProgress(progressFn, "stats", nil)
	snapshots, err := e.restic.ListSnapshots(ctx)
	if err != nil {
		result.ErrorLog = "snapshots: " + err.Error()
		return result
	}
	repoSize, err := e.restic.RepositorySize(ctx)
	if err != nil {
		result.ErrorLog = "stats: " + err.Error()
		return result
	}

	result.Status = "success"
	result.ErrorLog = ""
	result.Snapshots = append([]SnapshotInfo(nil), snapshots...)
	result.RepoSize = repoSize
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

func RunArchiveJob(ctx context.Context, cfg ExecutorConfig) (result TaskResult) {
	start := time.Now()
	result = TaskResult{
		Type:          "backup",
		Status:        "failed",
		BackupMode:    protocol.BackupModeArchive,
		ArchiveFormat: normalizeArchiveFormat(cfg.ArchiveFormat),
	}
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()

	artifactDir := filepath.Join(cfg.ConfigDir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		result.ErrorLog = "archive: " + err.Error()
		return result
	}

	artifactName := "backup-" + time.Now().UTC().Format("20060102-150405") + archiveFileSuffix(result.ArchiveFormat)
	artifactPath := filepath.Join(artifactDir, artifactName)
	if err := writeArchive(artifactPath, result.ArchiveFormat, cfg.BackupDirs, cfg.Excludes); err != nil {
		result.ErrorLog = "archive: " + err.Error()
		return result
	}
	remoteArtifactPath := filepath.ToSlash(filepath.Join("artifacts", artifactName))
	runner := PlainRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	if err := runner.CopyFileToRemote(ctx, artifactPath, remoteArtifactPath); err != nil {
		result.ErrorLog = "archive-upload: " + err.Error()
		return result
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		result.ErrorLog = "archive-stat: " + err.Error()
		return result
	}

	result.Status = "success"
	result.ArtifactPath = remoteArtifactPath
	result.ArtifactName = artifactName
	result.ArtifactSize = info.Size()
	result.ArtifactContentType = archiveContentType(result.ArchiveFormat)
	result.RepoSize = info.Size()
	return result
}

func normalizeArchiveFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case protocol.ArchiveFormatZip:
		return protocol.ArchiveFormatZip
	default:
		return protocol.ArchiveFormatTarGz
	}
}

func archiveFileSuffix(format string) string {
	if normalizeArchiveFormat(format) == protocol.ArchiveFormatZip {
		return ".zip"
	}
	return ".tar.gz"
}

func archiveContentType(format string) string {
	if normalizeArchiveFormat(format) == protocol.ArchiveFormatZip {
		return "application/zip"
	}
	return "application/gzip"
}

func writeArchive(output string, format string, dirs []string, excludes []string) error {
	if normalizeArchiveFormat(format) == protocol.ArchiveFormatZip {
		return writeZipArchive(output, dirs, excludes)
	}
	return writeTarGzArchive(output, dirs, excludes)
}

func writeTarGzArchive(output string, dirs []string, excludes []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()

	for _, root := range dirs {
		root = filepath.Clean(root)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			for _, exclude := range excludes {
				if exclude != "" && strings.Contains(path, exclude) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = strings.TrimPrefix(filepath.ToSlash(path), "/")
			if err := writer.WriteHeader(header); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(writer, f)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeZipArchive(output string, dirs []string, excludes []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	for _, root := range dirs {
		root = filepath.Clean(root)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			for _, exclude := range excludes {
				if exclude != "" && strings.Contains(path, exclude) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if info.IsDir() {
				return nil
			}
			entry, err := writer.Create(strings.TrimPrefix(filepath.ToSlash(path), "/"))
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(entry, f)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func emitProgress(progressFn ProgressCallback, phase string, progress *BackupProgress) {
	if progressFn != nil {
		progressFn(phase, progress)
	}
}
