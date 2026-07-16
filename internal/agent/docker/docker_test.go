package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestResolveDockerSourcesIncludesComposeEnvironmentFiles(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  app:\n    container_name: ${CONTAINER_NAME}\n"), 0o644))
	require.NoError(t, os.WriteFile(envPath, []byte("CONTAINER_NAME=test-app\nPRIVATE_TOKEN=must-not-enter-metadata\n"), 0o600))
	inspect := composeInspectFixture("abc123", "/app", "example:latest", "running", nil)
	inspect.Config.Labels["com.docker.compose.service"] = "app"
	inspect.Config.Labels["com.docker.compose.project.working_dir"] = dir
	inspect.Config.Labels["com.docker.compose.project.config_files"] = "docker-compose.yml"
	inspect.Config.Labels["com.docker.compose.project.environment_file"] = ".env"

	paths, metadata, err := Resolve(context.Background(), fakeDockerAPI{
		containers: []ContainerSummary{{ID: "abc123", Labels: map[string]string{
			"com.docker.compose.project": "app", "com.docker.compose.service": "app",
		}}},
		inspects: map[string]ContainerInspect{"abc123": inspect},
	}, []protocol.BackupSource{{
		Type: protocol.BackupSourceTypeDockerContainer,
		DockerContainer: &protocol.DockerContainerBackupSource{
			ComposeProject: "app", ComposeService: "app", IncludeComposeFiles: true,
		},
	}})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{composePath, envPath}, paths)
	require.NotNil(t, metadata)
	require.Len(t, metadata.Sources, 1)
	assert.Equal(t, []string{envPath}, metadata.Sources[0].Compose.EnvFiles)
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "must-not-enter-metadata")
}

func TestResolveNonDockerSourcesDoesNotCallDockerAPI(t *testing.T) {
	paths, metadata, err := Resolve(context.Background(), fakeDockerAPI{
		listErr: errors.New("docker must not be called"),
	}, []protocol.BackupSource{
		{Type: protocol.BackupSourceTypePath, Path: "/srv/data"},
		{
			Type: protocol.BackupSourceTypeDatabase,
			Database: &protocol.DatabaseBackupSource{
				Engine:        protocol.DatabaseEnginePostgreSQL,
				ExecutionMode: protocol.DatabaseExecutionHost,
			},
		},
	})

	require.NoError(t, err)
	assert.Empty(t, paths)
	assert.Nil(t, metadata)
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
	assert.Equal(t, "docker compose --project-directory "+dir+" -f "+composePath+" up -d db", calls[0])
}

func TestRestoreDockerSourceUsesComposeEnvironmentFile(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  uptime-kuma:\n    container_name: ${CONTAINER_NAME}\n"), 0o644))
	require.NoError(t, os.WriteFile(envPath, []byte("CONTAINER_NAME=uptime-kuma\n"), 0o600))

	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{{
		Name: "uptime-kuma",
		Compose: protocol.DockerComposeInfo{
			Project: "uptime-kuma", Service: "uptime-kuma", WorkingDir: dir,
			ConfigFiles: []string{"docker-compose.yml"}, EnvFiles: []string{envPath},
		},
	}}}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	})

	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "docker compose --project-directory "+dir+" --env-file "+envPath+" -f "+composePath+" up -d uptime-kuma", calls[0])
}

func TestRestoreDockerSourceRejectsMissingComposeEnvironmentFile(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  uptime-kuma:\n    container_name: ${CONTAINER_NAME}\n"), 0o644))

	called := false
	err := Restore(context.Background(), protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{{
		Name: "uptime-kuma",
		Compose: protocol.DockerComposeInfo{
			Project: "uptime-kuma", Service: "uptime-kuma", WorkingDir: dir, ConfigFiles: []string{"docker-compose.yml"},
		},
	}}}, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return nil, nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no readable environment file")
	assert.False(t, called)
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
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "docker start db", "docker container inspect db":
			return []byte("not found"), errors.New("start failed")
		default:
			return nil, nil
		}
	})

	require.NoError(t, err)
	require.Len(t, calls, 3)
	assert.Equal(t, "docker start db", calls[0])
	assert.Equal(t, "docker container inspect db", calls[1])
	assert.Equal(t, "docker run -d --name db -v /srv/db:/var/lib/postgresql/data postgres:16", calls[2])
	assert.NoFileExists(t, missingComposePath)
}

func TestRestoreDockerSourceRemovesExistingContainerBeforeRecreate(t *testing.T) {
	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:  "api",
				Image: "cliproxyapi:latest",
			},
		},
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "docker start api":
			return []byte("start failed"), errors.New("start failed")
		default:
			return nil, nil
		}
	})

	require.NoError(t, err)
	require.Len(t, calls, 4)
	assert.Equal(t, "docker start api", calls[0])
	assert.Equal(t, "docker container inspect api", calls[1])
	assert.Equal(t, "docker rm -f api", calls[2])
	assert.Equal(t, "docker run -d --name api cliproxyapi:latest", calls[3])
}

