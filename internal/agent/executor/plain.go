package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PlainRunner implements backup/restore using plain rclone copy/sync
// (no restic, no encryption). It is used when the user does not provide
// a restic password, producing plain files on the remote storage.
type PlainRunner struct {
	RcloneConfPath  string
	RepoPath        string
	CacheDir        string
	RcloneExtraArgs map[string]string
}

type plainBackupMetadata struct {
	Timestamp string   `json:"timestamp"`
	Dirs      []string `json:"dirs"`
}

func (r PlainRunner) remoteArg(subPath string) string {
	if subPath == "" {
		return "vaultfleet:" + r.RepoPath
	}
	return "vaultfleet:" + filepath.Join(r.RepoPath, subPath)
}

func (r PlainRunner) baseEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "RCLONE_CONFIG=") {
			env = append(env, entry)
		}
	}
	return append(env, "RCLONE_CONFIG="+r.RcloneConfPath)
}

func (r PlainRunner) rcloneBaseArgs() []string {
	args := []string{"--config", r.RcloneConfPath}
	for _, arg := range r.normalizedRcloneExtraArgs() {
		args = append(args, arg)
	}
	return args
}

func (r PlainRunner) normalizedRcloneExtraArgs() []string {
	if len(r.RcloneExtraArgs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(r.RcloneExtraArgs))
	values := make(map[string]string, len(r.RcloneExtraArgs))
	for key, value := range r.RcloneExtraArgs {
		normalized, ok := normalizeRcloneExtraArgValue(key, value)
		if isAllowedRcloneExtraArg(key) && ok {
			keys = append(keys, key)
			values[key] = normalized
		}
	}
	sort.Strings(keys)

	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		if isRcloneBooleanExtraArg(key) {
			args = append(args, "--"+key)
			continue
		}
		args = append(args, "--"+key, values[key])
	}
	return args
}

func (r PlainRunner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Env = r.baseEnv()
	return cmd
}

// InitRepo pre-creates the remote directory for plain backup.
func (r PlainRunner) InitRepo(ctx context.Context) error {
	args := r.rcloneBaseArgs()
	args = append(args, "mkdir", r.remoteArg(""))
	cmd := r.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("create plain backup directory", stderr.String(), err)
	}
	return nil
}

// RunBackup copies each backup directory to the remote using rclone copy.
// A metadata marker file (.vaultfleet-backup-meta) is written with the
// current timestamp so that ListSnapshots can report a synthetic snapshot.
func (r PlainRunner) RunBackup(ctx context.Context, dirs []string, excludes []string) (string, error) {
	for _, dir := range dirs {
		if err := r.syncDir(ctx, dir, excludes); err != nil {
			return "", err
		}
	}
	return r.writeBackupMarker(ctx, dirs)
}

// RunBackupWithProgress is the same as RunBackup but reports progress
// via a callback. Since rclone copy does not provide structured JSON
// progress like restic, this implementation calls progressFn at the
// start and end of each directory.
func (r PlainRunner) RunBackupWithProgress(ctx context.Context, dirs []string, excludes []string, progressFn func(BackupProgress)) (string, error) {
	total := len(dirs)
	for i, dir := range dirs {
		if progressFn != nil {
			progressFn(BackupProgress{
				PercentDone: float64(i) / float64(total),
				TotalFiles:  int64(total),
				FilesDone:   int64(i),
				CurrentFile: dir,
			})
		}
		if err := r.syncDir(ctx, dir, excludes); err != nil {
			return "", err
		}
	}
	if progressFn != nil {
		progressFn(BackupProgress{
			PercentDone: 1.0,
			TotalFiles:  int64(total),
			FilesDone:   int64(total),
		})
	}
	return r.writeBackupMarker(ctx, dirs)
}

func (r PlainRunner) syncDir(ctx context.Context, dir string, excludes []string) error {
	cleaned := filepath.Clean(dir)
	remotePath := "data/" + strings.TrimLeft(filepath.ToSlash(cleaned), "/")
	args := r.rcloneBaseArgs()
	operation := "sync"
	if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
		operation = "copyto"
	}
	args = append(args, operation, cleaned, r.remoteArg(remotePath))
	if operation == "sync" {
		for _, exclude := range excludes {
			args = append(args, "--exclude", exclude)
		}
	}
	cmd := r.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("rclone "+operation+" "+dir, stderr.String(), err)
	}
	return nil
}

func (r PlainRunner) CopyFileToRemote(ctx context.Context, localPath string, remoteSubPath string) error {
	args := r.rcloneBaseArgs()
	args = append(args, "copyto", localPath, r.remoteArg(remoteSubPath))
	cmd := r.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("rclone copyto "+localPath, stderr.String(), err)
	}
	return nil
}

