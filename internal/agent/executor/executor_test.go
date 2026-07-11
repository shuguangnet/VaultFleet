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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vaultfleet/pkg/protocol"
)

func TestNewExecutorBuildsRunnerAndCopiesConfig(t *testing.T) {
	cfgDir := t.TempDir()
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, ".restic-password"), []byte("secret"), 0o600)

	cfg := ExecutorConfig{
		ConfigDir:  cfgDir,
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

func TestNewExecutorPassesRcloneArgsToRunner(t *testing.T) {
	cfgDir := t.TempDir()
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, ".restic-password"), []byte("secret"), 0o600)

	cfg := ExecutorConfig{
		ConfigDir: cfgDir,
		RepoPath:  "repo/agent-1",
		RcloneArgs: map[string]string{
			"transfers": "2",
			"tpslimit":  "4",
		},
	}

	executor := NewExecutor(cfg)

	runner, ok := executor.restic.(ResticRunner)
	if !ok {
		t.Fatalf("NewExecutor() restic runner type = %T, want ResticRunner", executor.restic)
	}
	if runner.RcloneExtraArgs["transfers"] != "2" {
		t.Fatalf("RcloneExtraArgs[transfers] = %q, want 2", runner.RcloneExtraArgs["transfers"])
	}
	if runner.RcloneExtraArgs["tpslimit"] != "4" {
		t.Fatalf("RcloneExtraArgs[tpslimit] = %q, want 4", runner.RcloneExtraArgs["tpslimit"])
	}

	cfg.RcloneArgs["transfers"] = "99"
	if runner.RcloneExtraArgs["transfers"] != "2" {
		t.Fatalf("RcloneExtraArgs were not copied: %#v", runner.RcloneExtraArgs)
	}
}

func TestNewExecutorUsesPlainRunnerWhenNoPasswordFile(t *testing.T) {
	cfg := ExecutorConfig{
		ConfigDir:  t.TempDir(),
		RepoPath:   "repo/agent-1",
		BackupDirs: []string{"/data"},
	}

	executor := NewExecutor(cfg)

	if executor.restic == nil {
		t.Fatal("NewExecutor() runner is nil")
	}
	runner, ok := executor.restic.(PlainRunner)
	if !ok {
		t.Fatalf("NewExecutor() runner type = %T, want PlainRunner", executor.restic)
	}
	if runner.RepoPath != cfg.RepoPath {
		t.Fatalf("RepoPath = %q, want %q", runner.RepoPath, cfg.RepoPath)
	}
}

func TestPlainRunnerRcloneBaseArgsIncludesBooleanExtraArg(t *testing.T) {
	runner := PlainRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		RcloneExtraArgs: map[string]string{
			"local-no-check-updated": "true",
			"transfers":              "2",
			"timeout":                "10s",
		},
	}

	got := runner.rcloneBaseArgs()
	want := []string{
		"--config", "/tmp/rclone.conf",
		"--local-no-check-updated",
		"--timeout", "10s",
		"--transfers", "2",
	}
	assertStringSlicesEqual(t, got, want)
}

func assertStringSlicesEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q: got %#v", i, got[i], want[i], got)
		}
	}
}

func TestNewExecutorUsesPlainRunnerWhenPasswordFileIsEmpty(t *testing.T) {
	cfgDir := t.TempDir()
	os.WriteFile(filepath.Join(cfgDir, ".restic-password"), []byte("  \n  "), 0o600)

	cfg := ExecutorConfig{
		ConfigDir: cfgDir,
		RepoPath:  "repo/agent-1",
	}

	executor := NewExecutor(cfg)

	if _, ok := executor.restic.(PlainRunner); !ok {
		t.Fatalf("NewExecutor() runner type = %T, want PlainRunner (empty password)", executor.restic)
	}
}

