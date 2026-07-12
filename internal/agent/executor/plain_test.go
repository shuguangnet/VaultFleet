package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainRestoreUsesMetadataAndPreservesAbsoluteDockerPath(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "rclone.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$RCLONE_TEST_LOG"
case " $* " in
  *" cat "*) printf '%s' '{"timestamp":"2026-07-12T00:00:00Z","dirs":["/opt/app/mount","/srv/other"]}' ;;
esac
`
	requireWriteExecutable(t, filepath.Join(binDir, "rclone"), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RCLONE_TEST_LOG", logPath)

	runner := PlainRunner{RcloneConfPath: "/tmp/rclone.conf", RepoPath: "repo/source"}
	if err := runner.RestoreSnapshot(context.Background(), "snapshot-1", "/", []string{"/opt/app/mount"}); err != nil {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(raw)
	if strings.Contains(commands, " lsd ") {
		t.Fatalf("RestoreSnapshot() unexpectedly used lsd: %s", commands)
	}
	if !strings.Contains(commands, "copy vaultfleet:repo/source/data/mount /opt/app/mount") {
		t.Fatalf("RestoreSnapshot() copy command = %s", commands)
	}
	if strings.Contains(commands, "data/other") {
		t.Fatalf("RestoreSnapshot() restored unselected directory: %s", commands)
	}
}

func TestPlainRestoreRejectsMissingSelectedPath(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
case " $* " in
  *" cat "*) printf '%s' '{"timestamp":"2026-07-12T00:00:00Z","dirs":["/srv/data"]}' ;;
esac
`
	requireWriteExecutable(t, filepath.Join(binDir, "rclone"), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := PlainRunner{RcloneConfPath: "/tmp/rclone.conf", RepoPath: "repo/source"}
	err := runner.RestoreSnapshot(context.Background(), "snapshot-1", "/", []string{"/opt/missing"})
	if err == nil || !strings.Contains(err.Error(), "none of the requested restore paths") {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}
}

func requireWriteExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
