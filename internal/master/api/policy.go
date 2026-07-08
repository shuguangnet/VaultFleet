package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
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
	AgentID         string                               `json:"agent_id" binding:"required"`
	StorageID       string                               `json:"storage_id" binding:"required"`
	BackupMode      string                               `json:"backup_mode"`
	ArchiveFormat   string                               `json:"archive_format"`
	RepoPath        string                               `json:"repo_path"`
	ResticPassword  string                               `json:"restic_password"`
	BackupDirs      []string                             `json:"backup_dirs"`
	BackupSources   []protocol.BackupSource              `json:"backup_sources"`
	ExcludePatterns []string                             `json:"exclude_patterns"`
	PreBackupHook   *policyHookInput                     `json:"pre_backup_hook"`
	PostBackupHook  *policyHookInput                     `json:"post_backup_hook"`
	Schedule        string                               `json:"schedule" binding:"required"`
	Retention       map[string]any                       `json:"retention" binding:"required"`
	RcloneArgs      map[string]string                    `json:"rclone_args"`
	TimeoutHours    *int                                 `json:"timeout_hours"`
	Verification    *protocol.BackupVerificationSettings `json:"verification"`
}

type updatePolicyRequest struct {
	StorageID       string                               `json:"storage_id"`
	BackupMode      string                               `json:"backup_mode"`
	ArchiveFormat   string                               `json:"archive_format"`
	BackupDirs      []string                             `json:"backup_dirs"`
	BackupSources   []protocol.BackupSource              `json:"backup_sources"`
	ExcludePatterns []string                             `json:"exclude_patterns"`
	PreBackupHook   *policyHookInput                     `json:"pre_backup_hook"`
	PostBackupHook  *policyHookInput                     `json:"post_backup_hook"`
	Schedule        string                               `json:"schedule"`
	Retention       map[string]any                       `json:"retention"`
	RcloneArgs      map[string]string                    `json:"rclone_args"`
	TimeoutHours    *int                                 `json:"timeout_hours"`
	Verification    *protocol.BackupVerificationSettings `json:"verification"`
}

