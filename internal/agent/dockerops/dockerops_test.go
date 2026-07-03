package dockerops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestDiscoverParsesDockerInspectAndRedactsSecrets(t *testing.T) {
	inspect := `[{
  "Id":"abcdef1234567890",
  "Name":"/web",
  "Config":{"Image":"nginx:latest","Env":["PASSWORD=secret","APP_ENV=prod"],"Labels":{"com.docker.compose.project":"shop","com.docker.compose.service":"web","api_token":"secret"}},
  "State":{"Status":"running"},
  "Mounts":[{"Type":"bind","Source":"/srv/shop","Destination":"/app","RW":true}],
  "NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]}}
}]`
	svc := &Service{Runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "version" {
			return []byte(`"25.0.0"`), nil
		}
		if args[0] == "ps" {
			return []byte("abcdef123456\n"), nil
		}
		return []byte(inspect), nil
	}}

	resp, err := svc.Discover(context.Background(), protocol.DockerDiscoverReqPayload{AgentID: "agent-1"})
	require.NoError(t, err)
	require.Len(t, resp.Containers, 1)
	container := resp.Containers[0]
	assert.Equal(t, "abcdef123456", container.ID)
	assert.Equal(t, "web", container.Name)
	assert.Equal(t, "[redacted]", container.Env["PASSWORD"])
	assert.Equal(t, "prod", container.Env["APP_ENV"])
	assert.Equal(t, "[redacted]", container.Labels["api_token"])
	assert.Equal(t, "/srv/shop", container.Mounts[0].Source)
	assert.Equal(t, 8080, container.Ports[0].PublicPort)
}

func TestDiscoverDockerUnavailable(t *testing.T) {
	svc := &Service{Runner: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}}

	_, err := svc.Discover(context.Background(), protocol.DockerDiscoverReqPayload{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker unavailable")
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	manifest := BuildManifest([]protocol.DockerContainer{{Name: "web", Mounts: []protocol.DockerMount{{Source: "/srv/web"}}}}, createdAt)
	_, err := WriteManifest(root, manifest)
	require.NoError(t, err)

	loaded, err := ReadManifest(root, "")
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Version)
	assert.Equal(t, "web", loaded.Containers[0].Name)

	data, err := os.ReadFile(filepath.Join(root, DefaultManifestPath))
	require.NoError(t, err)
	var decoded protocol.DockerManifest
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, createdAt, decoded.CreatedAt)
}

func TestPrecheckReportsDockerUnavailable(t *testing.T) {
	svc := &Service{Runner: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}}
	warnings := svc.Precheck(context.Background(), protocol.DockerManifest{Plan: protocol.DockerRestorePlan{Command: "docker compose up -d"}}, "")
	assert.NotEmpty(t, warnings)
}
