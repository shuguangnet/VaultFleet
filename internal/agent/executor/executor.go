package executor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vaultfleet/internal/artifactnaming"
	"vaultfleet/pkg/protocol"
)

type TaskResult struct {
	Type                string                             `json:"type"`
	Status              string                             `json:"status"`
	DurationMs          int64                              `json:"duration_ms"`
	SnapshotID          string                             `json:"snapshot_id,omitempty"`
	PolicyName          string                             `json:"policy_name,omitempty"`
	BackupMode          string                             `json:"backup_mode,omitempty"`
	ArchiveFormat       string                             `json:"archive_format,omitempty"`
	ArtifactPath        string                             `json:"artifact_path,omitempty"`
	ArtifactName        string                             `json:"artifact_name,omitempty"`
	ArtifactSize        int64                              `json:"artifact_size,omitempty"`
	ArtifactContentType string                             `json:"artifact_content_type,omitempty"`
	RepoSize            int64                              `json:"repo_size,omitempty"`
	Snapshots           []SnapshotInfo                     `json:"snapshots,omitempty"`
	ErrorLog            string                             `json:"error_log,omitempty"`
	Docker              *protocol.DockerBackupMetadata     `json:"docker,omitempty"`
	Database            *protocol.DatabaseBackupMetadata   `json:"database,omitempty"`
	Verification        *protocol.BackupVerificationResult `json:"verification,omitempty"`
	Manifest            *protocol.BackupContentManifest    `json:"manifest,omitempty"`
	ManifestWarnings    []protocol.ManifestWarning         `json:"manifest_warnings,omitempty"`
	ArtifactNaming      *protocol.ArtifactNamingMetadata   `json:"artifact_naming,omitempty"`
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
		PolicyName:          r.PolicyName,
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
		Docker:              r.Docker,
		Database:            r.Database,
		Verification:        r.Verification,
		Manifest:            r.Manifest,
		ArtifactNaming:      r.ArtifactNaming,
	}
}

type ArchiveExtraFile struct {
	Name string
	Data []byte
}

type archiveFile struct {
	Path string
	Name string
	Info os.FileInfo
}

type archiveProgressReporter struct {
	mu          sync.Mutex
	totalBytes  int64
	bytesDone   int64
	totalFiles  int64
	filesDone   int64
	currentFile string
	lastEmit    time.Time
	progressFn  func(BackupProgress)
}

