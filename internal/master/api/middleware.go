package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

func RequireAuth(sessions *SessionStore, databases ...*db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var database *db.Database
		if len(databases) > 0 {
			database = databases[0]
		}

		if database != nil {
			if actor, ok := authenticateBearerActor(c, database); ok {
				if actor == nil {
					return
				}
				setActor(c, *actor)
				c.Next()
				return
			}
		}

		token, err := c.Cookie(sessionCookieName)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
			return
		}

		session, ok := sessions.Get(token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
			return
		}

		role := normalizeRole(session.Role)
		if database != nil {
			var user db.User
			if err := database.DB.First(&user, "id = ?", session.UserID).Error; err != nil || user.DisabledAt != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
				return
			}
			session.UserID = user.ID
			session.Username = user.Username
			role = normalizeRole(user.Role)
		}
		if role == "" {
			role = RoleAdmin
		}

		setActor(c, Actor{
			Type:        ActorTypeUser,
			UserID:      session.UserID,
			Username:    session.Username,
			Role:        role,
			Permissions: effectivePermissions(role, nil, false),
		})
		c.Set("user_id", session.UserID)
		c.Set("username", session.Username)
		c.Set("role", role)
		c.Next()
	}
}

func authenticateBearerActor(c *gin.Context, database *db.Database) (*Actor, bool) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header == "" {
		return nil, false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
		return nil, true
	}
	actor, err := AuthenticateAPIToken(database, parts[1])
	if err != nil {
		status := http.StatusUnauthorized
		if !errorsIsNotFound(err) {
			status = http.StatusInternalServerError
		}
		c.AbortWithStatusJSON(status, gin.H{"ok": false, "error": "unauthorized"})
		return nil, true
	}
	return actor, true
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func RequireInit(database *db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var count int64
		if err := database.DB.Model(&db.User{}).Count(&count).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
			return
		}
		if count == 0 {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"ok": false, "error": "init_required"})
			return
		}

		c.Next()
	}
}
