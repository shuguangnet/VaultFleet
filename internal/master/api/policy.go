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
	"vaultfleet/pkg/protocol"
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
	AgentID         string                  `json:"agent_id" binding:"required"`
	StorageID       string                  `json:"storage_id" binding:"required"`
	RepoPath        string                  `json:"repo_path"`
	ResticPassword  string                  `json:"restic_password"`
	BackupDirs      []string                `json:"backup_dirs"`
	BackupSources   []protocol.BackupSource `json:"backup_sources"`
	ExcludePatterns []string                `json:"exclude_patterns"`
	Schedule        string                  `json:"schedule" binding:"required"`
	Retention       map[string]any          `json:"retention" binding:"required"`
	RcloneArgs      map[string]string       `json:"rclone_args"`
	TimeoutHours    *int                    `json:"timeout_hours"`
}

type updatePolicyRequest struct {
	StorageID       string                  `json:"storage_id"`
	BackupDirs      []string                `json:"backup_dirs"`
	BackupSources   []protocol.BackupSource `json:"backup_sources"`
	ExcludePatterns []string                `json:"exclude_patterns"`
	Schedule        string                  `json:"schedule"`
	Retention       map[string]any          `json:"retention"`
	RcloneArgs      map[string]string       `json:"rclone_args"`
	TimeoutHours    *int                    `json:"timeout_hours"`
}