var openArchiveSourceFile = func(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

type ExecutorConfig struct {
	ConfigDir                string
	RepoPath                 string
	BackupDirs               []string
	Excludes                 []string
	Retention                RetentionPolicy
	RcloneArgs               map[string]string
	PlainBackup              bool
	BackupMode               string
	ArchiveFormat            string
	ArtifactContextName      string
	ArchiveRemoteDirTemplate string
	ArchiveNameTemplate      string
	ArtifactNamingContext    ArtifactNamingContext
	ExtraArchiveFiles        []ArchiveExtraFile
	PreBackupHook            *protocol.PolicyHook
	PostBackupHook           *protocol.PolicyHook
	TaskLog                  TaskLogCallback
}

type ArtifactNamingContext struct {
	AgentID       string
	AgentName     string
	PolicyID      string
	PolicyName    string
	BackupSources []protocol.BackupSource
	Docker        *protocol.DockerBackupMetadata
	Database      *protocol.DatabaseBackupMetadata
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
type TaskLogCallback func(level string, phase string, stream string, line string)

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
	taskLog    TaskLogCallback
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
		taskLog:    cfg.TaskLog,
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
	emitTaskLog(e.taskLog, "info", "init", "system", "checking repository")
	if err := e.restic.InitRepo(ctx); err != nil {
		emitTaskLog(e.taskLog, "error", "init", "stderr", err.Error())
		result.ErrorLog = "init: " + err.Error()
		return result
	}

	emitProgress(progressFn, "backup", nil)
	emitTaskLog(e.taskLog, "info", "backup", "system", "running backup")
	if progressRunner, ok := e.restic.(resticExecutorWithProgress); ok {
		var resticProgressFn func(BackupProgress)
		if progressFn != nil {
			resticProgressFn = func(progress BackupProgress) {
				progressCopy := progress
				emitProgress(progressFn, "backup", &progressCopy)
			}
		}
		if _, err := progressRunner.RunBackupWithProgress(ctx, e.backupDirs, e.excludes, resticProgressFn); err != nil {
			emitTaskLog(e.taskLog, "error", "backup", "stderr", err.Error())
			result.ErrorLog = "backup: " + err.Error()
			return result
		}
	} else if _, err := e.restic.RunBackup(ctx, e.backupDirs, e.excludes); err != nil {
		emitTaskLog(e.taskLog, "error", "backup", "stderr", err.Error())
		result.ErrorLog = "backup: " + err.Error()
		return result
	}

	emitProgress(progressFn, "forget", nil)
	emitTaskLog(e.taskLog, "info", "forget", "system", "applying retention policy")
	if err := e.restic.RunForget(ctx, e.retention); err != nil {
		emitTaskLog(e.taskLog, "error", "forget", "stderr", err.Error())
		result.ErrorLog = "forget: " + err.Error()
		return result
	}

	emitProgress(progressFn, "stats", nil)
	emitTaskLog(e.taskLog, "info", "stats", "system", "listing snapshots")
	snapshots, err := e.restic.ListSnapshots(ctx)
	if err != nil {
		emitTaskLog(e.taskLog, "error", "stats", "stderr", err.Error())
		result.ErrorLog = "snapshots: " + err.Error()
		return result
	}
	emitTaskLog(e.taskLog, "info", "stats", "system", "reading repository statistics")
	repoSize, err := e.restic.RepositorySize(ctx)
	if err != nil {
		emitTaskLog(e.taskLog, "error", "stats", "stderr", err.Error())
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
	return RunArchiveJobWithProgress(ctx, cfg, nil)
}

func RunArchiveJobWithProgress(ctx context.Context, cfg ExecutorConfig, progressFn ProgressCallback) (result TaskResult) {
	start := time.Now()
	result = TaskResult{
		Type:          "backup",
		Status:        "failed",
		BackupMode:    protocol.BackupModeArchive,
		ArchiveFormat: normalizeArchiveFormat(cfg.ArchiveFormat),
	}
	defer func() { result.DurationMs = time.Since(start).Milliseconds() }()
	naming, err := artifactnaming.Render(artifactnaming.RenderInput{
		Context: artifactnaming.Context{
			AgentID:       cfg.ArtifactNamingContext.AgentID,
			AgentName:     cfg.ArtifactNamingContext.AgentName,
			PolicyID:      cfg.ArtifactNamingContext.PolicyID,
			PolicyName:    cfg.ArtifactNamingContext.PolicyName,
			ContextName:   cfg.ArtifactContextName,
			ArchiveFormat: result.ArchiveFormat,
			Now:           start.UTC(),
			Sources:       cfg.ArtifactNamingContext.BackupSources,
			Docker:        cfg.ArtifactNamingContext.Docker,
			Database:      cfg.ArtifactNamingContext.Database,
		},
		RemoteDirTemplate: cfg.ArchiveRemoteDirTemplate,
		NameTemplate:      cfg.ArchiveNameTemplate,
	})
	if err != nil {
		emitTaskLog(cfg.TaskLog, "error", "archive", "stderr", err.Error())
		result.ErrorLog = "archive naming: " + err.Error()
		return result
	}
	result.ArtifactNaming = &naming

	artifactDir := filepath.Join(cfg.ConfigDir, "artifacts")
	emitTaskLog(cfg.TaskLog, "info", "archive", "system", "preparing archive directory")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		emitTaskLog(cfg.TaskLog, "error", "archive", "stderr", err.Error())
		result.ErrorLog = "archive: " + err.Error()
		return result
	}

	artifactName := naming.ArtifactName
	artifactPath := filepath.Join(artifactDir, artifactName)
	emitProgress(progressFn, "archive", nil)
	emitTaskLog(cfg.TaskLog, "info", "archive", "system", "writing local archive "+artifactName)
	warnings, err := writeArchive(ctx, artifactPath, result.ArchiveFormat, cfg.BackupDirs, cfg.Excludes, cfg.ExtraArchiveFiles, func(progress BackupProgress) {
		emitProgress(progressFn, "archive", &progress)
	})
	if err != nil {
		emitTaskLog(cfg.TaskLog, "error", "archive", "stderr", err.Error())
		result.ErrorLog = "archive: " + err.Error()
		return result
	}
	for _, warning := range warnings {
		emitTaskLog(cfg.TaskLog, "warn", "archive", "stderr", warning.Message)
	}
	remoteArtifactPath := naming.ArtifactPath
	runner := PlainRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	emitProgress(progressFn, "archive-upload", nil)
	emitTaskLog(cfg.TaskLog, "info", "archive-upload", "system", "uploading archive")
	if info, err := os.Stat(artifactPath); err == nil {
		emitProgress(progressFn, "archive-upload", &BackupProgress{
			PercentDone: 0,
			TotalFiles:  1,
			TotalBytes:  info.Size(),
			CurrentFile: artifactName,
		})
	}
	if err := runner.CopyFileToRemoteWithTaskLog(ctx, artifactPath, remoteArtifactPath, cfg.TaskLog); err != nil {
		emitTaskLog(cfg.TaskLog, "error", "archive-upload", "stderr", err.Error())
		result.ErrorLog = "archive-upload: " + err.Error()
		return result
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		emitTaskLog(cfg.TaskLog, "error", "archive-stat", "stderr", err.Error())
		result.ErrorLog = "archive-stat: " + err.Error()
		return result
	}
	emitProgress(progressFn, "archive-upload", &BackupProgress{
		PercentDone: 1,
		TotalFiles:  1,
		FilesDone:   1,
		TotalBytes:  info.Size(),
		BytesDone:   info.Size(),
		CurrentFile: artifactName,
	})

	result.Status = "success"
	result.ArtifactPath = remoteArtifactPath
	result.ArtifactName = artifactName
	result.ArtifactSize = info.Size()
	result.ArtifactContentType = archiveContentType(result.ArchiveFormat)
	result.ManifestWarnings = warnings
	result.RepoSize = info.Size()
	emitTaskLog(cfg.TaskLog, "info", "complete", "system", "archive backup completed")
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

func writeArchive(ctx context.Context, output string, format string, dirs []string, excludes []string, extraFiles []ArchiveExtraFile, progressFn func(BackupProgress)) ([]protocol.ManifestWarning, error) {
	files, warnings, err := planArchiveFiles(ctx, dirs, excludes)
	if err != nil {
		return nil, err
	}
	extraFiles = appendManifestWarnings(extraFiles, warnings)
	reporter := newArchiveProgressReporter(files, extraFiles, progressFn)
	if normalizeArchiveFormat(format) == protocol.ArchiveFormatZip {
		writeWarnings, err := writeZipArchive(ctx, output, files, extraFiles, reporter)
		return append(warnings, writeWarnings...), err
	}
	writeWarnings, err := writeTarGzArchive(ctx, output, files, extraFiles, reporter)
	return append(warnings, writeWarnings...), err
}

func planArchiveFiles(ctx context.Context, dirs []string, excludes []string) ([]archiveFile, []protocol.ManifestWarning, error) {
	files := make([]archiveFile, 0)
	warnings := make([]protocol.ManifestWarning, 0)
	for _, root := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		root = filepath.Clean(root)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					warnings = append(warnings, skippedArchiveFileWarning(path, walkErr))
					return nil
				}
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
			if info.Mode().IsRegular() {
				if err := verifyArchiveSourceReadable(ctx, path); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return err
					}
					warnings = append(warnings, protocol.ManifestWarning{
						Code:    "archive_file_skipped",
						Message: "skipped unreadable file: " + err.Error(),
						Source:  path,
					})
					return nil
				}
			}
			files = append(files, archiveFile{
				Path: path,
				Name: strings.TrimPrefix(filepath.ToSlash(path), "/"),
				Info: info,
			})
			return nil
		}); err != nil {
			return nil, nil, err
		}
	}
	return files, warnings, nil
}

