package artifactnaming

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestRenderLegacyDefaults(t *testing.T) {
	result, err := Render(RenderInput{
		Context: Context{
			AgentID:       "agent-1",
			ArchiveFormat: protocol.ArchiveFormatZip,
			Now:           time.Date(2026, 7, 9, 2, 15, 0, 0, time.UTC),
			Sources:       []protocol.BackupSource{{Type: protocol.BackupSourceTypePath, Path: "/srv/site-a"}},
		},
	})

	require.NoError(t, err)
	assert.True(t, result.Legacy)
	assert.Equal(t, "site-a", result.ContextName)
	assert.Equal(t, "path", result.SourceType)
	assert.Equal(t, "artifacts", result.RemoteDir)
	assert.Equal(t, "backup-20260709-021500.zip", result.ArtifactName)
	assert.Equal(t, "artifacts/backup-20260709-021500.zip", result.ArtifactPath)
}

func TestRenderRecommendedDockerVariables(t *testing.T) {
	result, err := Render(RenderInput{
		Context: Context{
			AgentName:     "node hk",
			ArchiveFormat: protocol.ArchiveFormatTarGz,
			Now:           time.Date(2026, 7, 9, 2, 15, 0, 0, time.UTC),
			Sources: []protocol.BackupSource{{
				Type: protocol.BackupSourceTypeDockerContainer,
				DockerContainer: &protocol.DockerContainerBackupSource{
					Name:           "web/app",
					ComposeProject: "cliproxyapi",
					ComposeService: "api",
				},
			}},
		},
		UseRecommendedDefaults: true,
	})

	require.NoError(t, err)
	assert.False(t, result.Legacy)
	assert.Equal(t, "cliproxyapi", result.ContextName)
	assert.Equal(t, "docker", result.SourceType)
	assert.Equal(t, "archives/node_hk/cliproxyapi/2026-07-09", result.RemoteDir)
	assert.Equal(t, "cliproxyapi_node_hk_20260709-021500.tar.gz", result.ArtifactName)
}

func TestRenderRejectsUnsafeTemplates(t *testing.T) {
	_, err := Render(RenderInput{
		Context:           Context{AgentID: "agent-1", Now: time.Date(2026, 7, 9, 2, 15, 0, 0, time.UTC)},
		RemoteDirTemplate: "../archives",
		NameTemplate:      "backup.zip",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe segment")

	_, err = Render(RenderInput{
		Context:           Context{AgentID: "agent-1", Now: time.Date(2026, 7, 9, 2, 15, 0, 0, time.UTC)},
		RemoteDirTemplate: "archives",
		NameTemplate:      "{{hostname}}.zip",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported artifact naming variable")
}

func TestRenderWarnsForCollisionProneFilename(t *testing.T) {
	result, err := Render(RenderInput{
		Context:           Context{AgentID: "agent-1", Now: time.Date(2026, 7, 9, 2, 15, 0, 0, time.UTC)},
		RemoteDirTemplate: "archives",
		NameTemplate:      "latest.zip",
	})

	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "possible_collision", result.Warnings[0].Code)
}