type policyResponse struct {
	ID              string                  `json:"id"`
	AgentID         string                  `json:"agent_id"`
	StorageID       string                  `json:"storage_id"`
	RepoPath        string                  `json:"repo_path"`
	BackupDirs      []string                `json:"backup_dirs"`
	BackupSources   []protocol.BackupSource `json:"backup_sources"`
	ExcludePatterns []string                `json:"exclude_patterns"`
	Schedule        string                  `json:"schedule"`
	Retention       map[string]any          `json:"retention"`
	RcloneArgs      map[string]string       `json:"rclone_args"`
	TimeoutHours    int                     `json:"timeout_hours"`
	Synced          bool                    `json:"synced"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
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

	normalizedDirs, normalizedSources, ok := h.normalizePolicySources(c, request.AgentID, request.BackupDirs, request.BackupSources)
	if !ok {
		return
	}
	backupDirs, ok := marshalPolicyJSON(c, normalizedDirs)
	if !ok {
		return
	}
	backupSources, ok := marshalPolicyJSON(c, normalizedSources)
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
		RepoPath:        repoPath,
		ResticPassword:  encryptedPassword,
		BackupDirs:      backupDirs,
		BackupSources:   backupSources,
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
	if request.BackupDirs != nil || request.BackupSources != nil {
		var nextDirs []string
		var nextSources []protocol.BackupSource
		if request.BackupDirs != nil {
			nextDirs = request.BackupDirs
		}
		if request.BackupSources != nil {
			nextSources = request.BackupSources
		}
		normalizedDirs, normalizedSources, ok := h.normalizePolicySources(c, policy.AgentID, nextDirs, nextSources)
		if !ok {
			return
		}
		backupDirs, ok := marshalPolicyJSON(c, normalizedDirs)
		if !ok {
			return
		}
		backupSources, ok := marshalPolicyJSON(c, normalizedSources)
		if !ok {
			return
		}
		policy.BackupDirs = backupDirs
		policy.BackupSources = backupSources
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
	backupDirs, err := policyBackupDirs(policy)
	if err != nil {
		return policyResponse{}, err
	}
	backupSources, err := policyBackupSources(policy)
	if err != nil {
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
		RepoPath:        policy.RepoPath,
		BackupDirs:      backupDirs,
		BackupSources:   backupSources,
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

func (h *PolicyHandler) normalizePolicySources(c *gin.Context, agentID string, backupDirs []string, backupSources []protocol.BackupSource) ([]string, []protocol.BackupSource, bool) {
	dirs := normalizePolicyPathList(backupDirs)
	sources := normalizeBackupSources(backupSources)
	if len(sources) == 0 {
		for _, dir := range dirs {
			sources = append(sources, protocol.BackupSource{Type: protocol.BackupSourceTypePath, Path: dir})
		}
	}

	mergedDirs := append([]string(nil), dirs...)
	hasDocker := false
	for i := range sources {
		switch sources[i].Type {
		case protocol.BackupSourceTypePath:
			sources[i].Path = strings.TrimSpace(sources[i].Path)
			if sources[i].Path == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "backup source path is required"})
				return nil, nil, false
			}
			mergedDirs = append(mergedDirs, sources[i].Path)
		case protocol.BackupSourceTypeDockerContainer:
			if sources[i].DockerContainer == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "docker container source is required"})
				return nil, nil, false
			}
			normalizeDockerContainerSource(sources[i].DockerContainer)
			if sources[i].DockerContainer.ContainerID == "" &&
				sources[i].DockerContainer.Name == "" &&
				(sources[i].DockerContainer.ComposeProject == "" || sources[i].DockerContainer.ComposeService == "") {
				c.JSON(http.StatusBadRequest, gin.H{"error": "docker container source needs a container id, name, or compose identity"})
				return nil, nil, false
			}
			if !sources[i].DockerContainer.IncludeBindMounts &&
				!sources[i].DockerContainer.IncludeVolumes &&
				!sources[i].DockerContainer.IncludeComposeFiles {
				c.JSON(http.StatusBadRequest, gin.H{"error": "docker container source must include at least one data category"})
				return nil, nil, false
			}
			hasDocker = true
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported backup source type"})
			return nil, nil, false
		}
	}
	mergedDirs = normalizePolicyPathList(mergedDirs)
	if len(sources) == 0 && len(mergedDirs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one backup source is required"})
		return nil, nil, false
	}
	if hasDocker {
		supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerWorkloadBackups)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
			return nil, nil, false
		}
		if !supported {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent does not support Docker workload backups"})
			return nil, nil, false
		}
	}
	return mergedDirs, sources, true
}

func normalizePolicyPathList(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	return normalized
}

func normalizeBackupSources(sources []protocol.BackupSource) []protocol.BackupSource {
	normalized := make([]protocol.BackupSource, 0, len(sources))
	for _, source := range sources {
		source.Type = strings.TrimSpace(source.Type)
		if source.Type == "" {
			continue
		}
		normalized = append(normalized, source)
	}
	return normalized
}

func normalizeDockerContainerSource(source *protocol.DockerContainerBackupSource) {
	source.ContainerID = strings.TrimSpace(source.ContainerID)
	source.Name = strings.Trim(strings.TrimSpace(source.Name), "/")
	source.Image = strings.TrimSpace(source.Image)
	source.ComposeProject = strings.TrimSpace(source.ComposeProject)
	source.ComposeService = strings.TrimSpace(source.ComposeService)
	source.ComposeWorkingDir = strings.TrimSpace(source.ComposeWorkingDir)
	source.ComposeConfigFiles = normalizePolicyPathList(source.ComposeConfigFiles)
}

func policyBackupDirs(policy db.BackupPolicy) ([]string, error) {
	backupDirs := []string{}
	if strings.TrimSpace(policy.BackupDirs) == "" {
		return backupDirs, nil
	}
	if err := json.Unmarshal([]byte(policy.BackupDirs), &backupDirs); err != nil {
		return nil, err
	}
	return normalizePolicyPathList(backupDirs), nil
}

func policyBackupSources(policy db.BackupPolicy) ([]protocol.BackupSource, error) {
	var sources []protocol.BackupSource
	if strings.TrimSpace(policy.BackupSources) != "" {
		if err := json.Unmarshal([]byte(policy.BackupSources), &sources); err != nil {
			return nil, err
		}
		return normalizeBackupSources(sources), nil
	}
	dirs, err := policyBackupDirs(policy)
	if err != nil {
		return nil, err
	}
	sources = make([]protocol.BackupSource, 0, len(dirs))
	for _, dir := range dirs {
		sources = append(sources, protocol.BackupSource{Type: protocol.BackupSourceTypePath, Path: dir})
	}
	return sources, nil
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