func TestRunBackupJobSuccessReturnsLatestSnapshotAndSnapshots(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	runner := &recordingRunner{
		backupDelay: 10 * time.Millisecond,
		repoSize:    4096,
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
	if result.RepoSize != 4096 {
		t.Fatalf("RepoSize = %d, want 4096", result.RepoSize)
	}
	if len(result.Snapshots) != 2 {
		t.Fatalf("Snapshots length = %d, want 2", len(result.Snapshots))
	}
	if result.DurationMs <= 0 {
		t.Fatalf("DurationMs = %d, want positive duration", result.DurationMs)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup", "forget", "snapshots", "stats"})
}

func TestRunBackupJobFailsWhenRepositorySizeCannotBeRead(t *testing.T) {
	runner := &recordingRunner{
		snapshots: []SnapshotInfo{{ID: "snap-1", Time: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}},
		statsErr:  errors.New("repository locked"),
	}
	executor := &Executor{
		restic:     runner,
		backupDirs: []string{"/data"},
		retention:  RetentionPolicy{KeepLast: 1},
	}

	result := executor.RunBackupJob(context.Background())

	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.ErrorLog, "stats: repository locked") {
		t.Fatalf("ErrorLog = %q, want stats stage and error", result.ErrorLog)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup", "forget", "snapshots", "stats"})
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

func TestRunArchiveJobUploadsArtifactToRemote(t *testing.T) {
	configDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "hello.txt"), []byte("hello archive"), 0o644); err != nil {
		t.Fatalf("seed backup file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("[vaultfleet]\ntype = memory\n"), 0o600); err != nil {
		t.Fatalf("write rclone config: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "rclone.log")
	binDir := t.TempDir()
	rclonePath := filepath.Join(binDir, "rclone")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nexit 0\n", shellQuoteForSh(logPath))
	if err := os.WriteFile(rclonePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake rclone: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatZip,
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	if !strings.HasPrefix(result.ArtifactPath, "artifacts/") {
		t.Fatalf("ArtifactPath = %q, want artifacts/...", result.ArtifactPath)
	}
	if !strings.HasSuffix(result.ArtifactName, ".zip") {
		t.Fatalf("ArtifactName = %q, want .zip suffix", result.ArtifactName)
	}
	if result.ArtifactContentType != "application/zip" {
		t.Fatalf("ArtifactContentType = %q, want application/zip", result.ArtifactContentType)
	}
	if result.ArtifactSize <= 0 {
		t.Fatalf("ArtifactSize = %d, want positive", result.ArtifactSize)
	}
	localArtifact := filepath.Join(configDir, "artifacts", result.ArtifactName)
	if _, err := os.Stat(localArtifact); err != nil {
		t.Fatalf("local archive missing: %v", err)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake rclone log: %v", err)
	}
	logLine := string(logged)
	if !strings.Contains(logLine, "copyto ") {
		t.Fatalf("rclone log = %q, want copyto invocation", logLine)
	}
	if !strings.Contains(logLine, "--stats 2s --stats-one-line --stats-log-level NOTICE") {
		t.Fatalf("rclone log = %q, want visible periodic upload stats", logLine)
	}
	if !strings.Contains(logLine, localArtifact) {
		t.Fatalf("rclone log = %q, want local artifact path", logLine)
	}
	if !strings.Contains(logLine, "vaultfleet:tenant/agent-1/"+filepath.ToSlash(result.ArtifactPath)) {
		t.Fatalf("rclone log = %q, want remote artifact destination", logLine)
	}
}

func TestRunArchiveJobUsesArtifactNamingTemplates(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)

	logPath := filepath.Join(t.TempDir(), "rclone.log")
	binDir := t.TempDir()
	rclonePath := filepath.Join(binDir, "rclone")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nexit 0\n", shellQuoteForSh(logPath))
	if err := os.WriteFile(rclonePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake rclone: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:                configDir,
		RepoPath:                 "tenant/agent-1",
		BackupDirs:               []string{backupDir},
		ArchiveFormat:            protocol.ArchiveFormatZip,
		ArtifactContextName:      "site a",
		ArchiveRemoteDirTemplate: "archives/{{agent_name}}/{{context_name}}/{{date}}",
		ArchiveNameTemplate:      "{{context_name}}_{{datetime}}.{{ext}}",
		ArtifactNamingContext: ArtifactNamingContext{
			AgentName: "node hk",
		},
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	if result.ArtifactNaming == nil {
		t.Fatal("ArtifactNaming is nil")
	}
	if result.ArtifactNaming.ContextName != "site_a" {
		t.Fatalf("ContextName = %q, want site_a", result.ArtifactNaming.ContextName)
	}
	if !strings.HasPrefix(result.ArtifactPath, "archives/node_hk/site_a/") {
		t.Fatalf("ArtifactPath = %q, want named archive path", result.ArtifactPath)
	}
	if !strings.HasSuffix(result.ArtifactName, ".zip") {
		t.Fatalf("ArtifactName = %q, want zip", result.ArtifactName)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake rclone log: %v", err)
	}
	if !strings.Contains(string(logged), result.ArtifactPath) {
		t.Fatalf("rclone log = %q, want remote artifact path %q", string(logged), result.ArtifactPath)
	}
}

func TestRunArchiveJobWritesManifestAtTarGzRoot(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	setupFakeRclone(t)
	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatTarGz,
		ExtraArchiveFiles: []ArchiveExtraFile{{
			Name: protocol.BackupContentManifestName,
			Data: []byte(`{"version":1}`),
		}},
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	content := readTarGzEntry(t, filepath.Join(configDir, "artifacts", result.ArtifactName), protocol.BackupContentManifestName)
	if string(content) != `{"version":1}` {
		t.Fatalf("manifest content = %q", content)
	}
}

func TestRunArchiveJobWritesManifestAtZipRoot(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	setupFakeRclone(t)
	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatZip,
		ExtraArchiveFiles: []ArchiveExtraFile{{
			Name: protocol.BackupContentManifestName,
			Data: []byte(`{"version":1}`),
		}},
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	content := readZipEntry(t, filepath.Join(configDir, "artifacts", result.ArtifactName), protocol.BackupContentManifestName)
	if string(content) != `{"version":1}` {
		t.Fatalf("manifest content = %q", content)
	}
}

func TestRunArchiveJobSkipsUnreadableFilesAndRecordsManifestWarning(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	badPath := filepath.Join(backupDir, "bad.mov")
	if err := os.WriteFile(badPath, []byte("unreadable movie"), 0o644); err != nil {
		t.Fatalf("seed unreadable file: %v", err)
	}
	setupFakeRclone(t)

	originalOpen := openArchiveSourceFile
	t.Cleanup(func() { openArchiveSourceFile = originalOpen })
	openArchiveSourceFile = func(path string) (io.ReadCloser, error) {
		if path == badPath {
			return readErrorCloser{err: errors.New("input/output error")}, nil
		}
		return os.Open(path)
	}

	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatZip,
		ExtraArchiveFiles: []ArchiveExtraFile{{
			Name: protocol.BackupContentManifestName,
			Data: []byte(`{"version":1}`),
		}},
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	if len(result.ManifestWarnings) != 1 {
		t.Fatalf("ManifestWarnings = %#v, want one skipped-file warning", result.ManifestWarnings)
	}
	warning := result.ManifestWarnings[0]
	if warning.Code != "archive_file_skipped" || warning.Source != badPath || !strings.Contains(warning.Message, "input/output error") {
		t.Fatalf("warning = %+v, want skipped bad.mov read error", warning)
	}

	archivePath := filepath.Join(configDir, "artifacts", result.ArtifactName)
	if !zipEntryExists(t, archivePath, archiveEntryName(filepath.Join(backupDir, "hello.txt"))) {
		t.Fatalf("archive missing readable hello.txt")
	}
	if zipEntryExists(t, archivePath, archiveEntryName(badPath)) {
		t.Fatalf("archive contains unreadable bad.mov")
	}

	content := readZipEntry(t, archivePath, protocol.BackupContentManifestName)
	var manifest protocol.BackupContentManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(manifest.Warnings) != 1 {
		t.Fatalf("manifest warnings = %#v, want one skipped-file warning", manifest.Warnings)
	}
	if manifest.Warnings[0].Source != badPath {
		t.Fatalf("manifest warning source = %q, want %q", manifest.Warnings[0].Source, badPath)
	}
}

func TestRunArchiveJobLimitsGrowingFilesToPlannedSize(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	growingPath := filepath.Join(backupDir, "growing.log")
	if err := os.WriteFile(growingPath, []byte("abc"), 0o644); err != nil {
		t.Fatalf("seed growing file: %v", err)
	}
	setupFakeRclone(t)

	originalOpen := openArchiveSourceFile
	t.Cleanup(func() { openArchiveSourceFile = originalOpen })
	openArchiveSourceFile = func(path string) (io.ReadCloser, error) {
		if path == growingPath {
			return io.NopCloser(strings.NewReader("abcdef")), nil
		}
		return os.Open(path)
	}

	result := RunArchiveJob(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatTarGz,
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	content := readTarGzEntry(t, filepath.Join(configDir, "artifacts", result.ArtifactName), archiveEntryName(growingPath))
	if string(content) != "abc" {
		t.Fatalf("growing file content = %q, want planned-size prefix", content)
	}
}

func TestRunArchiveJobCancelsBlockedArchiveRead(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	blockedPath := filepath.Join(backupDir, "hello.txt")

	originalOpen := openArchiveSourceFile
	t.Cleanup(func() { openArchiveSourceFile = originalOpen })
	opened := make(chan struct{})
	openArchiveSourceFile = func(path string) (io.ReadCloser, error) {
		if path == blockedPath {
			close(opened)
			return newBlockingReadCloser(), nil
		}
		return os.Open(path)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan TaskResult, 1)
	go func() {
		done <- RunArchiveJobWithProgress(ctx, ExecutorConfig{
			ConfigDir:     configDir,
			RepoPath:      "tenant/agent-1",
			BackupDirs:    []string{backupDir},
			ArchiveFormat: protocol.ArchiveFormatZip,
		}, nil)
	}()

	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked archive read")
	}
	cancel()

	select {
	case result := <-done:
		if result.Status != "failed" {
			t.Fatalf("Status = %q, want failed", result.Status)
		}
		if !strings.Contains(result.ErrorLog, context.Canceled.Error()) {
			t.Fatalf("ErrorLog = %q, want context canceled", result.ErrorLog)
		}
	case <-time.After(time.Second):
		t.Fatal("archive job did not return after cancellation")
	}
}

func TestRunArchiveJobWithProgressReportsArchiveAndUploadProgress(t *testing.T) {
	configDir, backupDir := setupArchiveTestDirs(t)
	setupFakeRclone(t)

	var updates []struct {
		phase    string
		progress *BackupProgress
	}
	result := RunArchiveJobWithProgress(context.Background(), ExecutorConfig{
		ConfigDir:     configDir,
		RepoPath:      "tenant/agent-1",
		BackupDirs:    []string{backupDir},
		ArchiveFormat: protocol.ArchiveFormatZip,
	}, func(phase string, progress *BackupProgress) {
		var copied *BackupProgress
		if progress != nil {
			value := *progress
			copied = &value
		}
		updates = append(updates, struct {
			phase    string
			progress *BackupProgress
		}{phase: phase, progress: copied})
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	var archiveComplete, uploadComplete bool
	for _, update := range updates {
		if update.progress == nil {
			continue
		}
		switch update.phase {
		case "archive":
			if update.progress.TotalBytes > 0 && update.progress.BytesDone == update.progress.TotalBytes && update.progress.TotalFiles > 0 && update.progress.FilesDone == update.progress.TotalFiles {
				archiveComplete = true
			}
		case "archive-upload":
			if update.progress.TotalBytes == result.ArtifactSize && update.progress.BytesDone == result.ArtifactSize && update.progress.FilesDone == 1 {
				uploadComplete = true
			}
		}
	}
	if !archiveComplete {
		t.Fatalf("updates = %#v, want completed archive progress", updates)
	}
	if !uploadComplete {
		t.Fatalf("updates = %#v, want completed archive upload progress", updates)
	}
}

func TestRunBackupJobWithProgressReportsPhasesAndUsesProgressRunner(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	runner := &recordingRunner{
		repoSize: 2048,
		snapshots: []SnapshotInfo{
			{ID: "snap-1", Time: now},
		},
	}
	executor := &Executor{
		restic:     runner,
		backupDirs: []string{"/data"},
		excludes:   []string{"*.tmp"},
		retention:  RetentionPolicy{KeepLast: 1},
	}
	var phases []string
	var updates []BackupProgress

	result := executor.RunBackupJobWithProgress(context.Background(), func(phase string, progress *BackupProgress) {
		phases = append(phases, phase)
		if progress != nil {
			updates = append(updates, *progress)
		}
	})

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup_with_progress", "forget", "snapshots", "stats"})
	wantPhases := []string{"init", "backup", "backup", "backup", "forget", "stats"}
	if strings.Join(phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("progress phases = %#v, want %#v", phases, wantPhases)
	}
	if len(updates) != 2 {
		t.Fatalf("progress updates = %#v, want 2 updates", updates)
	}
	if updates[0].PercentDone != 0.5 || updates[0].FilesDone != 1 || updates[0].CurrentFile != "/data/a.txt" {
		t.Fatalf("first progress update = %+v", updates[0])
	}
	if updates[1].PercentDone != 1 || updates[1].FilesDone != 2 || updates[1].CurrentFile != "/data/b.txt" {
		t.Fatalf("second progress update = %+v", updates[1])
	}
}

func TestRunBackupJobWithProgressFallsBackToPlainBackupRunner(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	runner := &plainRecordingRunner{
		repoSize:  1024,
		snapshots: []SnapshotInfo{{ID: "snap-plain", Time: now}},
	}
	executor := &Executor{
		restic:     runner,
		backupDirs: []string{"/data"},
		retention:  RetentionPolicy{KeepLast: 1},
	}

	result := executor.RunBackupJobWithProgress(context.Background(), nil)

	if result.Status != "success" {
		t.Fatalf("Status = %q, want success; error log: %q", result.Status, result.ErrorLog)
	}
	if result.SnapshotID != "snap-plain" {
		t.Fatalf("SnapshotID = %q, want snap-plain", result.SnapshotID)
	}
	assertRunnerCalls(t, runner.calls, []string{"init", "backup", "forget", "snapshots", "stats"})
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

func TestTaskResultToProtocolMarshalsProtocolShape(t *testing.T) {
	startedAt := time.Date(2026, 5, 18, 9, 30, 0, 0, time.UTC)
	result := TaskResult{
		Type:       "backup",
		Status:     "failed",
		DurationMs: 2500,
		ErrorLog:   "backup: permission denied",
	}

	payload := result.ToProtocol("agent-007", startedAt)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(protocol payload) error = %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(protocol payload JSON) error = %v", err)
	}

	for _, key := range []string{"agent_id", "task_type", "status", "duration_ms", "error_log", "started_at", "finished_at"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("protocol payload JSON missing key %q: %s", key, data)
		}
	}
	if _, ok := got["type"]; ok {
		t.Fatalf("protocol payload JSON used local type key instead of task_type: %s", data)
	}

	assertProtocolResult(t, payload, protocol.TaskResultPayload{
		AgentID:    "agent-007",
		TaskType:   "backup",
		Status:     "failed",
		DurationMs: 2500,
		ErrorLog:   "backup: permission denied",
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(2500 * time.Millisecond),
	})
}

func TestTaskResultToProtocolCopiesSuccessMetadata(t *testing.T) {
	startedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	result := TaskResult{
		Type:       "backup",
		Status:     "success",
		DurationMs: 45000,
		SnapshotID: "abc123def456",
		RepoSize:   1073741824,
	}

	payload := result.ToProtocol("agent-002", startedAt)

	assertProtocolResult(t, payload, protocol.TaskResultPayload{
		AgentID:    "agent-002",
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "abc123def456",
		DurationMs: 45000,
		RepoSize:   1073741824,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(45000 * time.Millisecond),
	})
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
	repoSize    int64
	statsErr    error
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

func (r *recordingRunner) RunBackupWithProgress(_ context.Context, dirs []string, excludes []string, progressFn func(BackupProgress)) (string, error) {
	r.calls = append(r.calls, "backup_with_progress")
	if r.backupDelay > 0 {
		time.Sleep(r.backupDelay)
	}
	if r.backupErr == nil && progressFn != nil {
		progressFn(BackupProgress{
			PercentDone: 0.5,
			TotalFiles:  2,
			FilesDone:   1,
			TotalBytes:  200,
			BytesDone:   100,
			CurrentFile: "/data/a.txt",
		})
		progressFn(BackupProgress{
			PercentDone: 1,
			TotalFiles:  2,
			FilesDone:   2,
			TotalBytes:  200,
			BytesDone:   200,
			CurrentFile: "/data/b.txt",
		})
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

func (r *recordingRunner) RepositorySize(context.Context) (int64, error) {
	r.calls = append(r.calls, "stats")
	return r.repoSize, r.statsErr
}

func (r *recordingRunner) RestoreSnapshot(context.Context, string, string, []string) error {
	r.calls = append(r.calls, "restore")
	return r.restoreErr
}

type plainRecordingRunner struct {
	calls     []string
	backupErr error
	forgetErr error
	snapshots []SnapshotInfo
	repoSize  int64
}

func (r *plainRecordingRunner) InitRepo(context.Context) error {
	r.calls = append(r.calls, "init")
	return nil
}

func (r *plainRecordingRunner) RunBackup(context.Context, []string, []string) (string, error) {
	r.calls = append(r.calls, "backup")
	return "", r.backupErr
}

func (r *plainRecordingRunner) RunForget(context.Context, RetentionPolicy) error {
	r.calls = append(r.calls, "forget")
	return r.forgetErr
}

func (r *plainRecordingRunner) ListSnapshots(context.Context) ([]SnapshotInfo, error) {
	r.calls = append(r.calls, "snapshots")
	return r.snapshots, nil
}

func (r *plainRecordingRunner) RepositorySize(context.Context) (int64, error) {
	r.calls = append(r.calls, "stats")
	return r.repoSize, nil
}

func (r *plainRecordingRunner) RestoreSnapshot(context.Context, string, string, []string) error {
	r.calls = append(r.calls, "restore")
	return nil
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

func assertProtocolResult(t *testing.T, got, want protocol.TaskResultPayload) {
	t.Helper()
	if got.AgentID != want.AgentID {
		t.Fatalf("AgentID = %q, want %q", got.AgentID, want.AgentID)
	}
	if got.TaskType != want.TaskType {
		t.Fatalf("TaskType = %q, want %q", got.TaskType, want.TaskType)
	}
	if got.Status != want.Status {
		t.Fatalf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.SnapshotID != want.SnapshotID {
		t.Fatalf("SnapshotID = %q, want %q", got.SnapshotID, want.SnapshotID)
	}
	if got.DurationMs != want.DurationMs {
		t.Fatalf("DurationMs = %d, want %d", got.DurationMs, want.DurationMs)
	}
	if got.RepoSize != want.RepoSize {
		t.Fatalf("RepoSize = %d, want %d", got.RepoSize, want.RepoSize)
	}
	if got.ErrorLog != want.ErrorLog {
		t.Fatalf("ErrorLog = %q, want %q", got.ErrorLog, want.ErrorLog)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("StartedAt = %s, want %s", got.StartedAt, want.StartedAt)
	}
	if !got.FinishedAt.Equal(want.FinishedAt) {
		t.Fatalf("FinishedAt = %s, want %s", got.FinishedAt, want.FinishedAt)
	}
}

func shellQuoteForSh(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func setupArchiveTestDirs(t *testing.T) (string, string) {
	t.Helper()
	configDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "hello.txt"), []byte("hello archive"), 0o644); err != nil {
		t.Fatalf("seed backup file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "rclone.conf"), []byte("[vaultfleet]\ntype = memory\n"), 0o600); err != nil {
		t.Fatalf("write rclone config: %v", err)
	}
	return configDir, backupDir
}

func setupFakeRclone(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	rclonePath := filepath.Join(binDir, "rclone")
	if err := os.WriteFile(rclonePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake rclone: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func readTarGzEntry(t *testing.T, archivePath string, entryName string) []byte {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open tar.gz: %v", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if header.Name == entryName {
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read tar manifest: %v", err)
			}
			return data
		}
	}
	t.Fatalf("entry %q not found in %s", entryName, archivePath)
	return nil
}

func readZipEntry(t *testing.T, archivePath string, entryName string) []byte {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != entryName {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip manifest: %v", err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read zip manifest: %v", err)
		}
		return data
	}
	t.Fatalf("entry %q not found in %s", entryName, archivePath)
	return nil
}

func zipEntryExists(t *testing.T, archivePath string, entryName string) bool {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name == entryName {
			return true
		}
	}
	return false
}

func archiveEntryName(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "/")
}

type readErrorCloser struct {
	err error
}

func (r readErrorCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r readErrorCloser) Close() error {
	return nil
}

type blockingReadCloser struct {
	closed chan struct{}
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, context.Canceled
}

func (r *blockingReadCloser) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}
