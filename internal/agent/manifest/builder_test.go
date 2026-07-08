package manifest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestBuildCreatesNonSecretSourceSummaries(t *testing.T) {
	generatedAt := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	dbPath := "/tmp/database-dumps/app.sql.gz"

	doc := Build(BuildInput{
		AgentID:       "agent-1",
		AgentVersion:  "1.2.3",
		GeneratedAt:   generatedAt,
		BackupMode:    protocol.BackupModeSnapshot,
		ArchiveFormat: protocol.ArchiveFormatTarGz,
		BackupDirs:    []string{"/srv/site", dbPath, dbPath},
		Excludes:      []string{"*.log", "", "*.log"},
		Policy: &protocol.PolicyPushPayload{
			AgentID: "policy-agent",
			Storage: protocol.StorageConfig{
				RcloneType:   "s3",
				RepoPath:     "tenant/agent",
				RcloneConfig: map[string]string{"access_key_id": "secret"},
				RcloneArgs:   map[string]string{"s3-no-check-bucket": "true"},
			},
			ResticPassword: "restic-secret",
		},
		Docker: &protocol.DockerBackupMetadata{
			Sources: []protocol.DockerResolvedSource{{
				ContainerID:   "container-1",
				Name:          "web",
				Image:         "nginx:latest",
				Compose:       protocol.DockerComposeInfo{Project: "site", Service: "web", WorkingDir: "/srv/site", ConfigFiles: []string{"/srv/site/docker-compose.yml"}},
				Mounts:        []protocol.DockerMount{{Type: "bind", Source: "/srv/site", Destination: "/usr/share/nginx/html", RW: true}},
				ResolvedPaths: []string{"/srv/site"},
				Env:           []string{"MYSQL_PASSWORD=secret"},
				Warnings:      []string{"compose file missing"},
			}},
			Warnings: []string{"docker warning"},
		},
		Database: &protocol.DatabaseBackupMetadata{
			Dumps: []protocol.DatabaseDumpMetadata{{
				Engine:        protocol.DatabaseEngineMySQL,
				ExecutionMode: "docker",
				Database:      "app",
				ContainerName: "mysql",
				OutputPath:    dbPath,
				OutputName:    "mysql-app.sql.gz",
				Size:          1234,
				Compressed:    true,
				Warnings:      []string{"dump warning"},
			}},
			Warnings: []string{"database warning"},
		},
	})

	require.NotNil(t, doc)
	assert.Equal(t, protocol.BackupContentManifestVersion, doc.Version)
	assert.Equal(t, generatedAt, doc.GeneratedAt)
	assert.Equal(t, "agent-1", doc.Agent.ID)
	assert.Equal(t, "1.2.3", doc.Agent.Version)
	assert.Equal(t, "s3", doc.Policy.StorageType)
	assert.Equal(t, "tenant/agent", doc.Policy.Repository)
	assert.Equal(t, []string{"*.log"}, doc.ExcludePatterns)

	require.Len(t, doc.Sources.Paths, 2)
	assert.Equal(t, "/srv/site", doc.Sources.Paths[0].Path)
	assert.Equal(t, "path", doc.Sources.Paths[0].Kind)
	assert.Equal(t, dbPath, doc.Sources.Paths[1].Path)
	assert.Equal(t, "database_dump", doc.Sources.Paths[1].Kind)

	require.Len(t, doc.Sources.Docker, 1)
	assert.Equal(t, "container-1", doc.Sources.Docker[0].ContainerID)
	assert.Equal(t, "site", doc.Sources.Docker[0].ComposeProject)
	assert.Equal(t, []string{"/srv/site/docker-compose.yml"}, doc.Sources.Docker[0].ComposeConfigFiles)
	require.Len(t, doc.Sources.Docker[0].Mounts, 1)
	assert.Equal(t, "/usr/share/nginx/html", doc.Sources.Docker[0].Mounts[0].Destination)

	require.Len(t, doc.Sources.Databases, 1)
	assert.Equal(t, protocol.DatabaseEngineMySQL, doc.Sources.Databases[0].Engine)
	assert.Equal(t, "mysql-app.sql.gz", doc.Sources.Databases[0].OutputName)
	assert.Equal(t, int64(1234), doc.Sources.Databases[0].Size)
	assert.True(t, doc.Sources.Databases[0].Compressed)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, "MYSQL_PASSWORD")
	assert.NotContains(t, text, "restic-secret")
	assert.NotContains(t, text, "access_key_id")
	assert.NotContains(t, text, "s3-no-check-bucket")
	assert.Contains(t, text, "docker warning")
	assert.Contains(t, text, "database warning")
}

func TestBuildMarksManifestPathSource(t *testing.T) {
	doc := Build(BuildInput{
		BackupDirs: []string{"/tmp/stage/" + protocol.BackupContentManifestName},
	})

	require.Len(t, doc.Sources.Paths, 1)
	assert.Equal(t, "manifest", doc.Sources.Paths[0].Kind)
	assert.Equal(t, "vaultfleet", doc.Sources.Paths[0].Origin)
}