type bulkAssignPolicyRequest struct {
	SourcePolicyID string   `json:"source_policy_id" binding:"required"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	TargetTags     []string `json:"target_tags"`
}

type policyResponse struct {
	ID                 string                               `json:"id"`
	AgentID            string                               `json:"agent_id"`
	StorageID          string                               `json:"storage_id"`
	BackupMode         string                               `json:"backup_mode"`
	ArchiveFormat      string                               `json:"archive_format,omitempty"`
	RepoPath           string                               `json:"repo_path"`
	BackupDirs         []string                             `json:"backup_dirs"`
	BackupSources      []protocol.BackupSource              `json:"backup_sources"`
	ExcludePatterns    []string                             `json:"exclude_patterns"`
	PreBackupHook      *policyHookInput                     `json:"pre_backup_hook,omitempty"`
	PostBackupHook     *policyHookInput                     `json:"post_backup_hook,omitempty"`
	Schedule           string                               `json:"schedule"`
	Retention          map[string]any                       `json:"retention"`
	RcloneArgs         map[string]string                    `json:"rclone_args"`
	TimeoutHours       int                                  `json:"timeout_hours"`
	Verification       *protocol.BackupVerificationSettings `json:"verification,omitempty"`
	LatestVerification *policyVerificationSummary           `json:"latest_verification,omitempty"`
	Synced             bool                                 `json:"synced"`
	CreatedAt          time.Time                            `json:"created_at"`
	UpdatedAt          time.Time                            `json:"updated_at"`
}

type policyVerificationSummary struct {
	Status     string     `json:"status"`
	SnapshotID string     `json:"snapshot_id,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	TaskID     string     `json:"task_id,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type bulkAssignPolicyResponse struct {
	SourcePolicyID string                   `json:"source_policy_id"`
	TargetTags     []string                 `json:"target_tags"`
	RequestedCount int                      `json:"requested_count"`
	MatchedCount   int                      `json:"matched_count"`
	CreatedCount   int                      `json:"created_count"`
	FailedCount    int                      `json:"failed_count"`
	Results        []bulkAssignPolicyResult `json:"results"`
}

type bulkAssignPolicyResult struct {
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	PolicyID  string `json:"policy_id,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

type policyHookInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

const maxPolicyHookTimeoutSeconds = 3600

func RegisterPolicyRoutes(rg *gin.RouterGroup, h *PolicyHandler) {
	rg.POST("/policies", h.CreatePolicy)
	rg.GET("/policies", h.ListPolicies)
	rg.POST("/policies/bulk-assign", h.BulkAssignPolicies)
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
	backupMode := normalizeBackupMode(request.BackupMode)
	verification, ok := validatePolicyVerification(c, backupMode, request.Verification)
	if !ok {
		return
	}
	verificationRaw, ok := marshalOptionalPolicyVerification(c, verification)
	if !ok {
		return
	}
	preBackupHook, ok := validatePolicyHook(c, request.PreBackupHook)
	if !ok {
		return
	}
	postBackupHook, ok := validatePolicyHook(c, request.PostBackupHook)
	if !ok {
		return
	}
	preBackupHookRaw, ok := marshalOptionalPolicyHook(c, preBackupHook)
	if !ok {
		return
	}
	postBackupHookRaw, ok := marshalOptionalPolicyHook(c, postBackupHook)
	if !ok {
		return
	}

	policy := db.BackupPolicy{
		AgentID:         request.AgentID,
		StorageID:       request.StorageID,
		BackupMode:      backupMode,
		ArchiveFormat:   normalizeArchiveFormat(request.ArchiveFormat),
		RepoPath:        repoPath,
		ResticPassword:  encryptedPassword,
		BackupDirs:      backupDirs,
		BackupSources:   backupSources,
		ExcludePatterns: excludePatterns,
		PreBackupHook:   preBackupHookRaw,
		PostBackupHook:  postBackupHookRaw,
		Schedule:        request.Schedule,
		Retention:       retention,
		RcloneArgs:      rcloneArgs,
		TimeoutHours:    timeoutHours,
		Verification:    verificationRaw,
		Synced:          false,
	}

	if err := h.DB.DB.Create(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.publishPolicyChanged(policy.AgentID, "created")
	h.writePolicyResponse(c, http.StatusCreated, policy)
}

func (h *PolicyHandler) BulkAssignPolicies(c *gin.Context) {
	var request bulkAssignPolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	source, ok := h.findPolicyByID(c, request.SourcePolicyID)
	if !ok {
		return
	}
	if !h.storageExists(c, source.StorageID) {
		return
	}

	targetTags, ok := normalizeAgentTagsFromValues(c, request.TargetTags)
	if !ok {
		return
	}
	if len(request.TargetAgentIDs) == 0 && len(targetTags) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_agent_ids or target_tags is required"})
		return
	}

	requirements, err := policySourceRequirementsForPolicy(source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode source policy"})
		return
	}

	var agents []db.Agent
	if err := h.DB.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	response := h.bulkAssignPolicies(source, agents, request.TargetAgentIDs, targetTags, requirements)
	writeDataResponse(c, http.StatusOK, response)
}

func (h *PolicyHandler) bulkAssignPolicies(source db.BackupPolicy, agents []db.Agent, targetAgentIDs []string, targetTags []string, requirements policySourceRequirements) bulkAssignPolicyResponse {
	agentByID := make(map[string]db.Agent, len(agents))
	for _, agent := range agents {
		agentByID[agent.ID] = agent
	}

	response := bulkAssignPolicyResponse{
		SourcePolicyID: source.ID,
		TargetTags:     targetTags,
		Results:        []bulkAssignPolicyResult{},
	}

	targets := make([]db.Agent, 0, len(targetAgentIDs)+len(agents))
	targetSeen := map[string]bool{}
	requestedSeen := map[string]bool{}
	for _, rawID := range targetAgentIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || requestedSeen[id] {
			continue
		}
		requestedSeen[id] = true
		response.RequestedCount++
		agent, found := agentByID[id]
		if !found {
			response.Results = append(response.Results, bulkAssignPolicyResult{
				AgentID: id,
				OK:      false,
				Error:   "agent not found",
			})
			response.FailedCount++
			continue
		}
		targets = append(targets, agent)
		targetSeen[id] = true
	}

	if len(targetTags) > 0 {
		matched := filterAgentsByTags(agents, targetTags)
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].Name < matched[j].Name
		})
		for _, agent := range matched {
			if targetSeen[agent.ID] {
				continue
			}
			response.RequestedCount++
			targetSeen[agent.ID] = true
			targets = append(targets, agent)
		}
	}

	response.MatchedCount = len(targets)
	for _, agent := range targets {
		result := bulkAssignPolicyResult{
			AgentID:   agent.ID,
			AgentName: agent.Name,
		}
		if requirements.requiresDocker {
			supported, err := agentHasCapability(h.DB, agent.ID, protocol.CapabilityDockerWorkloadBackups)
			if err != nil {
				result.Error = "agent not found"
			} else if !supported {
				result.Error = "agent does not support Docker workload backups"
			}
			if result.Error != "" {
				response.Results = append(response.Results, result)
				response.FailedCount++
				continue
			}
		}
		if requirements.requiresDatabase {
			supported, err := agentHasCapability(h.DB, agent.ID, protocol.CapabilityDatabaseBackups)
			if err != nil {
				result.Error = "agent not found"
			} else if !supported {
				result.Error = "agent does not support database backups"
			}
			if result.Error != "" {
				response.Results = append(response.Results, result)
				response.FailedCount++
				continue
			}
		}

		policy := clonePolicyForAgent(source, agent.ID)
		if err := h.DB.DB.Create(&policy).Error; err != nil {
			result.Error = "database error"
			response.Results = append(response.Results, result)
			response.FailedCount++
			continue
		}

		result.OK = true
		result.PolicyID = policy.ID
		response.CreatedCount++
		response.Results = append(response.Results, result)
		h.publishPolicyChanged(agent.ID, "bulk_assigned")
	}

	return response
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
		response, err := h.newPolicyResponse(policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
			return
		}
		responses = append(responses, response)
	}

	writeDataResponse(c, http.StatusOK, responses)
}

func clonePolicyForAgent(source db.BackupPolicy, agentID string) db.BackupPolicy {
	return db.BackupPolicy{
		AgentID:         agentID,
		StorageID:       source.StorageID,
		BackupMode:      normalizeBackupMode(source.BackupMode),
		ArchiveFormat:   normalizeArchiveFormat(source.ArchiveFormat),
		RepoPath:        "vaultfleet/" + agentID,
		ResticPassword:  source.ResticPassword,
		BackupDirs:      source.BackupDirs,
		BackupSources:   source.BackupSources,
		ExcludePatterns: source.ExcludePatterns,
		PreBackupHook:   source.PreBackupHook,
		PostBackupHook:  source.PostBackupHook,
		Schedule:        source.Schedule,
		Retention:       source.Retention,
		RcloneArgs:      source.RcloneArgs,
		TimeoutHours:    normalizedPolicyTimeoutHours(source.TimeoutHours),
		Verification:    source.Verification,
		Synced:          false,
	}
}

type policySourceRequirements struct {
	requiresDocker   bool
	requiresDatabase bool
}

func policySourceRequirementsForPolicy(policy db.BackupPolicy) (policySourceRequirements, error) {
	sources, err := policyBackupSources(policy)
	if err != nil {
		return policySourceRequirements{}, err
	}
	var requirements policySourceRequirements
	for _, source := range sources {
		if source.Type == protocol.BackupSourceTypeDockerContainer {
			requirements.requiresDocker = true
		}
		if source.Type == protocol.BackupSourceTypeDatabase {
			requirements.requiresDatabase = true
			if source.Database != nil && source.Database.ExecutionMode == protocol.DatabaseExecutionDocker {
				requirements.requiresDocker = true
			}
		}
	}
	return requirements, nil
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
	if request.BackupDirs != nil || request.BackupSources != nil {
		var nextDirs []string
		var nextSources []protocol.BackupSource
		if request.BackupDirs != nil {
			nextDirs = request.BackupDirs
		}
		if request.BackupSources != nil {
			nextSources = request.BackupSources
		}
		existingSources, err := policyBackupSources(policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
			return
		}
		normalizedDirs, normalizedSources, ok := h.normalizePolicySourcesForStorage(c, policy.AgentID, nextDirs, nextSources, existingSources)
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
	if request.PreBackupHook != nil {
		preBackupHook, ok := validatePolicyHook(c, request.PreBackupHook)
		if !ok {
			return
		}
		preBackupHookRaw, ok := marshalOptionalPolicyHook(c, preBackupHook)
		if !ok {
			return
		}
		policy.PreBackupHook = preBackupHookRaw
	}
	if request.PostBackupHook != nil {
		postBackupHook, ok := validatePolicyHook(c, request.PostBackupHook)
		if !ok {
			return
		}
		postBackupHookRaw, ok := marshalOptionalPolicyHook(c, postBackupHook)
		if !ok {
			return
		}
		policy.PostBackupHook = postBackupHookRaw
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
	if request.Verification != nil {
		verification, ok := validatePolicyVerification(c, normalizeBackupMode(policy.BackupMode), request.Verification)
		if !ok {
			return
		}
		verificationRaw, ok := marshalOptionalPolicyVerification(c, verification)
		if !ok {
			return
		}
		policy.Verification = verificationRaw
	}
	if request.Verification == nil && normalizeBackupMode(policy.BackupMode) == protocol.BackupModeArchive {
		existingVerification, err := unmarshalPolicyVerification(policy.Verification)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode verification settings"})
			return
		}
		if existingVerification != nil && existingVerification.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "archive backup verification is unsupported"})
			return
		}
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
	response, err := h.newPolicyResponse(policy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode policy"})
		return
	}

	writeDataResponse(c, status, response)
}

func (h *PolicyHandler) newPolicyResponse(policy db.BackupPolicy) (policyResponse, error) {
	response, err := newPolicyResponse(policy)
	if err != nil {
		return policyResponse{}, err
	}
	response.LatestVerification = h.latestPolicyVerification(policy.ID)
	return response, nil
}

func (h *PolicyHandler) latestPolicyVerification(policyID string) *policyVerificationSummary {
	if h == nil || h.DB == nil || h.DB.DB == nil || policyID == "" {
		return nil
	}
	var history db.TaskHistory
	if err := h.DB.DB.
		Where("policy_id = ? AND type = ?", policyID, "verify").
		Order("created_at DESC").
		First(&history).Error; err != nil {
		return nil
	}
	checkedAt := history.FinishedAt
	if checkedAt == nil {
		checkedAt = history.StartedAt
	}
	return &policyVerificationSummary{
		Status:     history.Status,
		SnapshotID: history.SnapshotID,
		CheckedAt:  checkedAt,
		TaskID:     history.ID,
		Error:      history.ErrorLog,
	}
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
	preBackupHook, err := unmarshalPolicyHook(policy.PreBackupHook)
	if err != nil {
		return policyResponse{}, err
	}
	postBackupHook, err := unmarshalPolicyHook(policy.PostBackupHook)
	if err != nil {
		return policyResponse{}, err
	}
	verification, err := unmarshalPolicyVerification(policy.Verification)
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
		BackupSources:   redactDatabaseBackupSourceSecrets(backupSources),
		ExcludePatterns: excludePatterns,
		PreBackupHook:   preBackupHook,
		PostBackupHook:  postBackupHook,
		Schedule:        policy.Schedule,
		Retention:       retention,
		RcloneArgs:      rcloneArgs,
		TimeoutHours:    normalizedPolicyTimeoutHours(policy.TimeoutHours),
		Verification:    verification,
		Synced:          policy.Synced,
		CreatedAt:       policy.CreatedAt,
		UpdatedAt:       policy.UpdatedAt,
	}, nil
}

func validatePolicyHook(c *gin.Context, hook *policyHookInput) (*policyHookInput, bool) {
	if hook == nil {
		return nil, true
	}
	command := strings.TrimSpace(hook.Command)
	if command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook command is required"})
		return nil, false
	}
	if hook.TimeoutSeconds < 0 || hook.TimeoutSeconds > maxPolicyHookTimeoutSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook timeout_seconds must be between 0 and 3600"})
		return nil, false
	}
	return &policyHookInput{Command: command, TimeoutSeconds: hook.TimeoutSeconds}, true
}

func marshalOptionalPolicyHook(c *gin.Context, hook *policyHookInput) (string, bool) {
	if hook == nil {
		return "", true
	}
	return marshalPolicyJSON(c, hook)
}

func unmarshalPolicyHook(raw string) (*policyHookInput, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var hook policyHookInput
	if err := json.Unmarshal([]byte(raw), &hook); err != nil {
		return nil, err
	}
	return &hook, nil
}

func validatePolicyVerification(c *gin.Context, backupMode string, settings *protocol.BackupVerificationSettings) (*protocol.BackupVerificationSettings, bool) {
	if settings == nil {
		return nil, true
	}
	normalized := *settings
	if !normalized.Enabled {
		return &normalized, true
	}
	if normalizeBackupMode(backupMode) == protocol.BackupModeArchive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archive backup verification is unsupported"})
		return nil, false
	}
	if normalized.SampleCount < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "verification sample_count must be greater than or equal to 0"})
		return nil, false
	}
	if normalized.SampleCount == 0 {
		normalized.SampleCount = 10
	}
	if normalized.TimeoutMinutes < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "verification timeout_minutes must be greater than or equal to 0"})
		return nil, false
	}
	if normalized.TimeoutMinutes == 0 {
		normalized.TimeoutMinutes = 60
	}
	return &normalized, true
}

func marshalOptionalPolicyVerification(c *gin.Context, settings *protocol.BackupVerificationSettings) (string, bool) {
	if settings == nil {
		return "", true
	}
	return marshalPolicyJSON(c, settings)
}

func unmarshalPolicyVerification(raw string) (*protocol.BackupVerificationSettings, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var settings protocol.BackupVerificationSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func (h *PolicyHandler) normalizePolicySources(c *gin.Context, agentID string, backupDirs []string, backupSources []protocol.BackupSource) ([]string, []protocol.BackupSource, bool) {
	return h.normalizePolicySourcesForStorage(c, agentID, backupDirs, backupSources, nil)
}

func (h *PolicyHandler) normalizePolicySourcesForStorage(c *gin.Context, agentID string, backupDirs []string, backupSources []protocol.BackupSource, existingSources []protocol.BackupSource) ([]string, []protocol.BackupSource, bool) {
	dirs := normalizePolicyPathList(backupDirs)
	sources := normalizeBackupSources(backupSources)
	if len(sources) == 0 {
		for _, dir := range dirs {
			sources = append(sources, protocol.BackupSource{Type: protocol.BackupSourceTypePath, Path: dir})
		}
	}

	mergedDirs := append([]string(nil), dirs...)
	hasDocker := false
	hasDatabase := false
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
		case protocol.BackupSourceTypeDatabase:
			if sources[i].Database == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "database source is required"})
				return nil, nil, false
			}
			if ok := normalizeDatabaseBackupSource(c, sources[i].Database); !ok {
				return nil, nil, false
			}
			if sources[i].Database.ExecutionMode == protocol.DatabaseExecutionDocker {
				if sources[i].Database.DockerContainer == nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "database docker source needs a container"})
					return nil, nil, false
				}
				normalizeDockerContainerSource(sources[i].Database.DockerContainer)
				if sources[i].Database.DockerContainer.ContainerID == "" &&
					sources[i].Database.DockerContainer.Name == "" &&
					(sources[i].Database.DockerContainer.ComposeProject == "" || sources[i].Database.DockerContainer.ComposeService == "") {
					c.JSON(http.StatusBadRequest, gin.H{"error": "database docker source needs a container id, name, or compose identity"})
					return nil, nil, false
				}
				hasDocker = true
			}
			password, ok := h.prepareDatabaseSourcePassword(c, *sources[i].Database, existingSources)
			if !ok {
				return nil, nil, false
			}
			sources[i].Database.Password = password
			sources[i].Database.PasswordSet = strings.TrimSpace(password) != ""
			hasDatabase = true
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
	if hasDatabase {
		supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDatabaseBackups)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
			return nil, nil, false
		}
		if !supported {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent does not support database backups"})
			return nil, nil, false
		}
	}
	return mergedDirs, sources, true
}

func (h *PolicyHandler) prepareDatabaseSourcePassword(c *gin.Context, source protocol.DatabaseBackupSource, existingSources []protocol.BackupSource) (string, bool) {
	if strings.TrimSpace(source.Password) != "" {
		encrypted, err := db.Encrypt(source.Password, h.MasterKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
			return "", false
		}
		return encrypted, true
	}
	if source.PasswordSet {
		if existing := findExistingDatabasePassword(source, existingSources); strings.TrimSpace(existing) != "" {
			return existing, true
		}
	}
	return "", true
}

func findExistingDatabasePassword(source protocol.DatabaseBackupSource, existingSources []protocol.BackupSource) string {
	key := databaseSourceIdentityKey(source)
	for _, existing := range existingSources {
		if existing.Type != protocol.BackupSourceTypeDatabase || existing.Database == nil {
			continue
		}
		if databaseSourceIdentityKey(*existing.Database) == key {
			return existing.Database.Password
		}
	}
	return ""
}

func databaseSourceIdentityKey(source protocol.DatabaseBackupSource) string {
	container := ""
	if source.DockerContainer != nil {
		container = source.DockerContainer.ContainerID + "|" + source.DockerContainer.Name + "|" + source.DockerContainer.ComposeProject + "|" + source.DockerContainer.ComposeService
	}
	return strings.Join([]string{
		strings.TrimSpace(source.Engine),
		strings.TrimSpace(source.ExecutionMode),
		strings.TrimSpace(source.Host),
		strconv.Itoa(source.Port),
		strings.TrimSpace(source.Username),
		strings.TrimSpace(source.Database),
		strconv.FormatBool(source.AllDatabases),
		container,
	}, "\x00")
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

func normalizeDatabaseBackupSource(c *gin.Context, source *protocol.DatabaseBackupSource) bool {
	source.Engine = strings.ToLower(strings.TrimSpace(source.Engine))
	switch source.Engine {
	case protocol.DatabaseEnginePostgreSQL, protocol.DatabaseEngineMySQL:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source engine must be postgresql or mysql"})
		return false
	}
	source.ExecutionMode = strings.ToLower(strings.TrimSpace(source.ExecutionMode))
	switch source.ExecutionMode {
	case protocol.DatabaseExecutionHost, protocol.DatabaseExecutionDocker:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source execution_mode must be host or docker"})
		return false
	}
	source.Host = strings.TrimSpace(source.Host)
	source.Username = strings.TrimSpace(source.Username)
	source.Database = strings.TrimSpace(source.Database)
	source.OutputName = strings.TrimSpace(source.OutputName)
	source.ConnectionName = strings.TrimSpace(source.ConnectionName)
	source.ExtraArgs = normalizePolicyPathList(source.ExtraArgs)
	if source.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source username is required"})
		return false
	}
	if source.Port < 0 || source.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source port must be between 0 and 65535"})
		return false
	}
	if !source.AllDatabases && source.Database == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source database is required unless all_databases is enabled"})
		return false
	}
	if source.AllDatabases {
		source.Database = ""
	}
	if source.DumpTimeoutSeconds < 0 || source.DumpTimeoutSeconds > maxPolicyHookTimeoutSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database source dump_timeout_seconds must be between 0 and 3600"})
		return false
	}
	return true
}

func redactDatabaseBackupSourceSecrets(sources []protocol.BackupSource) []protocol.BackupSource {
	redacted := make([]protocol.BackupSource, len(sources))
	copy(redacted, sources)
	for i := range redacted {
		if redacted[i].Type != protocol.BackupSourceTypeDatabase || redacted[i].Database == nil {
			continue
		}
		database := *redacted[i].Database
		database.PasswordSet = strings.TrimSpace(database.Password) != ""
		database.Password = ""
		redacted[i].Database = &database
	}
	return redacted
}

func decryptDatabaseBackupSourceSecrets(sources []protocol.BackupSource, masterKey []byte) ([]protocol.BackupSource, error) {
	decrypted := make([]protocol.BackupSource, len(sources))
	copy(decrypted, sources)
	for i := range decrypted {
		if decrypted[i].Type != protocol.BackupSourceTypeDatabase || decrypted[i].Database == nil {
			continue
		}
		database := *decrypted[i].Database
		if strings.TrimSpace(database.Password) != "" {
			password, err := db.Decrypt(database.Password, masterKey)
			if err != nil {
				return nil, err
			}
			database.Password = password
			database.PasswordSet = password != ""
		}
		decrypted[i].Database = &database
	}
	return decrypted, nil
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
	case "local-no-check-updated":
		return "true", isTruthyPolicyRcloneArgValue(normalized)
	default:
		return "", false
	}
}

func isAllowedPolicyRcloneArg(key string) bool {
	switch key {
	case "transfers", "tpslimit", "retries", "retries-sleep", "low-level-retries", "timeout", "local-no-check-updated":
		return true
	default:
		return false
	}
}

func isTruthyPolicyRcloneArgValue(value string) bool {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
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
