package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
)

type PolicyHandler struct {
	DB        *db.Database
	EventBus  *events.Bus
	MasterKey []byte
}

func NewPolicyHandler(database *db.Database, eventBus *events.Bus) *PolicyHandler {
	return &PolicyHandler{
		DB:        database,
		EventBus:  eventBus,
		MasterKey: database.MasterKey,
	}
}

type createPolicyRequest struct {
	AgentID         string         `json:"agent_id" binding:"required"`
	StorageID       string         `json:"storage_id" binding:"required"`
	RepoPath        string         `json:"repo_path"`
	ResticPassword  string         `json:"restic_password"`
	BackupDirs      []string       `json:"backup_dirs" binding:"required"`
	ExcludePatterns []string       `json:"exclude_patterns"`
	Schedule        string         `json:"schedule" binding:"required"`
	Retention       map[string]any `json:"retention" binding:"required"`
}

type updatePolicyRequest struct {
	StorageID       string         `json:"storage_id"`
	BackupDirs      []string       `json:"backup_dirs"`
	ExcludePatterns []string       `json:"exclude_patterns"`
	Schedule        string         `json:"schedule"`
	Retention       map[string]any `json:"retention"`
}

type policyResponse struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agent_id"`
	StorageID       string         `json:"storage_id"`
	RepoPath        string         `json:"repo_path"`
	ResticPassword  string         `json:"restic_password,omitempty"`
	BackupDirs      []string       `json:"backup_dirs"`
	ExcludePatterns []string       `json:"exclude_patterns"`
	Schedule        string         `json:"schedule"`
	Retention       map[string]any `json:"retention"`
	Synced          bool           `json:"synced"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func RegisterPolicyRoutes(rg *gin.RouterGroup, h *PolicyHandler) {
	rg.POST("/policies", h.CreatePolicy)
	rg.GET("/policies", h.ListPolicies)
	rg.GET("/policies/:id", h.GetPolicy)
	rg.PUT("/policies/:id", h.UpdatePolicy)
	rg.DELETE("/policies/:id", h.DeletePolicy)
}

func (h *PolicyHandler) CreatePolicy(c *gin.Context) {
	var request createPolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if !h.agentExists(c, request.AgentID) || !h.storageExists(c, request.StorageID) {
		return
	}

	repoPath := request.RepoPath
	if repoPath == "" {
		repoPath = "vaultfleet/" + request.AgentID
	}

	resticPassword := request.ResticPassword
	if resticPassword == "" {
		generatedPassword, err := generateResticPassword(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "password generation failed"})
			return
		}
		resticPassword = generatedPassword
	}

	encryptedPassword, err := db.Encrypt(resticPassword, h.MasterKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return
	}

	backupDirs, ok := marshalPolicyJSON(c, request.BackupDirs)
	if !ok {
		return
	}
	excludePatterns, ok := marshalPolicyJSON(c, request.ExcludePatterns)
	if !ok {
		return
	}
	retention, ok := marshalPolicyJSON(c, request.Retention)
	if !ok {
		return
	}

	policy := db.BackupPolicy{
		AgentID:         request.AgentID,
		StorageID:       request.StorageID,
		RepoPath:        repoPath,
		ResticPassword:  encryptedPassword,
		BackupDirs:      backupDirs,
		ExcludePatterns: excludePatterns,
		Schedule:        request.Schedule,
		Retention:       retention,
		Synced:          false,
	}

	if err := h.DB.DB.Create(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "created")
	h.writePolicyResponse(c, http.StatusCreated, policy, resticPassword)
}

func (h *PolicyHandler) ListPolicies(c *gin.Context) {
	var policies []db.BackupPolicy
	query := h.DB.DB.Order("created_at DESC")
	if agentID := c.Query("agent_id"); agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}

	if err := query.Find(&policies).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	responses := make([]policyResponse, 0, len(policies))
	for _, policy := range policies {
		response, err := newPolicyResponse(policy, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
			return
		}
		responses = append(responses, response)
	}

	c.JSON(http.StatusOK, responses)
}

func (h *PolicyHandler) GetPolicy(c *gin.Context) {
	policy, ok := h.findPolicyByID(c, c.Param("id"))
	if !ok {
		return
	}

	h.writePolicyResponse(c, http.StatusOK, policy, "")
}

func (h *PolicyHandler) UpdatePolicy(c *gin.Context) {
	policy, ok := h.findPolicyByID(c, c.Param("id"))
	if !ok {
		return
	}

	var request updatePolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if request.StorageID != "" {
		if !h.storageExists(c, request.StorageID) {
			return
		}
		policy.StorageID = request.StorageID
	}
	if request.BackupDirs != nil {
		backupDirs, ok := marshalPolicyJSON(c, request.BackupDirs)
		if !ok {
			return
		}
		policy.BackupDirs = backupDirs
	}
	if request.ExcludePatterns != nil {
		excludePatterns, ok := marshalPolicyJSON(c, request.ExcludePatterns)
		if !ok {
			return
		}
		policy.ExcludePatterns = excludePatterns
	}
	if request.Schedule != "" {
		policy.Schedule = request.Schedule
	}
	if request.Retention != nil {
		retention, ok := marshalPolicyJSON(c, request.Retention)
		if !ok {
			return
		}
		policy.Retention = retention
	}

	policy.Synced = false
	if err := h.DB.DB.Save(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "updated")

	h.writePolicyResponse(c, http.StatusOK, policy, "")
}

func (h *PolicyHandler) DeletePolicy(c *gin.Context) {
	policy, ok := h.findPolicyByID(c, c.Param("id"))
	if !ok {
		return
	}

	if err := h.DB.DB.Delete(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "deleted")
	c.Status(http.StatusNoContent)
}

func (h *PolicyHandler) findPolicyByID(c *gin.Context, id string) (db.BackupPolicy, bool) {
	var policy db.BackupPolicy
	if err := h.DB.DB.First(&policy, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
			return db.BackupPolicy{}, false
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return db.BackupPolicy{}, false
	}

	return policy, true
}

func (h *PolicyHandler) agentExists(c *gin.Context, id string) bool {
	var count int64
	if err := h.DB.DB.Model(&db.Agent{}).Where("id = ?", id).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return false
	}
	if count == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return false
	}

	return true
}

func (h *PolicyHandler) storageExists(c *gin.Context, id string) bool {
	var count int64
	if err := h.DB.DB.Model(&db.StorageConfig{}).Where("id = ?", id).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return false
	}
	if count == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "storage config not found"})
		return false
	}

	return true
}

func (h *PolicyHandler) writePolicyResponse(c *gin.Context, status int, policy db.BackupPolicy, plainPassword string) {
	response, err := newPolicyResponse(policy, plainPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
		return
	}

	c.JSON(status, response)
}

func (h *PolicyHandler) publishPolicyChanged(agentID string, action string) {
	if h.EventBus == nil {
		return
	}

	h.EventBus.Publish(events.Event{
		Type: events.PolicyChanged,
		Payload: map[string]interface{}{
			"agent_id": agentID,
			"action":   action,
		},
	})
}

func newPolicyResponse(policy db.BackupPolicy, plainPassword string) (policyResponse, error) {
	var backupDirs []string
	if err := json.Unmarshal([]byte(policy.BackupDirs), &backupDirs); err != nil {
		return policyResponse{}, err
	}

	excludePatterns := []string{}
	if policy.ExcludePatterns != "" {
		if err := json.Unmarshal([]byte(policy.ExcludePatterns), &excludePatterns); err != nil {
			return policyResponse{}, err
		}
	}

	var retention map[string]any
	if err := json.Unmarshal([]byte(policy.Retention), &retention); err != nil {
		return policyResponse{}, err
	}

	return policyResponse{
		ID:              policy.ID,
		AgentID:         policy.AgentID,
		StorageID:       policy.StorageID,
		RepoPath:        policy.RepoPath,
		ResticPassword:  plainPassword,
		BackupDirs:      backupDirs,
		ExcludePatterns: excludePatterns,
		Schedule:        policy.Schedule,
		Retention:       retention,
		Synced:          policy.Synced,
		CreatedAt:       policy.CreatedAt,
		UpdatedAt:       policy.UpdatedAt,
	}, nil
}

func marshalPolicyJSON(c *gin.Context, value any) (string, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}

	return string(data), true
}

func generateResticPassword(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes)[:length], nil
}
