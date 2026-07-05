package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

type fakeDockerAPI struct {
	pingErr    error
	listErr    error
	containers []ContainerSummary
	inspects   map[string]ContainerInspect
}

func (f fakeDockerAPI) Ping(context.Context) error {
	return f.pingErr
}

func (f fakeDockerAPI) ListContainers(context.Context) ([]ContainerSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.containers, nil
}

func (f fakeDockerAPI) InspectContainer(_ context.Context, id string) (ContainerInspect, error) {
	inspect, ok := f.inspects[id]
	if !ok {
		return ContainerInspect{}, errors.New("missing inspect")
	}
	return inspect, nil
}

func TestDiscoverUnavailable(t *testing.T) {
	resp := Discover(context.Background(), fakeDockerAPI{pingErr: errors.New("permission denied")})
	assert.False(t, resp.Available)
	assert.Contains(t, resp.Error, "permission denied")
	assert.Empty(t, resp.Containers)
}

func TestDiscoverSuccessReturnsContainerMetadata(t *testing.T) {
	resp := Discover(context.Background(), fakeDockerAPI{
		containers: []ContainerSummary{
			{
				ID:    "abc123",
				Names: []string{"/db"},
				Image: "postgres:16",
				State: "running",
				Labels: map[string]string{
					"com.docker.compose.project":              "app",
					"com.docker.compose.service":              "db",
					"com.docker.compose.project.working_dir":  "/srv/app",
					"com.docker.compose.project.config_files": "compose.yml",
				},
				Mounts: []Mount{{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db-data/_data", Destination: "/var/lib/postgresql/data", RW: true}},
			},
		},
	})
	require.True(t, resp.Available)
	require.Len(t, resp.Containers, 1)
	assert.Equal(t, "abc123", resp.Containers[0].ID)
	assert.Equal(t, []string{"db"}, resp.Containers[0].Names)
	assert.Equal(t, "app", resp.Containers[0].Compose.Project)
	assert.True(t, resp.Containers[0].Selectable)
}

func TestResolveDockerSources(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "data")
	require.NoError(t, os.MkdirAll(sourcePath, 0o755))
	api := fakeDockerAPI{
		containers: []ContainerSummary{
			{ID: "abc123", Names: []string{"/db"}, Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "db"}},
		},
		inspects: map[string]ContainerInspect{
			"abc123": inspectFixture("abc123", "/db", "postgres:16", "running", []Mount{{Type: "bind", Source: sourcePath, Destination: "/data", RW: true}}),
		},
	}

	paths, metadata, err := Resolve(context.Background(), api, []protocol.BackupSource{
		{
			Type: protocol.BackupSourceTypeDockerContainer,
			DockerContainer: &protocol.DockerContainerBackupSource{
				ComposeProject:    "app",
				ComposeService:    "db",
				IncludeBindMounts: true,
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{sourcePath}, paths)
	require.NotNil(t, metadata)
	require.Len(t, metadata.Sources, 1)
	assert.Equal(t, "abc123", metadata.Sources[0].ContainerID)
	assert.Equal(t, []string{sourcePath}, metadata.Sources[0].ResolvedPaths)
}

func TestResolveAmbiguousComposeSelection(t *testing.T) {
	api := fakeDockerAPI{
		containers: []ContainerSummary{
			{ID: "one", Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "web"}},
			{ID: "two", Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "web"}},
		},
	}

	_, _, err := Resolve(context.Background(), api, []protocol.BackupSource{dockerSource("app", "web")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestResolveMissingContainer(t *testing.T) {
	_, _, err := Resolve(context.Background(), fakeDockerAPI{}, []protocol.BackupSource{dockerSource("app", "db")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveUnreadableMount(t *testing.T) {
	api := fakeDockerAPI{
		containers: []ContainerSummary{{ID: "abc123", Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "db"}}},
		inspects: map[string]ContainerInspect{
			"abc123": inspectFixture("abc123", "/db", "postgres:16", "running", []Mount{{Type: "bind", Source: "/path/that/does/not/exist", Destination: "/data", RW: true}}),
		},
	}

	_, _, err := Resolve(context.Background(), api, []protocol.BackupSource{dockerSource("app", "db")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreadable")
}

func dockerSource(project string, service string) protocol.BackupSource {
	return protocol.BackupSource{
		Type: protocol.BackupSourceTypeDockerContainer,
		DockerContainer: &protocol.DockerContainerBackupSource{
			ComposeProject:    project,
			ComposeService:    service,
			IncludeBindMounts: true,
			IncludeVolumes:    true,
		},
	}
}

func inspectFixture(id string, name string, image string, state string, mounts []Mount) ContainerInspect {
	inspect := ContainerInspect{ID: id, Name: name, Mounts: mounts}
	inspect.Config.Image = image
	inspect.State.Status = state
	return inspect
}
