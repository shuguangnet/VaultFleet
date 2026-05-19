package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

type RouterHub interface {
	BrowseHub
	SnapshotHub
	RestoreHub
}

type RouterConfig struct {
	Database       *db.Database
	Hub            RouterHub
	EventBus       *events.Bus
	AgentWebSocket gin.HandlerFunc
}

func NewRouter(cfg RouterConfig) *gin.Engine {
	if cfg.Database == nil || cfg.Database.DB == nil {
		panic("router database is required")
	}

	r := gin.New()
	r.Use(gin.Recovery())

	authHandler := NewAuthHandler(cfg.Database)
	agentHandler := NewAgentHandler(cfg.Database)
	storageHandler := NewConfigHandler(cfg.Database)
	storageHandler.EventBus = cfg.EventBus
	policyHandler := NewPolicyHandler(cfg.Database, cfg.EventBus)
	browseHandler := NewBrowseHandler(cfg.Database, cfg.Hub)
	snapshotHandler := NewSnapshotHandler(cfg.Database, cfg.Hub)
	restoreHandler := NewRestoreHandler(cfg.Database, cfg.Hub)
	notificationHandler := NewNotificationHandler(cfg.Database)
	systemHandler := NewSystemHandler(cfg.Database)

	public := r.Group("/api")
	public.GET("/auth/check", authHandler.CheckInit)
	public.POST("/auth/init", authHandler.InitSetup)
	public.POST("/auth/login", authHandler.Login)
	public.POST("/agent/enroll", agentHandler.Enroll)

	agentWebSocket := cfg.AgentWebSocket
	if agentWebSocket == nil {
		agentWebSocket = unavailableAgentWebSocket(cfg.Database)
	}
	r.GET("/ws/agent", agentWebSocket)

	protected := r.Group("/api")
	protected.Use(RequireInit(cfg.Database), RequireAuth(authHandler.Sessions))
	protected.POST("/agents", agentHandler.Create)
	protected.GET("/agents", agentHandler.List)
	protected.GET("/agents/:id", agentHandler.Get)
	protected.DELETE("/agents/:id", agentHandler.Delete)
	protected.POST("/agents/:id/regenerate-token", agentHandler.RegenerateToken)
	RegisterStorageRoutes(protected, storageHandler)
	RegisterPolicyRoutes(protected, policyHandler)
	RegisterBrowseRoutes(protected, browseHandler)
	RegisterSnapshotRoutes(protected, snapshotHandler)
	RegisterRestoreRoutes(protected, restoreHandler)
	RegisterNotificationRoutes(protected, notificationHandler)
	RegisterSystemRoutes(protected.Group("/system"), systemHandler)

	RegisterFrontendRoutes(r)

	return r
}

func AuthenticateAgentByToken(database *db.Database) func(token string) (string, error) {
	return func(token string) (string, error) {
		var agent db.Agent
		if err := database.DB.First(&agent, "agent_token = ?", token).Error; err != nil {
			return "", err
		}
		return agent.ID, nil
	}
}

func CurrentPolicyLookup(database *db.Database) func(agentID string) (*protocol.Message, bool) {
	return func(agentID string) (*protocol.Message, bool) {
		var policy db.BackupPolicy
		if err := database.DB.
			Where("agent_id = ? AND synced = ?", agentID, false).
			Order("updated_at DESC").
			First(&policy).Error; err != nil {
			return nil, false
		}

		var storage db.StorageConfig
		if err := database.DB.First(&storage, "id = ?", policy.StorageID).Error; err != nil {
			return nil, false
		}

		payload, err := policyPushPayload(database, policy, storage)
		if err != nil {
			return nil, false
		}

		msg, err := protocol.NewMessage(protocol.TypePolicyPush, payload)
		if err != nil {
			return nil, false
		}
		return msg, true
	}
}

func NewPolicyAckProcessor(database *db.Database) func(agentID string, msg protocol.Message) error {
	return func(agentID string, msg protocol.Message) error {
		ack, err := protocol.ParsePayload[protocol.PolicyAckPayload](&msg)
		if err != nil {
			return err
		}
		if !ack.Success {
			return nil
		}

		var policy db.BackupPolicy
		if err := database.DB.
			Where("agent_id = ? AND synced = ?", agentID, false).
			Order("updated_at DESC").
			First(&policy).Error; err != nil {
			return nil
		}

		return database.DB.Model(&policy).Update("synced", true).Error
	}
}

func unavailableAgentWebSocket(database *db.Database) gin.HandlerFunc {
	authAgent := AuthenticateAgentByToken(database)
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "missing token"})
			return
		}
		if _, err := authAgent(token); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid token"})
			return
		}

		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "websocket handler not configured"})
	}
}

func policyPushPayload(database *db.Database, policy db.BackupPolicy, storage db.StorageConfig) (protocol.PolicyPushPayload, error) {
	resticPassword, err := db.Decrypt(policy.ResticPassword, database.MasterKey)
	if err != nil {
		return protocol.PolicyPushPayload{}, err
	}

	rcloneConfig, err := decryptPolicyRcloneConfig(database, storage.RcloneConfig)
	if err != nil {
		return protocol.PolicyPushPayload{}, err
	}

	var backupDirs []string
	if err := json.Unmarshal([]byte(policy.BackupDirs), &backupDirs); err != nil {
		return protocol.PolicyPushPayload{}, err
	}

	excludePatterns := []string{}
	if policy.ExcludePatterns != "" {
		if err := json.Unmarshal([]byte(policy.ExcludePatterns), &excludePatterns); err != nil {
			return protocol.PolicyPushPayload{}, err
		}
	}

	var retention protocol.RetentionPolicy
	if err := json.Unmarshal([]byte(policy.Retention), &retention); err != nil {
		return protocol.PolicyPushPayload{}, err
	}

	return protocol.PolicyPushPayload{
		AgentID: policy.AgentID,
		Storage: protocol.StorageConfig{
			RcloneType:   storage.RcloneType,
			RcloneConfig: rcloneConfig,
			RepoPath:     policy.RepoPath,
		},
		ResticPassword:  resticPassword,
		BackupDirs:      backupDirs,
		ExcludePatterns: excludePatterns,
		Schedule:        policy.Schedule,
		Retention:       retention,
	}, nil
}

func decryptPolicyRcloneConfig(database *db.Database, rawConfig string) (map[string]string, error) {
	plaintext, err := db.Decrypt(rawConfig, database.MasterKey)
	if err != nil {
		return nil, err
	}

	var values map[string]string
	if err := json.Unmarshal([]byte(plaintext), &values); err == nil {
		return values, nil
	}

	var anyValues map[string]any
	if err := json.Unmarshal([]byte(plaintext), &anyValues); err != nil {
		return nil, err
	}

	values = make(map[string]string, len(anyValues))
	for key, value := range anyValues {
		if stringValue, ok := value.(string); ok {
			values[key] = stringValue
			continue
		}
		return nil, fmt.Errorf("rclone config %q must be a string", key)
	}
	return values, nil
}
