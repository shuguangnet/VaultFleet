package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
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

const (
	defaultPolicyTimeoutHours = 6
	minPolicyTimeoutHours     = 1
	maxPolicyTimeoutHours     = 72
)

func NewPolicyHandler(database *db.Database, eventBus *events.Bus) *PolicyHandler {
	return &PolicyHandler{
		DB:        database,
		EventBus:  eventBus,
		MasterKey: database.MasterKey,
	}
}

type createPolicyRequest struct {
	AgentID         string            `json:"agent_id" binding:"required"`
	StorageID       string            `json:"storage_id" binding:"required"`
	BackupMode      string            `json:"backup_mode"`
	ArchiveFormat   string            `json:"archive_format"`
	RepoPath        string            `json:"repo_path"`
	ResticPassword  string            `json:"restic_password"`
	BackupDirs      []string          `json:"backup_dirs" binding:"required"`
	ExcludePatterns []string          `json:"exclude_patterns"`
	Schedule        string            `json:"schedule" binding:"required"`
	Retention       map[string]any    `json:"retention" binding:"required"`
	RcloneArgs      map[string]string `json:"rclone_args"`
	TimeoutHours    *int              `json:"timeout_hours"`
}

type updatePolicyRequest struct {
	StorageID       string            `json:"storage_id"`
	BackupMode      string            `json:"backup_mode"`
	ArchiveFormat   string            `json:"archive_format"`
	BackupDirs      []string          `json:"backup_dirs"`
	ExcludePatterns []string          `json:"exclude_patterns"`
	Schedule        string            `json:"schedule"`
	Retention       map[string]any    `json:"retention"`
	RcloneArgs      map[string]string `json:"rclone_args"`
	TimeoutHours    *int              `json:"timeout_hours"`
}

