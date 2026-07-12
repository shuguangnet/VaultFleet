package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRoutesServeInstallerAndAgentBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	downloadsDir := filepath.Join(dataDir, "downloads", "v1.2.3")
	require.NoError(t, os.MkdirAll(downloadsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "agent-linux-amd64"), []byte("agent-binary"), 0o644))

	router := gin.New()
	RegisterDownloadRoutes(router, dataDir, "v1.2.3", "shuguangnet/VaultFleet")

	w := getJSON(t, router, "/install.sh")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/x-shellscript")
	assert.Contains(t, w.Body.String(), "VaultFleet Agent")
	assert.Contains(t, w.Body.String(), "RESTIC_VERSION")
	assert.Contains(t, w.Body.String(), "RCLONE_VERSION")
	assert.Contains(t, w.Body.String(), `restic_${RESTIC_VERSION}_${OS}_${ARCH}.bz2`)
	assert.Contains(t, w.Body.String(), `rclone-v${RCLONE_VERSION}-${OS}-${ARCH}.zip`)

	w = getJSON(t, router, "/download/agent-linux-amd64")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "agent-binary", w.Body.String())
}

func TestServedInstallerMatchesBuildInstaller(t *testing.T) {
	buildScript, err := os.ReadFile("../../../build/install.sh")
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSpace(string(buildScript)), strings.TrimSpace(agentInstallScript))
}

func TestDownloadRoutesFetchesAgentFromReleaseWhenLocalMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	original := agentReleaseBaseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent-linux-amd64" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/vaultfleet-agent-linux-amd64" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("downloaded-agent"))
	}))
	defer server.Close()
	agentReleaseBaseURL = server.URL
	defer func() { agentReleaseBaseURL = original }()

	router := gin.New()
	RegisterDownloadRoutes(router, dataDir, "v1.2.3", "shuguangnet/VaultFleet")

	w := getJSON(t, router, "/download/agent-linux-amd64")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "downloaded-agent", w.Body.String())

	cached, err := os.ReadFile(filepath.Join(dataDir, "downloads", "v1.2.3", "agent-linux-amd64"))
	require.NoError(t, err)
	assert.Equal(t, "downloaded-agent", string(cached))
}

func TestDownloadRoutesRefreshesLatestCacheWhenVersionIsNotReleaseTag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	original := agentReleaseBaseURL
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vaultfleet-agent-linux-amd64" {
			http.NotFound(w, r)
			return
		}
		requests++
		_, _ = w.Write([]byte("downloaded-agent-v2"))
	}))
	defer server.Close()
	agentReleaseBaseURL = server.URL
	defer func() { agentReleaseBaseURL = original }()

	latestPath := filepath.Join(dataDir, "downloads", "latest", "agent-linux-amd64")
	require.NoError(t, os.MkdirAll(filepath.Dir(latestPath), 0o755))
	require.NoError(t, os.WriteFile(latestPath, []byte("downloaded-agent-v1"), 0o755))

	router := gin.New()
	RegisterDownloadRoutes(router, dataDir, "dev", "shuguangnet/VaultFleet")

	w := getJSON(t, router, "/download/agent-linux-amd64")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "downloaded-agent-v2", w.Body.String())
	assert.Equal(t, 1, requests)
}

func TestDevelopmentBuildDownloadsAgentLatestRelease(t *testing.T) {
	source := newAgentDownloadSource("c7694e9bd0c4110825e2a050dc78a7c5caf76c81", "shuguangnet/VaultFleet")
	assert.Equal(t, "https://github.com/shuguangnet/VaultFleet/releases/download/agent-latest", source.baseURL)
	assert.Equal(t, "latest", source.cacheKey)
}

func TestDownloadRoutesRejectMissingOrUnsafeAgentPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterDownloadRoutes(router, t.TempDir(), "v1.2.3", "shuguangnet/VaultFleet")

	for _, path := range []string{"/download/agent-linux-sparc", "/download/../master.key", "/download/not-agent"} {
		t.Run(path, func(t *testing.T) {
			w := getJSON(t, router, path)
			require.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}
