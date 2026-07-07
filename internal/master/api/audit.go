package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
)

const (
	AuditResultSuccess = "success"
	AuditResultFailure = "failure"
	AuditResultDenied  = "denied"
)

type AuditHandler struct {
	DB *db.Database
}

func NewAuditHandler(database *db.Database) *AuditHandler {
	return &AuditHandler{DB: database}
}

func RegisterAuditRoutes(rg *gin.RouterGroup, h *AuditHandler) {
	rg.GET("/audit-events", h.List)
}

type auditEventResponse = db.AuditEvent

func (h *AuditHandler) List(c *gin.Context) {
	query := h.DB.DB.Model(&db.AuditEvent{})
	for column, param := range map[string]string{
		"actor_id":    "actor_id",
		"actor_type":  "actor_type",
		"action":      "action",
		"target_type": "target_type",
		"target_id":   "target_id",
		"result":      "result",
	} {
		if value := strings.TrimSpace(c.Query(param)); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if from := strings.TrimSpace(c.Query("from")); from != "" {
		if parsed, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("created_at >= ?", parsed)
		}
	}
	if to := strings.TrimSpace(c.Query("to")); to != "" {
		if parsed, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("created_at <= ?", parsed)
		}
	}
	var events []db.AuditEvent
	if err := query.Order("created_at DESC").Limit(queryLimit(c, 100, 500)).Find(&events).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, events)
}

func RecordAudit(database *db.Database, c *gin.Context, action string, targetType string, targetID string, result string, message string) {
	if database == nil || database.DB == nil || strings.TrimSpace(action) == "" {
		return
	}
	actor := currentActor(c)
	event := db.AuditEvent{
		Action:     strings.TrimSpace(action),
		TargetType: strings.TrimSpace(targetType),
		TargetID:   strings.TrimSpace(targetID),
		Result:     strings.TrimSpace(result),
		Message:    redactAuditMessage(message),
	}
	if event.Result == "" {
		event.Result = AuditResultSuccess
	}
	if c != nil {
		event.IPAddress = c.ClientIP()
		event.UserAgent = c.Request.UserAgent()
	}
	if actor != nil {
		event.ActorType = actor.Type
		event.ActorID = actor.UserID
		event.ActorName = actor.Username
		event.ActorRole = actor.Role
		event.TokenID = actor.TokenID
	}
	_ = database.DB.Create(&event).Error
}

func AuditMiddleware(database *db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		action, targetType := auditActionForRoute(c.Request.Method, c.FullPath())
		c.Next()
		if action == "" {
			return
		}
		result := AuditResultSuccess
		if c.Writer.Status() == http.StatusForbidden {
			result = AuditResultDenied
		} else if c.Writer.Status() >= 400 {
			result = AuditResultFailure
		}
		RecordAudit(database, c, action, targetType, c.Param("id"), result, "")
	}
}

func auditActionForRoute(method string, path string) (string, string) {
	if method == http.MethodGet {
		return "", ""
	}
	switch {
	case strings.HasPrefix(path, "/api/users"):
		return "user." + routeVerb(method, path), "user"
	case strings.HasPrefix(path, "/api/api-tokens"):
		return "api_token." + routeVerb(method, path), "api_token"
	case strings.HasPrefix(path, "/api/storage"):
		return "storage." + routeVerb(method, path), "storage"
	case strings.HasPrefix(path, "/api/policies"):
		return "policy." + routeVerb(method, path), "policy"
	case strings.HasPrefix(path, "/api/notifications"):
		return "notification." + routeVerb(method, path), "notification"
	case strings.Contains(path, "backup-now"):
		return "backup.run", "agent"
	case strings.Contains(path, "verify-now"):
		return "backup.verify", "policy"
	case strings.Contains(path, "snapshots/refresh"):
		return "snapshot.refresh", "agent"
	case strings.Contains(path, "restore"):
		return "restore." + routeVerb(method, path), "snapshot"
	case strings.Contains(path, "cancel"):
		return "task.cancel", "task"
	case strings.HasPrefix(path, "/api/system/import"):
		return "system.import", "system"
	case path == "/api/system/export":
		return "system.export", "system"
	case path == "/api/system/password":
		return "user.password_change", "user"
	case strings.HasPrefix(path, "/api/system/diagnostics"):
		return "diagnostic.collect", "system"
	case strings.HasPrefix(path, "/api/agents"):
		return "agent." + routeVerb(method, path), "agent"
	default:
		return "", ""
	}
}

func routeVerb(method string, path string) string {
	switch method {
	case http.MethodPost:
		if strings.Contains(path, "revoke") {
			return "revoke"
		}
		if strings.Contains(path, "disable") {
			return "disable"
		}
		if strings.Contains(path, "enable") {
			return "enable"
		}
		if strings.Contains(path, "reset-password") {
			return "reset_password"
		}
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

func redactAuditMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
		return "[redacted]"
	}
	return message
}

func queryLimit(c *gin.Context, fallback int, max int) int {
	limit := fallback
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > max {
		limit = max
	}
	return limit
}
