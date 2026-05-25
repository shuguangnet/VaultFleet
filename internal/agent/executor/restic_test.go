package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildInitCmdIncludesRepoPasswordAndRcloneConfigEnv(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, ".restic-password")
	os.WriteFile(pwFile, []byte("secret"), 0o600)

	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "backups/agent-1",
	}

	cmd := runner.buildInitCmd()

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"init",
		"-r",
		"rclone:vaultfleet:backups/agent-1",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
	assertEnvContains(t, cmd.Env, "RCLONE_CONFIG=/tmp/rclone.conf")
}

func TestBuildInitCmdProvidesCacheDirWhenServiceEnvironmentOmitsHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	configDir := t.TempDir()
	pwFile := filepath.Join(configDir, ".restic-password")
	os.WriteFile(pwFile, []byte("secret"), 0o600)

	runner := ResticRunner{
		RcloneConfPath: filepath.Join(configDir, "rclone.conf"),
		PasswordFile:   pwFile,
		RepoPath:       "backups/agent-1",
	}

	cmd := runner.buildInitCmd()

	assertEnvContains(t, cmd.Env, "XDG_CACHE_HOME="+filepath.Join(configDir, ".cache"))
}

func TestBuildBackupCmdIncludesExcludesAndDirectories(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildBackupCmd([]string{"/home/alice", "/etc"}, []string{"*.tmp", "/home/alice/cache"})

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"backup",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
		"--exclude=*.tmp",
		"--exclude=/home/alice/cache",
		"/home/alice",
		"/etc",
	})
	assertEnvContains(t, cmd.Env, "RCLONE_CONFIG=/tmp/rclone.conf")
}

func TestBuildForgetCmdIncludesPruneAndNonZeroRetention(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildForgetCmd(RetentionPolicy{
		KeepLast:    3,
		KeepDaily:   7,
		KeepMonthly: 12,
	})

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"forget",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
		"--prune",
		"--keep-last=3",
		"--keep-daily=7",
		"--keep-monthly=12",
	})
}

