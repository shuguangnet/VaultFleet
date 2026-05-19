package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const frontendPlaceholderHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>VaultFleet</title>
</head>
<body>
  <main>
    <h1>VaultFleet</h1>
    <p>VaultFleet master console placeholder.</p>
  </main>
</body>
</html>`

func RegisterFrontendRoutes(r *gin.Engine) {
	r.NoRoute(func(c *gin.Context) {
		if isBackendRoute(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(frontendPlaceholderHTML))
	})
}

func isBackendRoute(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") ||
		path == "/ws" || strings.HasPrefix(path, "/ws/")
}
