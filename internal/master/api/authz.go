package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"

	ActorTypeUser     = "user"
	ActorTypeAPIToken = "api_token"

	PermissionReadOperational    = "read:operational"
	PermissionWriteNodes         = "write:nodes"
	PermissionWriteStorage       = "write:storage"
	PermissionWritePolicies      = "write:policies"
	PermissionRunBackup          = "run:backup"
	PermissionRunRestore         = "run:restore"
	PermissionWriteNotifications = "write:notifications"
	PermissionReadSystem         = "read:system"
	PermissionAdminSystem        = "admin:system"
	PermissionAdminUsers         = "admin:users"
	PermissionAdminTokens        = "admin:tokens"
	PermissionReadAudit          = "read:audit"
)

type Actor struct {
	Type        string   `json:"actor_type"`
	UserID      string   `json:"user_id,omitempty"`
	Username    string   `json:"username,omitempty"`
	Role        string   `json:"role"`
	TokenID     string   `json:"token_id,omitempty"`
	TokenName   string   `json:"token_name,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Permissions []string `json:"permissions"`
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleAdmin:
		return RoleAdmin
	case RoleOperator:
		return RoleOperator
	case RoleViewer:
		return RoleViewer
	default:
		return ""
	}
}

func validRole(role string) bool {
	return normalizeRole(role) != ""
}

func rolePermissions(role string) map[string]bool {
	switch normalizeRole(role) {
	case RoleAdmin:
		return permissionsSet(
			PermissionReadOperational,
			PermissionWriteNodes,
			PermissionWriteStorage,
			PermissionWritePolicies,
			PermissionRunBackup,
			PermissionRunRestore,
			PermissionWriteNotifications,
			PermissionReadSystem,
			PermissionAdminSystem,
			PermissionAdminUsers,
			PermissionAdminTokens,
			PermissionReadAudit,
		)
	case RoleOperator:
		return permissionsSet(
			PermissionReadOperational,
			PermissionWriteNodes,
			PermissionWriteStorage,
			PermissionWritePolicies,
			PermissionRunBackup,
			PermissionRunRestore,
			PermissionWriteNotifications,
			PermissionReadSystem,
			PermissionReadAudit,
		)
	case RoleViewer:
		return permissionsSet(
			PermissionReadOperational,
			PermissionReadSystem,
			PermissionReadAudit,
		)
	default:
		return map[string]bool{}
	}
}

func effectivePermissions(role string, scopes []string, tokenActor bool) []string {
	roleSet := rolePermissions(role)
	effective := make([]string, 0, len(roleSet))
	if tokenActor {
		scopeSet := permissionsSet(scopes...)
		for permission := range roleSet {
			if scopeSet[permission] {
				effective = append(effective, permission)
			}
		}
		return effective
	}
	for permission := range roleSet {
		effective = append(effective, permission)
	}
	return effective
}

func actorHasPermission(actor *Actor, permission string) bool {
	if actor == nil || permission == "" {
		return false
	}
	for _, candidate := range actor.Permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

func setActor(c *gin.Context, actor Actor) {
	copy := actor
	c.Set("actor", &copy)
	c.Set("actor_type", actor.Type)
	c.Set("user_id", actor.UserID)
	c.Set("username", actor.Username)
	c.Set("role", actor.Role)
	if actor.TokenID != "" {
		c.Set("token_id", actor.TokenID)
	}
}

func currentActor(c *gin.Context) *Actor {
	if value, ok := c.Get("actor"); ok {
		if actor, ok := value.(*Actor); ok {
			return actor
		}
	}
	role := c.GetString("role")
	if role == "" {
		return nil
	}
	return &Actor{
		Type:        c.GetString("actor_type"),
		UserID:      c.GetString("user_id"),
		Username:    c.GetString("username"),
		Role:        role,
		TokenID:     c.GetString("token_id"),
		Permissions: effectivePermissions(role, nil, false),
	}
}

func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := currentActor(c)
		if !actorHasPermission(actor, permission) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error": "forbidden"})
			return
		}
		c.Next()
	}
}

func AuthorizeByRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		permission := permissionForRoute(c.Request.Method, c.FullPath())
		if permission == "" {
			c.Next()
			return
		}
		RequirePermission(permission)(c)
	}
}

func permissionForRoute(method string, path string) string {
	switch {
	case strings.HasPrefix(path, "/api/users"):
		return PermissionAdminUsers
	case strings.HasPrefix(path, "/api/api-tokens"):
		return PermissionAdminTokens
	case strings.HasPrefix(path, "/api/audit-events"):
		return PermissionReadAudit
	case strings.HasPrefix(path, "/api/system/import"), path == "/api/system/export":
		return PermissionAdminSystem
	case strings.HasPrefix(path, "/api/system/diagnostics"):
		return PermissionRunBackup
	case path == "/api/system/password":
		return PermissionReadSystem
	case strings.HasPrefix(path, "/api/system"):
		return PermissionReadSystem
	case strings.Contains(path, "/restore") || strings.Contains(path, "restore"):
		if method == http.MethodGet {
			return PermissionReadOperational
		}
		return PermissionRunRestore
	case strings.Contains(path, "backup-now") || strings.Contains(path, "verify-now") || strings.Contains(path, "snapshots/refresh") || strings.Contains(path, "cancel"):
		return PermissionRunBackup
	case strings.HasPrefix(path, "/api/storage"):
		if method == http.MethodGet {
			return PermissionReadOperational
		}
		return PermissionWriteStorage
	case strings.HasPrefix(path, "/api/policies"):
		if method == http.MethodGet {
			return PermissionReadOperational
		}
		return PermissionWritePolicies
	case strings.HasPrefix(path, "/api/notifications"):
		if method == http.MethodGet {
			return PermissionReadOperational
		}
		return PermissionWriteNotifications
	case strings.HasPrefix(path, "/api/agents"):
		if method == http.MethodGet {
			if strings.Contains(path, "install-token") || strings.Contains(path, "commands") {
				return PermissionWriteNodes
			}
			return PermissionReadOperational
		}
		if strings.Contains(path, "commands") {
			return PermissionReadOperational
		}
		return PermissionWriteNodes
	case strings.HasPrefix(path, "/api/tasks"):
		if strings.Contains(path, "download") {
			return PermissionRunRestore
		}
		return PermissionReadOperational
	case strings.HasPrefix(path, "/api/commands"), strings.HasPrefix(path, "/api/snapshots"):
		return PermissionReadOperational
	default:
		if method == http.MethodGet {
			return PermissionReadOperational
		}
		return ""
	}
}

func permissionsSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}
