package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed frontend_dist/*
var frontendDist embed.FS

func RegisterFrontendRoutes(r *gin.Engine) {
	sub, err := fs.Sub(frontendDist, "frontend_dist")
	if err != nil {
		panic(err)
	}

	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// Serve assets
	// Built files are in frontend_dist/assets/*
	// HTML references them as /assets/*
	assets, err := fs.Sub(sub, "assets")
	if err == nil {
		r.StaticFS("/assets", http.FS(assets))
	}

	indexHTML, _ := fs.ReadFile(sub, "index.html")

	// Serve index.html for all other routes (SPA)
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if isBackendRoute(path) || !isFrontendRoute(path) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}

		if len(indexHTML) > 0 {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		} else {
			// Fallback if not built yet (mostly for tests if not built)
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html><html><body><div id=\"root\">云备份（未构建）</div></body></html>"))
		}
	})
}

func isBackendRoute(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") ||
		path == "/ws" || strings.HasPrefix(path, "/ws/") ||
		path == "/install.sh" || strings.HasPrefix(path, "/download/")
}

func isFrontendRoute(path string) bool {
	return path == "/" ||
		path == "/dashboard" ||
		path == "/nodes" ||
		strings.HasPrefix(path, "/nodes/") ||
		path == "/storage" ||
		path == "/policies" ||
		path == "/tasks" ||
		path == "/snapshots" ||
		path == "/notifications" ||
		path == "/system" ||
		path == "/login" ||
		path == "/setup"
}