func TestRestoreDockerSourceCreatesMissingCustomNetwork(t *testing.T) {
	var calls []string
	err := Restore(context.Background(), protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{
			{
				Name:        "api",
				Image:       "cliproxyapi:latest",
				NetworkMode: "cliproxyapi_default",
			},
		},
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "docker start api":
			return []byte("not found"), errors.New("start failed")
		case "docker network inspect cliproxyapi_default":
			return []byte("not found"), errors.New("network not found")
		case "docker container inspect api":
			return []byte("not found"), errors.New("container not found")
		default:
			return nil, nil
		}
	})

	require.NoError(t, err)
	require.Len(t, calls, 5)
	assert.Equal(t, "docker network inspect cliproxyapi_default", calls[0])
	assert.Equal(t, "docker network create cliproxyapi_default", calls[1])
	assert.Equal(t, "docker start api", calls[2])
	assert.Equal(t, "docker container inspect api", calls[3])
	assert.Equal(t, "docker run -d --name api --network cliproxyapi_default cliproxyapi:latest", calls[4])
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
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "docker start db", "docker container inspect db":
			return []byte("not found"), errors.New("start failed")
		default:
			return nil, nil
		}
	})

	require.NoError(t, err)
	require.Len(t, calls, 3)
	assert.Equal(t, "docker start db", calls[0])
	assert.Equal(t, "docker container inspect db", calls[1])
	assert.Equal(t, "docker run -d --name db -v /srv/db:/var/lib/postgresql/data -v /srv/config:/config:ro -e POSTGRES_DB=app --label app=vaultfleet -p 15432:5432 --restart unless-stopped -w /var/lib/postgresql -u 999:999 postgres:16 postgres -c config_file=/config/postgresql.conf", calls[2])
}

func TestRestoreBatchContinuesAfterSourceFailure(t *testing.T) {
	var starts []string
	results := RestoreBatch(context.Background(), protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{
		{ContainerID: "first-id", Name: "first"},
		{ContainerID: "second-id", Name: "second", Image: "second:latest"},
	}}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(call, "docker start ") {
			starts = append(starts, strings.TrimPrefix(call, "docker start "))
			if strings.HasSuffix(call, "first") {
				return nil, errors.New("not found")
			}
			return nil, nil
		}
		return nil, errors.New("not found")
	}, nil)

	require.Len(t, results, 2)
	assert.Equal(t, protocol.RestoreItemStatusFailed, results[0].Status)
	assert.True(t, results[0].Retryable)
	assert.Equal(t, protocol.RestoreItemStatusSuccess, results[1].Status)
	assert.Equal(t, []string{"first", "second"}, starts)
}

func TestRestoreBatchStopsBeforeNextSourceWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var starts []string
	results := RestoreBatch(ctx, protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{
		{ContainerID: "first-id", Name: "first", Image: "first:latest"},
		{ContainerID: "second-id", Name: "second", Image: "second:latest"},
	}}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if call == "docker start first" {
			starts = append(starts, "first")
			cancel()
			return nil, nil
		}
		if strings.HasPrefix(call, "docker start ") {
			starts = append(starts, strings.TrimPrefix(call, "docker start "))
		}
		return nil, nil
	}, nil)

	require.Len(t, results, 2)
	assert.Equal(t, protocol.RestoreItemStatusSuccess, results[0].Status)
	assert.Equal(t, protocol.RestoreItemStatusSkipped, results[1].Status)
	assert.Equal(t, []string{"first"}, starts)
}

func TestRestoreBatchReportsStableProgress(t *testing.T) {
	var events []string
	results := RestoreBatch(context.Background(), protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{
		{ContainerID: "first-id", Name: "first", Image: "first:latest"},
		{ContainerID: "second-id", Name: "second", Image: "second:latest"},
	}}, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}, func(source protocol.DockerResolvedSource, completed int, failed int) {
		events = append(events, fmt.Sprintf("%s:%d:%d", protocol.DockerSourceID(source), completed, failed))
	})

	require.Len(t, results, 2)
	assert.Equal(t, []string{"first-id:0:0", "first-id:1:0", "second-id:1:0", "second-id:2:0"}, events)
}

func TestPreflightRestoreAttributesChecksToSource(t *testing.T) {
	path := t.TempDir()
	checks := PreflightRestore(context.Background(), fakeDockerAPI{}, protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{{
		ContainerID: "db-id", Name: "db", Image: "postgres:16", ResolvedPaths: []string{path},
	}}})

	var attributed bool
	for _, check := range checks {
		if check.Code == "docker_restore_path_exists" {
			attributed = check.SourceID == "db-id" && check.SourceName == "db"
		}
	}
	assert.True(t, attributed, "expected source-attributed restore path check: %#v", checks)
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

func TestPreflightRestoreBlocksComposeVariablesWithoutEnvironmentFile(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  uptime-kuma:\n    container_name: ${CONTAINER_NAME}\n"), 0o644))

	checks := PreflightRestore(context.Background(), fakeDockerAPI{}, protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{{
			ContainerID: "uptime-kuma-id", Name: "uptime-kuma", ResolvedPaths: []string{composePath},
			Compose: protocol.DockerComposeInfo{
				Project: "uptime-kuma", Service: "uptime-kuma", WorkingDir: dir, ConfigFiles: []string{"docker-compose.yml"},
			},
		}},
	})

	assertDockerPreflightCheck(t, checks, "docker_compose_environment", protocol.RestorePreflightSeverityError)
}

func TestPreflightRestoreAllowsEnvironmentFilePlannedForRestore(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(composePath, []byte("services:\n  uptime-kuma:\n    container_name: ${CONTAINER_NAME}\n"), 0o644))

	checks := PreflightRestore(context.Background(), fakeDockerAPI{}, protocol.DockerRestoreRequest{
		Sources: []protocol.DockerResolvedSource{{
			ContainerID: "uptime-kuma-id", Name: "uptime-kuma", ResolvedPaths: []string{composePath, envPath},
			Compose: protocol.DockerComposeInfo{
				Project: "uptime-kuma", Service: "uptime-kuma", WorkingDir: dir,
				ConfigFiles: []string{"docker-compose.yml"}, EnvFiles: []string{envPath},
			},
		}},
	})

	for _, check := range checks {
		assert.NotEqual(t, "docker_compose_environment", check.Code, "unexpected environment error: %#v", check)
	}
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