func (r PlainRunner) CopyFileToRemoteWithTaskLog(ctx context.Context, localPath string, remoteSubPath string, logFn TaskLogCallback) error {
	args := r.rcloneBaseArgs()
	// rclone's default NOTICE log level can suppress periodic stats when stderr
	// is a pipe (as it is under systemd). Keep stats at NOTICE so long uploads
	// remain observable without requiring a terminal.
	args = append(args, "--stats", "2s", "--stats-one-line", "--stats-log-level", "NOTICE", "copyto", localPath, r.remoteArg(remoteSubPath))
	cmd := r.command(ctx, args...)
	return runCommandWithTaskLog(cmd, "rclone copyto "+localPath, "archive-upload", logFn)
}

func (r PlainRunner) CopyFileFromRemote(ctx context.Context, remoteSubPath string, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	args := r.rcloneBaseArgs()
	args = append(args, "copyto", r.remoteArg(remoteSubPath), localPath)
	cmd := r.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("rclone copyto "+remoteSubPath, stderr.String(), err)
	}
	return nil
}

func (r PlainRunner) writeBackupMarker(ctx context.Context, dirs []string) (string, error) {
	marker := fmt.Sprintf(`{"timestamp":"%s","dirs":%s}`,
		time.Now().UTC().Format(time.RFC3339),
		quoteJSONStrings(dirs))

	args := r.rcloneBaseArgs()
	args = append(args, "rcat", r.remoteArg(".vaultfleet-backup-meta"))
	cmd := r.command(ctx, args...)
	cmd.Stdin = strings.NewReader(marker)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError("write backup marker", stderr.String(), err)
	}

	// Generate a deterministic snapshot ID from the timestamp.
	h := sha256.Sum256([]byte(marker))
	return hex.EncodeToString(h[:8]), nil
}

// RunForget is a no-op for plain backups (no snapshot retention).
func (r PlainRunner) RunForget(_ context.Context, _ RetentionPolicy) error {
	return nil
}

// ListSnapshots returns a single synthetic snapshot representing the
// current state of the plain backup.
func (r PlainRunner) ListSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	args := r.rcloneBaseArgs()
	args = append(args, "cat", r.remoteArg(".vaultfleet-backup-meta"))
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If marker doesn't exist, return empty list (no backups yet).
		return nil, nil
	}

	var meta plainBackupMetadata
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("parse plain backup metadata: %w", err)
	}

	t, _ := time.Parse(time.RFC3339, meta.Timestamp)
	if t.IsZero() {
		t = time.Now().UTC()
	}

	h := sha256.Sum256([]byte(strings.TrimSpace(stdout.String())))
	snapID := hex.EncodeToString(h[:8])

	return []SnapshotInfo{{
		ID:       snapID,
		Time:     t,
		Paths:    meta.Dirs,
		Hostname: "",
	}}, nil
}

// RepositorySize returns the total size of the plain backup via rclone size.
func (r PlainRunner) RepositorySize(ctx context.Context) (int64, error) {
	args := r.rcloneBaseArgs()
	args = append(args, "size", r.remoteArg("data"), "--json")
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, commandError("rclone size", stderr.String(), err)
	}

	var sizeInfo struct {
		TotalBytes int64 `json:"bytes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &sizeInfo); err != nil {
		return 0, fmt.Errorf("parse rclone size JSON: %w", err)
	}
	return sizeInfo.TotalBytes, nil
}

// RestoreSnapshot copies all backed-up directories from the remote
// to the target path.
func (r PlainRunner) RestoreSnapshot(ctx context.Context, snapshotID, targetPath string, includePaths []string) error {
	meta, err := r.readBackupMetadata(ctx)
	if err != nil {
		return err
	}
	if len(meta.Dirs) == 0 {
		return errors.New("plain backup metadata contains no source directories")
	}

	restored := 0
	for _, sourceDir := range meta.Dirs {
		sourceDir = filepath.Clean(strings.TrimSpace(sourceDir))
		if sourceDir == "." || sourceDir == string(filepath.Separator) {
			continue
		}
		if len(includePaths) > 0 && !r.matchesIncludePaths(sourceDir, includePaths) {
			continue
		}

		dirName := filepath.Base(sourceDir)
		remotePath := "data/" + strings.TrimLeft(filepath.ToSlash(sourceDir), "/")
		srcRemote := r.remoteArg(remotePath)
		destPath := filepath.Join(targetPath, dirName)
		if filepath.Clean(targetPath) == string(filepath.Separator) && filepath.IsAbs(sourceDir) {
			destPath = sourceDir
		}

		isDir, err := r.remotePathIsDir(ctx, srcRemote)
		if err != nil {
			// Backups created before path-preserving storage used only the basename.
			srcRemote = r.remoteArg("data/" + dirName)
			isDir, err = r.remotePathIsDir(ctx, srcRemote)
			if err != nil {
				return err
			}
		}
		copyArgs := r.rcloneBaseArgs()
		operation := "copy"
		if !isDir {
			operation = "copyto"
		}
		copyArgs = append(copyArgs, operation, srcRemote, destPath)
		cpCmd := r.command(ctx, copyArgs...)
		var cpStderr bytes.Buffer
		cpCmd.Stderr = &cpStderr
		if err := cpCmd.Run(); err != nil {
			return commandError("rclone restore copy", cpStderr.String(), err)
		}
		restored++
	}
	if len(includePaths) > 0 && restored == 0 {
		return fmt.Errorf("none of the requested restore paths exist in plain backup metadata: %s", strings.Join(includePaths, ", "))
	}
	return nil
}

func (r PlainRunner) remotePathIsDir(ctx context.Context, remote string) (bool, error) {
	args := r.rcloneBaseArgs()
	args = append(args, "lsjson", remote, "--stat")
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, commandError("stat plain backup path", stderr.String(), err)
	}
	var info struct {
		IsDir bool `json:"IsDir"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return false, fmt.Errorf("parse plain backup path metadata: %w", err)
	}
	return info.IsDir, nil
}

