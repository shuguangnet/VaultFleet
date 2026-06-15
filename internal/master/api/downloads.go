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

var agentReleaseBaseURL = "https://github.com/shuguangnet/VaultFleet/releases/download/agent-latest"

var agentReleaseCandidates = map[string][]string{
	"agent-linux-amd64": {"agent-linux-amd64", "vaultfleet-agent-linux-amd64"},
	"agent-linux-arm64": {"agent-linux-arm64", "vaultfleet-agent-linux-arm64"},
}

func RegisterDownloadRoutes(r *gin.Engine, dataDir string) {
	r.GET("/install.sh", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(agentInstallScript))
	})
	r.GET("/download/:name", func(c *gin.Context) {
		name := c.Param("name")
		if !allowedAgentDownloadName(name) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}
		path, err := ensureAgentDownload(dataDir, name)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}
		c.File(path)
	})
}

func ensureAgentDownload(dataDir, name string) (string, error) {
	path := filepath.Join(dataDir, "downloads", name)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	for _, releaseName := range releaseNamesForAgent(name) {
		if err := downloadAgentReleaseAsset(path, releaseName); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("download %s: no matching release asset", name)
}

func downloadAgentReleaseAsset(path, releaseName string) error {
	url := fmt.Sprintf("%s/%s", agentReleaseBaseURL, releaseName)
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

//go:embed assets/install.sh
var agentInstallScript string