func skippedArchiveFileWarning(path string, err error) protocol.ManifestWarning {
	return protocol.ManifestWarning{
		Code:    "archive_file_skipped",
		Message: "skipped file that disappeared during archive: " + err.Error(),
		Source:  path,
	}
}

func verifyArchiveSourceReadable(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := openArchiveSourceFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(io.Discard, newContextReadCloser(ctx, file)); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func appendManifestWarnings(extraFiles []ArchiveExtraFile, warnings []protocol.ManifestWarning) []ArchiveExtraFile {
	if len(warnings) == 0 {
		return extraFiles
	}
	result := append([]ArchiveExtraFile(nil), extraFiles...)
	for i := range result {
		if result[i].Name != protocol.BackupContentManifestName {
			continue
		}
		var manifest protocol.BackupContentManifest
		if err := json.Unmarshal(result[i].Data, &manifest); err != nil {
			continue
		}
		manifest.Warnings = append(manifest.Warnings, warnings...)
		raw, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			continue
		}
		result[i].Data = append(raw, '\n')
	}
	return result
}

func writeTarGzArchive(ctx context.Context, output string, files []archiveFile, extraFiles []ArchiveExtraFile, reporter *archiveProgressReporter) ([]protocol.ManifestWarning, error) {
	warnings := make([]protocol.ManifestWarning, 0)
	file, err := os.Create(output)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()

	for _, extra := range extraFiles {
		name, ok := safeArchiveRootName(extra.Name)
		if !ok {
			continue
		}
		header := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(extra.Data)),
			ModTime: time.Now().UTC(),
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := writer.Write(extra.Data); err != nil {
			return nil, err
		}
		reporter.AddBytes(int64(len(extra.Data)), name)
	}

	for _, archiveFile := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var source io.ReadCloser
		if archiveFile.Info.Mode().IsRegular() {
			source, err = openArchiveSourceFile(archiveFile.Path)
			if err != nil {
				if os.IsNotExist(err) {
					warnings = append(warnings, skippedArchiveFileWarning(archiveFile.Path, err))
					continue
				}
				return nil, err
			}
		}
		header, err := tar.FileInfoHeader(archiveFile.Info, "")
		if err != nil {
			if source != nil {
				_ = source.Close()
			}
			return nil, err
		}
		header.Name = archiveFile.Name
		if err := writer.WriteHeader(header); err != nil {
			if source != nil {
				_ = source.Close()
			}
			return nil, err
		}
		if !archiveFile.Info.Mode().IsRegular() {
			continue
		}
		reporter.StartFile(archiveFile.Path)
		if _, err := copyArchiveSource(ctx, writer, source, archiveFile.Info.Size(), archiveFile.Path, reporter); err != nil {
			_ = source.Close()
			return nil, err
		}
		if err := source.Close(); err != nil {
			return nil, err
		}
		reporter.FinishFile(archiveFile.Path)
	}
	reporter.Finish()
	return warnings, nil
}

