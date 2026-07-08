package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			"abc123": composeInspectFixture("abc123", "/db", "postgres:16", "running", []Mount{{Type: "bind", Source: sourcePath, Destination: "/data", RW: true}}),
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
	require.Len(t, metadata.Sources[0].Mounts, 1)
	assert.Equal(t, sourcePath, metadata.Sources[0].Mounts[0].Source)
	assert.Equal(t, "/data", metadata.Sources[0].Mounts[0].Destination)
	assert.Equal(t, "app", metadata.Sources[0].Compose.Project)
	assert.Equal(t, []string{"POSTGRES_DB=app"}, metadata.Sources[0].Env)
	assert.Equal(t, []string{"postgres"}, metadata.Sources[0].Cmd)
	assert.Equal(t, "unless-stopped", metadata.Sources[0].RestartPolicy)
	require.Len(t, metadata.Sources[0].Ports, 1)
	assert.Equal(t, "5432", metadata.Sources[0].Ports[0].ContainerPort)
	assert.Equal(t, "15432", metadata.Sources[0].Ports[0].HostPort)
}

func TestRestoreDockerSourceUsesComposeWhenMetadataAvailable(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  db:\n    image: postgres:16\n"), 0o644))

	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:    "db",
				Compose: protocol.DockerComposeInfo{Project: "app", Service: "db", WorkingDir: dir, ConfigFiles: []string{"compose.yml"}},
			},
		},
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	})

	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "docker compose -f "+composePath+" up -d db", calls[0])
}

func TestRestoreDockerSourceFallsBackWhenComposeFileMissing(t *testing.T) {
	dir := t.TempDir()
	missingComposePath := filepath.Join(dir, "compose.yml")

	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:    "db",
				Image:   "postgres:16",
				Compose: protocol.DockerComposeInfo{Project: "app", Service: "db", WorkingDir: dir, ConfigFiles: []string{"compose.yml"}},
				Mounts: []protocol.DockerMount{
					{Type: "bind", Source: "/srv/db", Destination: "/var/lib/postgresql/data", RW: true},
				},
			},
		},
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(calls) == 1 {
			return []byte("not found"), errors.New("start failed")
		}
		return nil, nil
	})

	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "docker start db", calls[0])
	assert.Equal(t, "docker run -d --name db -v /srv/db:/var/lib/postgresql/data postgres:16", calls[1])
	assert.NoFileExists(t, missingComposePath)
}

func TestRestoreDockerSourceRunsContainerWithRecordedMounts(t *testing.T) {
	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:  "db",
				Image: "postgres:16",
				Labels: map[string]string{
					"app": "vaultfleet",
				},
				Mounts: []protocol.DockerMount{
					{Type: "bind", Source: "/srv/db", Destination: "/var/lib/postgresql/data", RW: true},
					{Type: "bind", Source: "/srv/config", Destination: "/config", RW: false},
				},
				Env:           []string{"POSTGRES_DB=app"},
				Cmd:           []string{"postgres", "-c", "config_file=/config/postgresql.conf"},
				WorkingDir:    "/var/lib/postgresql",
				User:          "999:999",
				Ports:         []protocol.DockerPortBinding{{ContainerPort: "5432", Protocol: "tcp", HostPort: "15432"}},
				RestartPolicy: "unless-stopped",
			},
		},
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(calls) == 1 {
			return []byte("not found"), errors.New("start failed")
		}
		return nil, nil
	})

	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "docker start db", calls[0])
	assert.Equal(t, "docker run -d --name db -v /srv/db:/var/lib/postgresql/data -v /srv/config:/config:ro -e POSTGRES_DB=app --label app=vaultfleet -p 15432:5432 --restart unless-stopped -w /var/lib/postgresql -u 999:999 postgres:16 postgres -c config_file=/config/postgresql.conf", calls[1])
}

func TestPreflightRestoreReportsMissingSources(t *testing.T) {
	checks := PreflightRestore(context.Background(), fakeDockerAPI{}, protocol.DockerRestoreRequest{})

	assertDockerPreflightCheck(t, checks, "docker_metadata", protocol.RestorePreflightSeverityError)
}

func TestPreflightRestoreReportsDockerUnavailable(t *testing.T) {
	checks := PreflightRestore(context.Background(), fakeDockerAPI{pingErr: errors.New("permission denied")}, protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{{Name: "db", Image: "postgres:16", ResolvedPaths: []string{"/srv/db"}}},
	})

	assertDockerPreflightCheck(t, checks, "docker_available", protocol.RestorePreflightSeverityError)
}

func TestPreflightRestoreReportsConflictAndPathWarnings(t *testing.T) {
	restorePath := t.TempDir()
	checks := PreflightRestore(context.Background(), fakeDockerAPI{
		containers: []ContainerSummary{
			{ID: "abc123", Names: []string{"/db"}, Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "db"}},
		},
	}, protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:          "db",
				Image:         "postgres:16",
				Compose:       protocol.DockerComposeInfo{Project: "app", Service: "db"},
				ResolvedPaths: []string{restorePath, filepath.Join(restorePath, "missing-child")},
			},
		},
	})

	assertDockerPreflightCheck(t, checks, "docker_available", protocol.RestorePreflightSeverityInfo)
	assertDockerPreflightCheck(t, checks, "docker_container_conflict", protocol.RestorePreflightSeverityWarning)
	assertDockerPreflightCheck(t, checks, "docker_restore_path_exists", protocol.RestorePreflightSeverityWarning)
	assertDockerPreflightCheck(t, checks, "docker_restore_path_missing", protocol.RestorePreflightSeverityWarning)
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

func assertDockerPreflightCheck(t *testing.T, checks []protocol.RestorePreflightCheck, code string, severity string) {
	t.Helper()
	for _, check := range checks {
		if check.Code == code && check.Severity == severity {
			return
		}
	}
	t.Fatalf("preflight check %s with severity %s not found in %#v", code, severity, checks)
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

func composeInspectFixture(id string, name string, image string, state string, mounts []Mount) ContainerInspect {
	inspect := inspectFixture(id, name, image, state, mounts)
	inspect.Config.Labels = map[string]string{
		"com.docker.compose.project":              "app",
		"com.docker.compose.service":              "db",
		"com.docker.compose.project.working_dir":  "/srv/app",
		"com.docker.compose.project.config_files": "compose.yml",
	}
	inspect.Config.Env = []string{"POSTGRES_DB=app"}
	inspect.Config.Cmd = []string{"postgres"}
	inspect.HostConfig.RestartPolicy.Name = "unless-stopped"
	inspect.HostConfig.PortBindings = map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}{
		"5432/tcp": {{HostPort: "15432"}},
	}
	return inspect
}
