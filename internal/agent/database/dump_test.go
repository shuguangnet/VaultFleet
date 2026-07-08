package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

func TestPreparePostgresHostDump(t *testing.T) {
	var command string
	var env []string
	result, cleanup, err := Prepare(context.Background(), Config{
		ConfigDir: t.TempDir(),
		Sources: []protocol.BackupSource{{
			Type: protocol.BackupSourceTypeDatabase,
			Database: &protocol.DatabaseBackupSource{
				Engine:        protocol.DatabaseEnginePostgreSQL,
				ExecutionMode: protocol.DatabaseExecutionHost,
				Host:          "127.0.0.1",
				Port:          5432,
				Username:      "postgres",
				Password:      "secret",
				Database:      "app",
				Compress:      true,
			},
		}},
		Now: func() time.Time { return time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC) },
		Runner: func(_ context.Context, runnerEnv []string, name string, args ...string) ([]byte, []byte, error) {
			env = runnerEnv
			command = name + " " + strings.Join(args, " ")
			return []byte("SQL"), nil, nil
		},
	})
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, []string{"PGPASSWORD=secret"}, env)
	assert.Equal(t, "pg_dump -h 127.0.0.1 -p 5432 -U postgres -d app", command)
	require.Len(t, result.Paths, 1)
	assert.FileExists(t, result.Paths[0])
	assert.True(t, strings.HasSuffix(result.Paths[0], ".sql.gz"))
	require.NotNil(t, result.Metadata)
	require.Len(t, result.Metadata.Dumps, 1)
	assert.Equal(t, "app", result.Metadata.Dumps[0].Database)
	assert.True(t, result.Metadata.Dumps[0].Compressed)
}

func TestPrepareMySQLDockerDump(t *testing.T) {
	var command string
	result, cleanup, err := Prepare(context.Background(), Config{
		ConfigDir: t.TempDir(),
		Sources: []protocol.BackupSource{{
			Type: protocol.BackupSourceTypeDatabase,
			Database: &protocol.DatabaseBackupSource{
				Engine:        protocol.DatabaseEngineMySQL,
				ExecutionMode: protocol.DatabaseExecutionDocker,
				Username:      "root",
				Password:      "secret",
				AllDatabases:  true,
				DockerContainer: &protocol.DockerContainerBackupSource{
					Name: "mysql",
				},
			},
		}},
		Runner: func(_ context.Context, _ []string, name string, args ...string) ([]byte, []byte, error) {
			command = name + " " + strings.Join(args, " ")
			return []byte("SQL"), nil, nil
		},
	})
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, "docker exec -i -e MYSQL_PWD=secret mysql mysqldump -u root --all-databases", command)
	require.Len(t, result.Metadata.Dumps, 1)
	assert.True(t, result.Metadata.Dumps[0].AllDatabases)
	assert.Equal(t, "mysql", result.Metadata.Dumps[0].ContainerName)
}

func TestPrepareRedactsSecretFromStderr(t *testing.T) {
	var lines []string
	_, _, err := Prepare(context.Background(), Config{
		ConfigDir: t.TempDir(),
		Sources: []protocol.BackupSource{{
			Type: protocol.BackupSourceTypeDatabase,
			Database: &protocol.DatabaseBackupSource{
				Engine:        protocol.DatabaseEnginePostgreSQL,
				ExecutionMode: protocol.DatabaseExecutionHost,
				Username:      "postgres",
				Password:      "plain-secret",
				Database:      "app",
			},
		}},
		TaskLog: func(_, _, _ string, line string) {
			lines = append(lines, line)
		},
		Runner: func(context.Context, []string, string, ...string) ([]byte, []byte, error) {
			return nil, []byte("password plain-secret failed"), errors.New("exit")
		},
	})
	require.Error(t, err)
	assert.NotContains(t, strings.Join(lines, "\n"), "plain-secret")
	assert.Contains(t, strings.Join(lines, "\n"), redact.Placeholder)
}

func TestPrepareCleanupRemovesStageDir(t *testing.T) {
	result, cleanup, err := Prepare(context.Background(), Config{
		ConfigDir: t.TempDir(),
		Sources: []protocol.BackupSource{{
			Type: protocol.BackupSourceTypeDatabase,
			Database: &protocol.DatabaseBackupSource{
				Engine:        protocol.DatabaseEngineMySQL,
				ExecutionMode: protocol.DatabaseExecutionHost,
				Username:      "root",
				Database:      "app",
			},
		}},
		Runner: func(context.Context, []string, string, ...string) ([]byte, []byte, error) {
			return []byte("SQL"), nil, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Paths, 1)
	stageDir := strings.TrimSuffix(result.Paths[0], "/"+result.Metadata.Dumps[0].OutputName)
	assert.DirExists(t, stageDir)

	cleanup()

	_, err = os.Stat(stageDir)
	assert.True(t, os.IsNotExist(err))
}