func writeZipArchive(ctx context.Context, output string, files []archiveFile, extraFiles []ArchiveExtraFile, reporter *archiveProgressReporter) ([]protocol.ManifestWarning, error) {
	warnings := make([]protocol.ManifestWarning, 0)
	file, err := os.Create(output)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	for _, extra := range extraFiles {
		name, ok := safeArchiveRootName(extra.Name)
		if !ok {
			continue
		}
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(extra.Data); err != nil {
			return nil, err
		}
		reporter.AddBytes(int64(len(extra.Data)), name)
	}

	for _, archiveFile := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !archiveFile.Info.Mode().IsRegular() {
			continue
		}
		f, err := openArchiveSourceFile(archiveFile.Path)
		if err != nil {
			if os.IsNotExist(err) {
				warnings = append(warnings, skippedArchiveFileWarning(archiveFile.Path, err))
				continue
			}
			return nil, err
		}
		entry, err := writer.Create(archiveFile.Name)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		reporter.StartFile(archiveFile.Path)
		if _, err := copyArchiveSource(ctx, entry, f, archiveFile.Info.Size(), archiveFile.Path, reporter); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		reporter.FinishFile(archiveFile.Path)
	}
	reporter.Finish()
	return warnings, nil
}

func copyArchiveSource(ctx context.Context, writer io.Writer, source io.ReadCloser, size int64, path string, reporter *archiveProgressReporter) (int64, error) {
	if size < 0 {
		size = 0
	}
	reader := io.LimitReader(newContextReadCloser(ctx, source), size)
	written, err := io.Copy(writer, reporter.Reader(reader, path))
	if err != nil {
		return written, err
	}
	if written >= size {
		return written, nil
	}
	padded, err := copyZeroPadding(writer, size-written)
	return written + padded, err
}