func TestBuildSnapshotsCmdRequestsJSON(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildSnapshotsCmd()

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"snapshots",
		"--json",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBuildLsSnapshotCmdIncludesSnapshotIDAndJSON(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildLsSnapshotCmd("abc123")

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"ls",
		"abc123",
		"--json",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBuildStatsCmdRequestsRawRepositorySizeAsJSON(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildStatsCmd()

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"stats",
		"--mode",
		"raw-data",
		"--json",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBuildRestoreCmdIncludesSnapshotAndTarget(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildRestoreCmdWithIncludes("abc123", "/restore/target", nil)

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"restore",
		"abc123",
		"--target",
		"/restore/target",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBuildRestoreCmdWithIncludePaths(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildRestoreCmdWithIncludes("abc123", "/restore/target", []string{"/data/a", "/data/c"})

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"restore",
		"abc123",
		"--target",
		"/restore/target",
		"--include",
		"/data/a",
		"--include",
		"/data/c",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBuildRestoreCmdRejectsIncludePathPatterns(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	_, err := runner.buildRestoreCmdWithIncludesChecked(context.Background(), "abc123", "/restore/target", []string{"/data/file[1].txt"})

	if err == nil {
		t.Fatal("buildRestoreCmdWithIncludesChecked() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "include path contains unsupported pattern characters") {
		t.Fatalf("error = %q, want unsupported pattern characters", err.Error())
	}
}

func TestBuildRestoreCmdWithEmptyIncludesHasNoIncludeFlag(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildRestoreCmdWithIncludes("abc123", "/restore/target", nil)

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"restore",
		"abc123",
		"--target",
		"/restore/target",
		"-r",
		"rclone:vaultfleet:repo",
		"--password-file",
		pwFile,
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestInitRepoIgnoresAlreadyInitializedError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
	}{
		{
			name:   "restic already initialized",
			stderr: "repository already initialized\n",
		},
		{
			name: "rclone config already exists",
			stderr: "Fatal: create repository at rclone:vaultfleet:repo failed: " +
				"Fatal: unable to open repository at rclone:vaultfleet:repo: config file already exists\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFakeRestic(t, dir, fakeResticScript{
				Stdout: "",
				Stderr: tt.stderr,
				Exit:   1,
			})
			prependPath(t, dir)

			pwFile := writeTempPasswordFile(t, "secret")
			runner := ResticRunner{
				RcloneConfPath: "/tmp/rclone.conf",
				PasswordFile:   pwFile,
				RepoPath:       "repo",
			}

			if err := runner.InitRepo(context.Background()); err != nil {
				t.Fatalf("InitRepo() error = %v, want nil", err)
			}
		})
	}
}

func TestInitRepoSkipsInitWhenSnapshotsCanListExistingRepository(t *testing.T) {
	dir := t.TempDir()
	writeFakeResticRouter(t, dir, map[string]fakeResticScript{
		"cat":       {Stderr: "repository does not exist\n", Exit: 10},
		"snapshots": {Stdout: `[]` + "\n"},
		"init":      {Stderr: "init should not be called\n", Exit: 1},
	})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	if err := runner.InitRepo(context.Background()); err != nil {
		t.Fatalf("InitRepo() error = %v, want nil", err)
	}
}

func TestInitRepoCallsRcloneMkdirBeforeResticInit(t *testing.T) {
	dir := t.TempDir()

	writeFakeRclone(t, dir, fakeResticScript{})
	writeFakeResticRouter(t, dir, map[string]fakeResticScript{
		"snapshots": {Stderr: "Is there a repository at the following location?\n", Exit: 1},
		"init":      {Stdout: "created restic repository\n"},
	})
	prependPath(t, dir)

	runner := ResticRunner{
		RcloneConfPath: filepath.Join(dir, "rclone.conf"),
		PasswordFile:   filepath.Join(dir, ".restic-password"),
		RepoPath:       "backups/node-1",
	}

	if err := runner.InitRepo(context.Background()); err != nil {
		t.Fatalf("InitRepo() error = %v, want nil", err)
	}

	logPath := filepath.Join(dir, "rclone.log")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rclone log: %v", err)
	}
	got := strings.TrimSpace(string(logData))
	want := "--config " + filepath.Join(dir, "rclone.conf") + " mkdir vaultfleet:backups/node-1"
	if got != want {
		t.Fatalf("rclone called with %q, want %q", got, want)
	}
}

func TestInitRepoReturnsErrorWhenRcloneMkdirFails(t *testing.T) {
	dir := t.TempDir()

	writeFakeRclone(t, dir, fakeResticScript{Stderr: "mkdir failed\n", Exit: 1})
	writeFakeResticRouter(t, dir, map[string]fakeResticScript{
		"snapshots": {Stderr: "Is there a repository at the following location?\n", Exit: 1},
	})
	prependPath(t, dir)

	runner := ResticRunner{
		RcloneConfPath: filepath.Join(dir, "rclone.conf"),
		PasswordFile:   filepath.Join(dir, ".restic-password"),
		RepoPath:       "backups/node-1",
	}

	err := runner.InitRepo(context.Background())
	if err == nil {
		t.Fatal("InitRepo() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "pre-create remote directory") {
		t.Fatalf("InitRepo() error = %q, want 'pre-create remote directory'", err.Error())
	}
}

func TestRunBackupReturnsStdoutAndIncludesStderrOnFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeRestic(t, dir, fakeResticScript{Stdout: "snapshot abc123 saved\n"})
		prependPath(t, dir)

		pwFile := writeTempPasswordFile(t, "secret")
		runner := ResticRunner{
			RcloneConfPath: "/tmp/rclone.conf",
			PasswordFile:   pwFile,
			RepoPath:       "repo",
		}

		got, err := runner.RunBackup(context.Background(), []string{"/data"}, nil)
		if err != nil {
			t.Fatalf("RunBackup() error = %v", err)
		}
		if got != "snapshot abc123 saved\n" {
			t.Fatalf("RunBackup() stdout = %q", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeRestic(t, dir, fakeResticScript{Stderr: "backup failed for /data\n", Exit: 2})
		prependPath(t, dir)

		pwFile := writeTempPasswordFile(t, "secret")
		runner := ResticRunner{
			RcloneConfPath: "/tmp/rclone.conf",
			PasswordFile:   pwFile,
			RepoPath:       "repo",
		}

		_, err := runner.RunBackup(context.Background(), []string{"/data"}, nil)
		if err == nil {
			t.Fatal("RunBackup() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "backup failed for /data") {
			t.Fatalf("RunBackup() error = %q, want stderr included", err.Error())
		}
	})
}

func TestListSnapshotsParsesResticJSON(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{
		Stdout: `[{"id":"abc123","time":"2026-05-18T12:34:56Z","paths":["/data"],"hostname":"agent-1","size":4096}]` + "\n",
	})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	got, err := runner.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSnapshots() returned %d snapshots, want 1", len(got))
	}
	wantTime := time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC)
	if got[0].ID != "abc123" || got[0].Hostname != "agent-1" || got[0].Size != 4096 || !got[0].Time.Equal(wantTime) {
		t.Fatalf("ListSnapshots()[0] = %+v", got[0])
	}
	if len(got[0].Paths) != 1 || got[0].Paths[0] != "/data" {
		t.Fatalf("ListSnapshots()[0].Paths = %#v", got[0].Paths)
	}
}

func TestLsSnapshotParsesResticJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonl := `{"struct_type":"snapshot","id":"abc123","time":"2026-05-18T12:00:00Z","paths":["/data"]}
{"struct_type":"node","name":"data","type":"dir","path":"/data","size":0,"mtime":"2026-05-18T12:00:00Z"}
{"struct_type":"node","name":"file.txt","type":"file","path":"/data/file.txt","size":1024,"mtime":"2026-05-18T11:30:00Z"}
`
	writeFakeRestic(t, dir, fakeResticScript{Stdout: jsonl})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	got, err := runner.LsSnapshot(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("LsSnapshot() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LsSnapshot() returned %d entries, want 2", len(got))
	}
	if got[0].Path != "/data" || got[0].Type != "dir" {
		t.Fatalf("entry[0] = %+v, want dir /data", got[0])
	}
	if got[1].Path != "/data/file.txt" || got[1].Type != "file" || got[1].Size != 1024 {
		t.Fatalf("entry[1] = %+v, want file /data/file.txt size=1024", got[1])
	}
	if got[1].Mtime != "2026-05-18T11:30:00Z" {
		t.Fatalf("entry[1].Mtime = %q, want 2026-05-18T11:30:00Z", got[1].Mtime)
	}
}

func TestRepositorySizeParsesResticStatsJSON(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{
		Stdout: `{"total_size":987654,"total_file_count":12}` + "\n",
	})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	got, err := runner.RepositorySize(context.Background())
	if err != nil {
		t.Fatalf("RepositorySize() error = %v", err)
	}
	if got != 987654 {
		t.Fatalf("RepositorySize() = %d, want 987654", got)
	}
}

func TestRepositorySizeReturnsStderrOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{Stderr: "stats failed\n", Exit: 1})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	_, err := runner.RepositorySize(context.Background())
	if err == nil {
		t.Fatal("RepositorySize() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "stats failed") {
		t.Fatalf("RepositorySize() error = %q, want stderr included", err.Error())
	}
}

func TestRestoreSnapshotReturnsStderrOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{Stderr: "restore failed\n", Exit: 1})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	err := runner.RestoreSnapshot(context.Background(), "abc123", "/restore", nil)
	if err == nil {
		t.Fatal("RestoreSnapshot() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("RestoreSnapshot() error = %q, want stderr included", err.Error())
	}
}

func TestRestoreSnapshotRejectsIncludePathPatternsBeforeRunningRestic(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{Stderr: "restic should not run\n", Exit: 1})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	err := runner.RestoreSnapshot(context.Background(), "abc123", "/restore", []string{"/data/*.txt"})

	if err == nil {
		t.Fatal("RestoreSnapshot() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "include path contains unsupported pattern characters") {
		t.Fatalf("RestoreSnapshot() error = %q, want unsupported pattern characters", err.Error())
	}
	if strings.Contains(err.Error(), "restic should not run") {
		t.Fatalf("RestoreSnapshot() ran restic unexpectedly: %v", err)
	}
}

func TestRunForgetHonorsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFakeRestic(t, dir, fakeResticScript{SleepSeconds: 2})
	prependPath(t, dir)

	pwFile := writeTempPasswordFile(t, "secret")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.RunForget(ctx, RetentionPolicy{KeepLast: 1})
	if err == nil {
		t.Fatal("RunForget() error = nil, want context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunForget() error = %v, want context.Canceled", err)
	}
}

func TestBaseArgsUsesInsecureNoPasswordWhenPasswordFileIsEmpty(t *testing.T) {
	pwFile := writeTempPasswordFile(t, "")
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   pwFile,
		RepoPath:       "repo",
	}

	cmd := runner.buildInitCmd()

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"init",
		"-r",
		"rclone:vaultfleet:repo",
		"--insecure-no-password",
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
	})
}

func TestBaseArgsUsesInsecureNoPasswordWhenPasswordFileMissing(t *testing.T) {
	runner := ResticRunner{
		RcloneConfPath: "/tmp/rclone.conf",
		PasswordFile:   "/nonexistent/.restic-password",
		RepoPath:       "repo",
	}

	cmd := runner.buildBackupCmd([]string{"/data"}, nil)

	assertArgsEqual(t, cmd.Args, []string{
		"restic",
		"backup",
		"-r",
		"rclone:vaultfleet:repo",
		"--insecure-no-password",
		"-o",
		"rclone.args=serve restic --stdio --config /tmp/rclone.conf",
		"/data",
	})
}

func writeTempPasswordFile(t *testing.T, password string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".restic-password")
	if err := os.WriteFile(path, []byte(password), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	return path
}

func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args length = %d, want %d\nargs: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q\nargs: %#v", i, got[i], want[i], got)
		}
	}
}

func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, entry := range env {
		if entry == want {
			return
		}
	}
	t.Fatalf("env missing %q in %#v", want, env)
}

type fakeResticScript struct {
	Stdout       string
	Stderr       string
	Exit         int
	SleepSeconds int
}

func writeFakeRestic(t *testing.T, dir string, script fakeResticScript) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake restic shell script is not supported on windows")
	}

	path := filepath.Join(dir, "restic")
	content := "#!/bin/sh\n"
	if script.SleepSeconds > 0 {
		content += "sleep " + strconv.Itoa(script.SleepSeconds) + "\n"
	}
	if script.Stdout != "" {
		content += "printf '%s' " + shellQuote(script.Stdout) + "\n"
	}
	if script.Stderr != "" {
		content += "printf '%s' " + shellQuote(script.Stderr) + " >&2\n"
	}
	content += "exit " + strconv.Itoa(script.Exit) + "\n"

	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
}

func writeFakeResticRouter(t *testing.T, dir string, scripts map[string]fakeResticScript) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake restic shell script is not supported on windows")
	}

	path := filepath.Join(dir, "restic")
	content := "#!/bin/sh\n"
	content += "case \"$1\" in\n"
	keys := make([]string, 0, len(scripts))
	for key := range scripts {
		keys = append(keys, key)
	}
	for _, key := range keys {
		script := scripts[key]
		content += key + ")\n"
		if script.SleepSeconds > 0 {
			content += "sleep " + strconv.Itoa(script.SleepSeconds) + "\n"
		}
		if script.Stdout != "" {
			content += "printf '%s' " + shellQuote(script.Stdout) + "\n"
		}
		if script.Stderr != "" {
			content += "printf '%s' " + shellQuote(script.Stderr) + " >&2\n"
		}
		content += "exit " + strconv.Itoa(script.Exit) + "\n"
		content += ";;\n"
	}
	content += "*) echo unexpected command \"$1\" >&2; exit 99;;\n"
	content += "esac\n"

	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake restic router: %v", err)
	}
}

func writeFakeRclone(t *testing.T, dir string, script fakeResticScript) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake rclone shell script is not supported on windows")
	}

	path := filepath.Join(dir, "rclone")
	logPath := filepath.Join(dir, "rclone.log")
	content := "#!/bin/sh\n"
	content += "echo \"$@\" >> " + shellQuote(logPath) + "\n"
	if script.Stderr != "" {
		content += "printf '%s' " + shellQuote(script.Stderr) + " >&2\n"
	}
	content += "exit " + strconv.Itoa(script.Exit) + "\n"

	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake rclone: %v", err)
	}
}

func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