type policyResponse struct {
	ID              string            `json:"id"`
	AgentID         string            `json:"agent_id"`
	StorageID       string            `json:"storage_id"`
	BackupMode      string            `json:"backup_mode"`
	ArchiveFormat   string            `json:"archive_format,omitempty"`
	RepoPath        string            `json:"repo_path"`
	BackupDirs      []string          `json:"backup_dirs"`
	ExcludePatterns []string          `json:"exclude_patterns"`
	Schedule        string            `json:"schedule"`
	Retention       map[string]any    `json:"retention"`
	RcloneArgs      map[string]string `json:"rclone_args"`
	TimeoutHours    int               `json:"timeout_hours"`
	Synced          bool              `json:"synced"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
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

	encryptedPassword, err := db.Encrypt(request.ResticPassword, h.MasterKey)
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
	rcloneArgs := ""
	if len(request.RcloneArgs) > 0 {
		normalizedRcloneArgs, ok := validatePolicyRcloneArgs(c, request.RcloneArgs)
		if !ok {
			return
		}
		rcloneArgs, ok = marshalPolicyJSON(c, normalizedRcloneArgs)
		if !ok {
			return
		}
	}
	timeoutHours, ok := validatePolicyTimeoutHours(c, request.TimeoutHours)
	if !ok {
		return
	}

	policy := db.BackupPolicy{
		AgentID:         request.AgentID,
		StorageID:       request.StorageID,
		BackupMode:      normalizeBackupMode(request.BackupMode),
		ArchiveFormat:   normalizeArchiveFormat(request.ArchiveFormat),
		RepoPath:        repoPath,
		ResticPassword:  encryptedPassword,
		BackupDirs:      backupDirs,
		ExcludePatterns: excludePatterns,
		Schedule:        request.Schedule,
		Retention:       retention,
		RcloneArgs:      rcloneArgs,
		TimeoutHours:    timeoutHours,
		Synced:          false,
	}

	if err := h.DB.DB.Create(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "created")
	h.writePolicyResponse(c, http.StatusCreated, policy)
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
		response, err := newPolicyResponse(policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
			return
		}
		responses = append(responses, response)
	}

	writeDataResponse(c, http.StatusOK, responses)
}

func (h *PolicyHandler) GetPolicy(c *gin.Context) {
	policy, ok := h.findPolicyByID(c, c.Param("id"))
	if !ok {
		return
	}

	h.writePolicyResponse(c, http.StatusOK, policy)
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
	if request.BackupMode != "" {
		policy.BackupMode = normalizeBackupMode(request.BackupMode)
	}
	if request.ArchiveFormat != "" {
		policy.ArchiveFormat = normalizeArchiveFormat(request.ArchiveFormat)
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
	if request.RcloneArgs != nil {
		normalizedRcloneArgs, ok := validatePolicyRcloneArgs(c, request.RcloneArgs)
		if !ok {
			return
		}
		rcloneArgs, ok := marshalPolicyJSON(c, normalizedRcloneArgs)
		if !ok {
			return
		}
		policy.RcloneArgs = rcloneArgs
	}
	if request.TimeoutHours != nil {
		timeoutHours, ok := validatePolicyTimeoutHours(c, request.TimeoutHours)
		if !ok {
			return
		}
		policy.TimeoutHours = timeoutHours
	}

	policy.Synced = false
	if err := h.DB.DB.Save(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "updated")

	h.writePolicyResponse(c, http.StatusOK, policy)
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

func (h *PolicyHandler) writePolicyResponse(c *gin.Context, status int, policy db.BackupPolicy) {
	response, err := newPolicyResponse(policy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
		return
	}

	writeDataResponse(c, status, response)
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

func newPolicyResponse(policy db.BackupPolicy) (policyResponse, error) {
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

	rcloneArgs, err := unmarshalPolicyRcloneArgs(policy.RcloneArgs)
	if err != nil {
		return policyResponse{}, err
	}

	return policyResponse{
		ID:              policy.ID,
		AgentID:         policy.AgentID,
		StorageID:       policy.StorageID,
		BackupMode:      normalizeBackupMode(policy.BackupMode),
		ArchiveFormat:   normalizeArchiveFormat(policy.ArchiveFormat),
		RepoPath:        policy.RepoPath,
		BackupDirs:      backupDirs,
		ExcludePatterns: excludePatterns,
		Schedule:        policy.Schedule,
		Retention:       retention,
		RcloneArgs:      rcloneArgs,
		TimeoutHours:    normalizedPolicyTimeoutHours(policy.TimeoutHours),
		Synced:          policy.Synced,
		CreatedAt:       policy.CreatedAt,
		UpdatedAt:       policy.UpdatedAt,
	}, nil
}

func validatePolicyTimeoutHours(c *gin.Context, timeoutHours *int) (int, bool) {
	if timeoutHours == nil {
		return defaultPolicyTimeoutHours, true
	}
	if *timeoutHours < minPolicyTimeoutHours || *timeoutHours > maxPolicyTimeoutHours {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timeout_hours must be between 1 and 72"})
		return 0, false
	}
	return *timeoutHours, true
}

func normalizedPolicyTimeoutHours(timeoutHours int) int {
	if timeoutHours < minPolicyTimeoutHours || timeoutHours > maxPolicyTimeoutHours {
		return defaultPolicyTimeoutHours
	}
	return timeoutHours
}

func normalizeBackupMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", protocol.BackupModeSnapshot:
		return protocol.BackupModeSnapshot
	case protocol.BackupModeArchive:
		return protocol.BackupModeArchive
	default:
		return protocol.BackupModeSnapshot
	}
}

func normalizeArchiveFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case protocol.ArchiveFormatZip:
		return protocol.ArchiveFormatZip
	case protocol.ArchiveFormatTarGz, "":
		return protocol.ArchiveFormatTarGz
	default:
		return protocol.ArchiveFormatTarGz
	}
}

func unmarshalPolicyRcloneArgs(raw string) (map[string]string, error) {
	rcloneArgs := map[string]string{}
	if raw == "" {
		return rcloneArgs, nil
	}
	if err := json.Unmarshal([]byte(raw), &rcloneArgs); err != nil {
		return nil, err
	}
	if rcloneArgs == nil {
		return map[string]string{}, nil
	}
	return rcloneArgs, nil
}

func validatePolicyRcloneArgs(c *gin.Context, args map[string]string) (map[string]string, bool) {
	normalized := make(map[string]string, len(args))
	for key, value := range args {
		normalizedValue, ok := normalizePolicyRcloneArgValue(key, value)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rclone_args"})
			return nil, false
		}
		normalized[key] = normalizedValue
	}
	return normalized, true
}

func normalizePolicyRcloneArgValue(key string, value string) (string, bool) {
	if !isAllowedPolicyRcloneArg(key) {
		return "", false
	}

	normalized := strings.TrimSpace(value)
	if normalized == "" || strings.ContainsAny(normalized, " \t\r\n") {
		return "", false
	}

	switch key {
	case "transfers":
		parsed, err := strconv.Atoi(normalized)
		return normalized, err == nil && parsed > 0
	case "retries", "low-level-retries":
		parsed, err := strconv.Atoi(normalized)
		return normalized, err == nil && parsed >= 0
	case "tpslimit":
		parsed, err := strconv.ParseFloat(normalized, 64)
		return normalized, err == nil && parsed >= 0
	case "retries-sleep", "timeout":
		if normalized == "0" {
			return normalized, true
		}
		parsed, err := time.ParseDuration(normalized)
		return normalized, err == nil && parsed >= 0
	default:
		return "", false
	}
}

func isAllowedPolicyRcloneArg(key string) bool {
	switch key {
	case "transfers", "tpslimit", "retries", "retries-sleep", "low-level-retries", "timeout":
		return true
	default:
		return false
	}
}

func marshalPolicyJSON(c *gin.Context, value any) (string, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}

	return string(data), true
}
