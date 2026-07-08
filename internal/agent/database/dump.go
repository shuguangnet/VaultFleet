package database

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

const dumpPhase = "database-dump"

type LogFunc func(level string, phase string, stream string, line string)

type CommandRunner func(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error)

type Config struct {
	ConfigDir string
	Sources   []protocol.BackupSource
	TaskLog   LogFunc
	Runner    CommandRunner
	Now       func() time.Time
}

type Result struct {
	Paths    []string
	Metadata *protocol.DatabaseBackupMetadata
}

func Prepare(ctx context.Context, cfg Config) (Result, func(), error) {
	sources := databaseSources(cfg.Sources)
	if len(sources) == 0 {
		return Result{}, func() {}, nil
	}
	if cfg.Runner == nil {
		cfg.Runner = runCommand
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	configDir := strings.TrimSpace(cfg.ConfigDir)
	if configDir == "" {
		return Result{}, func() {}, errors.New("database dump config dir is required")
	}
	stageDir, err := os.MkdirTemp(configDir, "database-dumps-*")
	if err != nil {
		return Result{}, func() {}, fmt.Errorf("prepare database dump staging: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stageDir) }

	result := Result{
		Metadata: &protocol.DatabaseBackupMetadata{},
	}
	for _, source := range sources {
		meta, err := dumpSource(ctx, cfg, stageDir, source)
		if err != nil {
			cleanup()
			return Result{}, func() {}, err
		}
		result.Paths = append(result.Paths, meta.OutputPath)
		result.Metadata.Dumps = append(result.Metadata.Dumps, meta)
	}
	return result, cleanup, nil
}

func databaseSources(sources []protocol.BackupSource) []protocol.DatabaseBackupSource {
	var databases []protocol.DatabaseBackupSource
	for _, source := range sources {
		if source.Type == protocol.BackupSourceTypeDatabase && source.Database != nil {
			databases = append(databases, *source.Database)
		}
	}
	return databases
}

func dumpSource(ctx context.Context, cfg Config, stageDir string, source protocol.DatabaseBackupSource) (protocol.DatabaseDumpMetadata, error) {
	name := dumpFileName(source, cfg.Now())
	outputPath := filepath.Join(stageDir, name)
	log(cfg.TaskLog, "info", "system", "starting "+source.Engine+" dump "+dumpLabel(source))

	env, command, args, err := buildCommand(stageDir, source)
	if err != nil {
		return protocol.DatabaseDumpMetadata{}, err
	}
	stdout, stderr, err := cfg.Runner(ctx, env, command, args...)
	if len(stderr) > 0 {
		log(cfg.TaskLog, "error", "stderr", redactSecrets(string(stderr), source.Password))
	}
	if err != nil {
		return protocol.DatabaseDumpMetadata{}, fmt.Errorf("database dump %s: %w", dumpLabel(source), err)
	}
	if err := writeDump(outputPath, stdout, source.Compress); err != nil {
		return protocol.DatabaseDumpMetadata{}, fmt.Errorf("write database dump %s: %w", outputPath, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return protocol.DatabaseDumpMetadata{}, fmt.Errorf("stat database dump %s: %w", outputPath, err)
	}
	meta := protocol.DatabaseDumpMetadata{
		Engine:        source.Engine,
		ExecutionMode: source.ExecutionMode,
		Database:      source.Database,
		AllDatabases:  source.AllDatabases,
		OutputPath:    outputPath,
		OutputName:    name,
		Size:          info.Size(),
		Compressed:    source.Compress,
	}
	if source.DockerContainer != nil {
		meta.ContainerName = firstContainerIdentity(*source.DockerContainer)
	}
	log(cfg.TaskLog, "info", "system", "completed "+source.Engine+" dump "+name)
	return meta, nil
}

func buildCommand(stageDir string, source protocol.DatabaseBackupSource) ([]string, string, []string, error) {
	switch source.Engine {
	case protocol.DatabaseEnginePostgreSQL:
		return buildPostgresCommand(source)
	case protocol.DatabaseEngineMySQL:
		return buildMySQLCommand(stageDir, source)
	default:
		return nil, "", nil, fmt.Errorf("unsupported database engine %q", source.Engine)
	}
}

func buildPostgresCommand(source protocol.DatabaseBackupSource) ([]string, string, []string, error) {
	tool := "pg_dump"
	if source.AllDatabases {
		tool = "pg_dumpall"
	}
	args := postgresConnectionArgs(source)
	if !source.AllDatabases {
		args = append(args, "-d", source.Database)
	}
	args = append(args, source.ExtraArgs...)
	env := []string{}
	if source.Password != "" {
		env = append(env, "PGPASSWORD="+source.Password)
	}
	if source.ExecutionMode == protocol.DatabaseExecutionDocker {
		container := dockerContainerName(source)
		if container == "" {
			return nil, "", nil, errors.New("database docker source needs a container")
		}
		dockerArgs := []string{"exec", "-i"}
		if source.Password != "" {
			dockerArgs = append(dockerArgs, "-e", "PGPASSWORD="+source.Password)
		}
		dockerArgs = append(dockerArgs, container, tool)
		dockerArgs = append(dockerArgs, args...)
		return nil, "docker", dockerArgs, nil
	}
	return env, tool, args, nil
}

func postgresConnectionArgs(source protocol.DatabaseBackupSource) []string {
	var args []string
	if source.Host != "" {
		args = append(args, "-h", source.Host)
	}
	if source.Port > 0 {
		args = append(args, "-p", strconv.Itoa(source.Port))
	}
	if source.Username != "" {
		args = append(args, "-U", source.Username)
	}
	return args
}

func buildMySQLCommand(stageDir string, source protocol.DatabaseBackupSource) ([]string, string, []string, error) {
	args := mysqlConnectionArgs(source)
	if source.AllDatabases {
		args = append(args, "--all-databases")
	} else {
		args = append(args, source.Database)
	}
	args = append(args, source.ExtraArgs...)
	if source.ExecutionMode == protocol.DatabaseExecutionDocker {
		container := dockerContainerName(source)
		if container == "" {
			return nil, "", nil, errors.New("database docker source needs a container")
		}
		dockerArgs := []string{"exec", "-i"}
		if source.Password != "" {
			dockerArgs = append(dockerArgs, "-e", "MYSQL_PWD="+source.Password)
		}
		dockerArgs = append(dockerArgs, container, "mysqldump")
		dockerArgs = append(dockerArgs, args...)
		return nil, "docker", dockerArgs, nil
	}
	if source.Password != "" {
		defaultsFile, err := writeMySQLDefaultsFile(stageDir, source)
		if err != nil {
			return nil, "", nil, err
		}
		args = append([]string{"--defaults-extra-file=" + defaultsFile}, args...)
	}
	return nil, "mysqldump", args, nil
}

func mysqlConnectionArgs(source protocol.DatabaseBackupSource) []string {
	var args []string
	if source.Host != "" {
		args = append(args, "-h", source.Host)
	}
	if source.Port > 0 {
		args = append(args, "-P", strconv.Itoa(source.Port))
	}
	if source.Username != "" {
		args = append(args, "-u", source.Username)
	}
	return args
}

func writeMySQLDefaultsFile(stageDir string, source protocol.DatabaseBackupSource) (string, error) {
	path := filepath.Join(stageDir, ".mysql-"+safeFilePart(source.Username)+".cnf")
	content := "[client]\nuser=" + source.Username + "\npassword=" + source.Password + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write mysql defaults file: %w", err)
	}
	return path, nil
}

func writeDump(path string, data []byte, compress bool) error {
	if compress {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		gz := gzip.NewWriter(file)
		if _, err := gz.Write(data); err != nil {
			_ = gz.Close()
			_ = file.Close()
			return err
		}
		if err := gz.Close(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	}
	return os.WriteFile(path, data, 0o600)
}

func dumpFileName(source protocol.DatabaseBackupSource, now time.Time) string {
	if source.OutputName != "" {
		return safeFilePart(source.OutputName)
	}
	name := source.Engine
	if source.AllDatabases {
		name += "-all"
	} else if source.Database != "" {
		name += "-" + source.Database
	}
	name += "-" + now.UTC().Format("20060102-150405") + ".sql"
	if source.Compress {
		name += ".gz"
	}
	return safeFilePart(name)
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return "database-dump.sql"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "database-dump.sql"
	}
	return b.String()
}

func dockerContainerName(source protocol.DatabaseBackupSource) string {
	if source.DockerContainer == nil {
		return ""
	}
	return firstContainerIdentity(*source.DockerContainer)
}

func firstContainerIdentity(container protocol.DockerContainerBackupSource) string {
	for _, value := range []string{container.ContainerID, container.Name, container.ComposeService} {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value != "" {
			return value
		}
	}
	return ""
}

func dumpLabel(source protocol.DatabaseBackupSource) string {
	if source.AllDatabases {
		return "all databases"
	}
	if source.Database != "" {
		return source.Database
	}
	return source.Engine
}

func redactSecrets(text string, secrets ...string) string {
	text = redact.Text(text)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			text = strings.ReplaceAll(text, secret, redact.Placeholder)
		}
	}
	return text
}

func log(logFn LogFunc, level string, stream string, line string) {
	if logFn != nil {
		logFn(level, dumpPhase, stream, line)
	}
}

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