func copyZeroPadding(writer io.Writer, size int64) (int64, error) {
	var written int64
	var zeros [32 * 1024]byte
	for written < size {
		chunk := size - written
		if chunk > int64(len(zeros)) {
			chunk = int64(len(zeros))
		}
		n, err := writer.Write(zeros[:chunk])
		written += int64(n)
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func newArchiveProgressReporter(files []archiveFile, extraFiles []ArchiveExtraFile, progressFn func(BackupProgress)) *archiveProgressReporter {
	reporter := &archiveProgressReporter{progressFn: progressFn}
	for _, file := range files {
		if !file.Info.Mode().IsRegular() {
			continue
		}
		reporter.totalFiles++
		reporter.totalBytes += file.Info.Size()
	}
	for _, extra := range extraFiles {
		if _, ok := safeArchiveRootName(extra.Name); ok {
			reporter.totalBytes += int64(len(extra.Data))
		}
	}
	return reporter
}

func (r *archiveProgressReporter) Reader(reader io.Reader, currentFile string) io.Reader {
	if r == nil {
		return reader
	}
	return &archiveCountingReader{
		reader: reader,
		onRead: func(n int) {
			r.AddBytes(int64(n), currentFile)
		},
	}
}

func (r *archiveProgressReporter) StartFile(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.currentFile = path
	r.emitLocked(false)
	r.mu.Unlock()
}

func (r *archiveProgressReporter) AddBytes(n int64, currentFile string) {
	if r == nil || n <= 0 {
		return
	}
	r.mu.Lock()
	r.bytesDone += n
	if currentFile != "" {
		r.currentFile = currentFile
	}
	r.emitLocked(false)
	r.mu.Unlock()
}

func (r *archiveProgressReporter) FinishFile(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.filesDone++
	if path != "" {
		r.currentFile = path
	}
	r.emitLocked(false)
	r.mu.Unlock()
}

func (r *archiveProgressReporter) Finish() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.totalBytes > 0 {
		r.bytesDone = r.totalBytes
	}
	if r.totalFiles > 0 {
		r.filesDone = r.totalFiles
	}
	r.emitLocked(true)
	r.mu.Unlock()
}

func (r *archiveProgressReporter) emitLocked(force bool) {
	if r.progressFn == nil {
		return
	}
	now := time.Now()
	if !force && !r.lastEmit.IsZero() && now.Sub(r.lastEmit) < 2*time.Second {
		return
	}
	r.lastEmit = now
	progress := BackupProgress{
		TotalFiles:  r.totalFiles,
		FilesDone:   r.filesDone,
		TotalBytes:  r.totalBytes,
		BytesDone:   r.bytesDone,
		CurrentFile: r.currentFile,
	}
	if r.totalBytes > 0 {
		progress.PercentDone = float64(r.bytesDone) / float64(r.totalBytes)
	}
	r.progressFn(progress)
}

type archiveCountingReader struct {
	reader io.Reader
	onRead func(int)
}

func (r *archiveCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.onRead != nil {
		r.onRead(n)
	}
	return n, err
}

type contextReadCloser struct {
	ctx    context.Context
	reader io.ReadCloser
	once   sync.Once
	done   chan struct{}
}

func newContextReadCloser(ctx context.Context, reader io.ReadCloser) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	wrapped := &contextReadCloser{
		ctx:    ctx,
		reader: reader,
		done:   make(chan struct{}),
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = wrapped.reader.Close()
		case <-wrapped.done:
		}
	}()
	return wrapped
}

func (r *contextReadCloser) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err != nil && r.ctx.Err() != nil {
		return n, r.ctx.Err()
	}
	return n, err
}

func (r *contextReadCloser) Close() error {
	var err error
	r.once.Do(func() {
		close(r.done)
		err = r.reader.Close()
	})
	return err
}

func safeArchiveRootName(name string) (string, bool) {
	name = strings.TrimSpace(filepath.ToSlash(name))
	if name == "" || name == "." || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", false
	}
	return name, true
}

func emitProgress(progressFn ProgressCallback, phase string, progress *BackupProgress) {
	if progressFn != nil {
		progressFn(phase, progress)
	}
}

func emitTaskLog(logFn TaskLogCallback, level string, phase string, stream string, line string) {
	if logFn != nil {
		logFn(level, phase, stream, line)
	}
}
