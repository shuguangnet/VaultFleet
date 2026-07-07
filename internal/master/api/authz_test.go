package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermissionForTaskAndCommandLogs(t *testing.T) {
	assert.Equal(t, PermissionReadOperational, permissionForRoute(http.MethodGet, "/api/tasks/:id/logs"))
	assert.Equal(t, PermissionReadOperational, permissionForRoute(http.MethodGet, "/api/commands/:id/logs"))
}
