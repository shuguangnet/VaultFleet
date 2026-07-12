package api

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var agentReleaseBaseURL string

const defaultAgentGitHubRepo = "shuguangnet/VaultFleet"

var agentReleaseCandidates = map[string][]string{
	"agent-linux-amd64": {"agent-linux-amd64", "vaultfleet-agent-linux-amd64"},
	"agent-linux-arm64": {"agent-linux-arm64", "vaultfleet-agent-linux-arm64"},
}

type agentDownloadSource struct {
	baseURL  string
	cacheKey string
}

func RegisterDownloadRoutes(r *gin.Engine, dataDir string, version string, githubRepo string) {
	source := newAgentDownloadSource(version, githubRepo)
	r.GET("/install.sh", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(agentInstallScript))
	})
	r.GET("/download/:name", func(c *gin.Context) {
		name := c.Param("name")
		if !allowedAgentDownloadName(name) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}
		path, err := ensureAgentDownload(dataDir, name, source)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}
		c.File(path)
	})
}

func newAgentDownloadSource(version string, githubRepo string) agentDownloadSource {
	if agentReleaseBaseURL != "" {
		return agentDownloadSource{
			baseURL:  strings.TrimRight(agentReleaseBaseURL, "/"),
			cacheKey: releaseDownloadCacheKey(version),
		}
	}

	repo := strings.TrimSpace(githubRepo)
	if repo == "" {
		repo = defaultAgentGitHubRepo
	}
	releaseVersion := strings.TrimSpace(version)
	if isAgentReleaseVersion(releaseVersion) {
		return agentDownloadSource{
			baseURL:  fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, releaseVersion),
			cacheKey: releaseDownloadCacheKey(releaseVersion),
		}
	}
	return agentDownloadSource{
		baseURL:  fmt.Sprintf("https://github.com/%s/releases/download/agent-latest", repo),
		cacheKey: "latest",
	}
}

func ensureAgentDownload(dataDir, name string, source agentDownloadSource) (string, error) {
	path := filepath.Join(dataDir, "downloads", source.cacheKey, name)
	if source.cacheKey != "latest" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	for _, releaseName := range releaseNamesForAgent(name) {
		if err := downloadAgentReleaseAsset(source.baseURL, path, releaseName); err == nil {
			return path, nil
		}
	}
	if source.cacheKey == "latest" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("download %s: no matching release asset", name)
}

func downloadAgentReleaseAsset(baseURL, path, releaseName string) error {
	url := fmt.Sprintf("%s/%s", strings.TrimRight(baseURL, "/"), releaseName)
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: http %d", releaseName, resp.StatusCode)
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func releaseNamesForAgent(name string) []string {
	if names, ok := agentReleaseCandidates[name]; ok && len(names) > 0 {
		return names
	}
	return []string{name}
}

func allowedAgentDownloadName(name string) bool {
	return name == "agent-linux-amd64" || name == "agent-linux-arm64" ||
		strings.HasPrefix(name, "agent-linux-amd64.") ||
		strings.HasPrefix(name, "agent-linux-arm64.")
}

func isAgentReleaseVersion(version string) bool {
	return strings.HasPrefix(version, "v")
}

func releaseDownloadCacheKey(version string) string {
	version = strings.TrimSpace(version)
	if !isAgentReleaseVersion(version) {
		return "latest"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_")
	return replacer.Replace(version)
}

//go:embed assets/install.sh
var agentInstallScript string