func (r PlainRunner) readBackupMetadata(ctx context.Context) (plainBackupMetadata, error) {
	args := r.rcloneBaseArgs()
	args = append(args, "cat", r.remoteArg(".vaultfleet-backup-meta"))
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return plainBackupMetadata{}, commandError("read plain backup metadata", stderr.String(), err)
	}
	var meta plainBackupMetadata
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return plainBackupMetadata{}, fmt.Errorf("parse plain backup metadata: %w", err)
	}
	return meta, nil
}

func (r PlainRunner) matchesIncludePaths(sourceDir string, includePaths []string) bool {
	dirName := filepath.Base(sourceDir)
	cleanSource := filepath.Clean(sourceDir)
	for _, p := range includePaths {
		cleanPath := filepath.Clean(p)
		base := filepath.Base(cleanPath)
		if !filepath.IsAbs(cleanPath) && base == dirName {
			return true
		}
		if cleanPath == cleanSource || strings.HasPrefix(cleanPath, cleanSource+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func runCommandWithTaskLog(cmd *exec.Cmd, operation string, phase string, logFn TaskLogCallback) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("prepare %s stdout: %w", operation, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("prepare %s stderr: %w", operation, err)
	}

	var outputMu sync.Mutex
	var stderrText bytes.Buffer
	var stdoutText bytes.Buffer
	appendOutput := func(buffer *bytes.Buffer, text string) {
		outputMu.Lock()
		buffer.WriteString(text)
		buffer.WriteByte('\n')
		outputMu.Unlock()
	}

	if err := cmd.Start(); err != nil {
		return commandError(operation, "", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanCommandProgressLines(stdout, func(line string) {
			appendOutput(&stdoutText, line)
			emitTaskLog(logFn, "info", phase, "stdout", line)
		})
	}()
	go func() {
		defer wg.Done()
		scanCommandProgressLines(stderr, func(line string) {
			appendOutput(&stderrText, line)
			emitTaskLog(logFn, "info", phase, "stderr", line)
		})
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	if waitErr != nil {
		outputMu.Lock()
		details := strings.TrimSpace(stderrText.String())
		if details == "" {
			details = strings.TrimSpace(stdoutText.String())
		}
		outputMu.Unlock()
		return commandError(operation, details, waitErr)
	}
	return nil
}

func scanCommandProgressLines(reader io.Reader, emit func(string)) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(splitCommandProgressLines)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			emit(line)
		}
	}
}

func splitCommandProgressLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// LsSnapshot lists files in the plain backup. Since plain mode has only
// one "snapshot", the snapshotID parameter is ignored.
func (r PlainRunner) LsSnapshot(ctx context.Context, snapshotID string, paths ...string) ([]SnapshotFileEntry, error) {
	subPath := ""
	if len(paths) > 0 {
		subPath = paths[0]
	}

	targetRemote := r.remoteArg("data/" + subPath)
	args := r.rcloneBaseArgs()
	args = append(args, "lsjson", targetRemote, "-R")
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// If path doesn't exist, try the top-level data directory.
		if subPath != "" {
			targetRemote = r.remoteArg("data")
			args = r.rcloneBaseArgs()
			args = append(args, "lsjson", targetRemote, "-R")
			cmd = r.command(ctx, args...)
			stdout.Reset()
			stderr.Reset()
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return nil, commandError("list plain backup files", stderr.String(), err)
			}
		} else {
			return nil, commandError("list plain backup files", stderr.String(), err)
		}
	}

	var items []struct {
		Path    string `json:"Path"`
		Size    int64  `json:"Size"`
		ModTime string `json:"ModTime"`
		IsDir   bool   `json:"IsDir"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		return nil, fmt.Errorf("parse rclone lsjson: %w", err)
	}

	entries := make([]SnapshotFileEntry, 0, len(items))
	for _, item := range items {
		entryType := "file"
		if item.IsDir {
			entryType = "dir"
		}
		entries = append(entries, SnapshotFileEntry{
			Path:  item.Path,
			Type:  entryType,
			Size:  item.Size,
			Mtime: item.ModTime,
		})
	}
	return entries, nil
}

func quoteJSONStrings(strs []string) string {
	quoted := make([]string, len(strs))
	for i, s := range strs {
		quoted[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
