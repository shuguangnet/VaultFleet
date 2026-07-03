package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"vaultfleet/internal/agent/dockerops"
	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

const dockerRequestTimeout = 30 * time.Second

type DockerHandler struct {
	DB       *db.Database
	Hub      DockerHub
	Commands *commands.Service
	EventBus *events.Bus

	timeout     time.Duration
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type DockerHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type dockerBackupProfileRequest struct {
	StorageID       string                     `json:"storage_id" binding:"required"`
	RepoPath        string                     `json:"repo_path"`
	ResticPassword  string                     `json:"restic_password"`
	Containers      []protocol.DockerContainer `json:"containers" binding:"required"`
	ExcludePatterns []string                   `json:"exclude_patterns"`
	Schedule        string                     `json:"schedule"`
	Retention       map[string]any             `json:"retention"`
	TimeoutHours    *int                       `json:"timeout_hours"`
	RunNow          bool                       `json:"run_now"`
}

type dockerRestoreRequest struct {
	SnapshotID            string   `json:"snapshot_id" binding:"required"`
	TargetPath            string   `json:"target_path" binding:"required"`
	IncludePaths          []string `json:"include_paths"`
	ManifestPath          string   `json:"manifest_path"`
	PrecheckOnly          bool     `json:"precheck_only"`
	StartContainers       bool     `json:"start_containers"`
	StartupCommand        string   `json:"startup_command"`
	CommandTimeoutSeconds int      `json:"command_timeout_seconds"`
}

func NewDockerHandler(database *db.Database, hub DockerHub, commandService *commands.Service, eventBus *events.Bus) *DockerHandler {
	h := &DockerHandler{DB: database, Hub: hub, Commands: commandService, EventBus: eventBus, timeout: dockerRequestTimeout}
	h.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		return h.Hub.SendAndWait(agentID, msg, timeout)
	}
	return h
}

func RegisterDockerRoutes(rg *gin.RouterGroup, h *DockerHandler) {
	rg.POST("/agents/:id/docker/discover", h.Discover)
	rg.POST("/agents/:id/docker/backup-profile", h.CreateBackupProfile)
	rg.POST("/agents/:id/docker/restore", h.Restore)
}

func (h *DockerHandler) Discover(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeErrorResponse(c, http.StatusConflict, "agent is offline")
		return
	}
	if ok, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerBackup); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	} else if !ok {
		writeErrorResponse(c, http.StatusBadRequest, "agent does not support docker backup")
		return
	}
	var req protocol.DockerDiscoverReqPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	req.AgentID = agentID
	msg, err := protocol.NewMessage(protocol.TypeDockerDiscoverReq, req)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode docker discovery request")
		return
	}
	respCh, err := h.sendAndWait(agentID, *msg, h.timeout)
	if err != nil {
		writeErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}
	select {
	case resp, ok := <-respCh:
		if !ok || resp.Type != protocol.TypeDockerDiscoverResp {
			writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
			return
		}
		payload, err := protocol.ParsePayload[protocol.DockerDiscoverRespPayload](&resp)
		if err != nil {
			writeErrorResponse(c, http.StatusBadGateway, "invalid agent response")
			return
		}
		if payload.Error != "" {
			writeErrorResponse(c, http.StatusBadGateway, payload.Error)
			return
		}
		writeDataResponse(c, http.StatusOK, payload)
	case <-c.Request.Context().Done():
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func (h *DockerHandler) CreateBackupProfile(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	var req dockerBackupProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Containers) == 0 {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	if !storageExistsByID(c, h.DB, req.StorageID) {
		return
	}
	timeoutHours, ok := validatePolicyTimeoutHours(c, req.TimeoutHours)
	if !ok {
		return
	}
	policyID := uuid.NewString()
	metadataDir := filepath.Join("/etc/vaultfleet/docker-backups", policyID)
	manifest := dockerops.BuildManifest(req.Containers, time.Now().UTC())
	backupDirs := append(dockerops.BackupDirs(req.Containers), metadataDir)
	backupDirs = uniqueStrings(backupDirs)
	if len(backupDirs) == 0 {
		writeErrorResponse(c, http.StatusBadRequest, "no backup paths discovered")
		return
	}
	manifest.BackupDirs = backupDirs
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode docker manifest")
		return
	}
	backupDirsRaw, ok := marshalPolicyJSON(c, backupDirs)
	if !ok {
		return
	}
	excludeRaw, ok := marshalPolicyJSON(c, req.ExcludePatterns)
	if !ok {
		return
	}
	retention := req.Retention
	if retention == nil {
		retention = map[string]any{"keep_last": 7, "keep_daily": 7, "keep_weekly": 4, "keep_monthly": 6}
	}
	retentionRaw, ok := marshalPolicyJSON(c, retention)
	if !ok {
		return
	}
	encryptedPassword, err := db.Encrypt(req.ResticPassword, h.DB.MasterKey)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encryption failed")
		return
	}
	repoPath := req.RepoPath
	if repoPath == "" {
		repoPath = "vaultfleet/" + agentID + "/docker"
	}
	policy := db.BackupPolicy{
		ID:              policyID,
		AgentID:         agentID,
		StorageID:       req.StorageID,
		BackupMode:      protocol.BackupModeSnapshot,
		RepoPath:        repoPath,
		ResticPassword:  encryptedPassword,
		BackupDirs:      backupDirsRaw,
		ExcludePatterns: excludeRaw,
		PreBackupHook:   dockerManifestHook(metadataDir, manifestData),
		Schedule:        req.Schedule,
		Retention:       retentionRaw,
		TimeoutHours:    timeoutHours,
		Synced:          false,
	}
	if err := h.DB.DB.WithContext(contextFromGin(c)).Create(&policy).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	h.publishPolicyChanged(agentID, "created")
	response := gin.H{"policy_id": policy.ID, "backup_dirs": backupDirs}
	if req.RunNow {
		backup, err := h.queueBackupNow(c, agentID, policy)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, err.Error())
			return
		}
		response["backup_command"] = backup
	}
	writeDataResponse(c, http.StatusCreated, response)
}

func (h *DockerHandler) Restore(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	var req dockerRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	msg, err := protocol.NewMessage(protocol.TypeDockerRestoreReq, protocol.DockerRestoreReqPayload{
		SnapshotID:            req.SnapshotID,
		Target:                req.TargetPath,
		IncludePaths:          req.IncludePaths,
		ManifestPath:          req.ManifestPath,
		PrecheckOnly:          req.PrecheckOnly,
		StartContainers:       req.StartContainers,
		StartupCommand:        req.StartupCommand,
		CommandTimeoutSeconds: req.CommandTimeoutSeconds,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode docker restore request")
		return
	}
	var timeoutHours int
	var policyID, storageID string
	var policy db.BackupPolicy
	if err := h.DB.DB.Where("agent_id = ?", agentID).First(&policy).Error; err == nil {
		timeoutHours = normalizedPolicyTimeoutHours(policy.TimeoutHours)
		policyID = policy.ID
		storageID = policy.StorageID
	}
	commandService := h.commandService()
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID: agentID, Type: protocol.TypeDockerRestoreReq, Message: *msg, TaskType: "restore", TaskState: commands.TaskStatusPending,
		SnapshotID: req.SnapshotID, PolicyID: policyID, StorageID: storageID, TimeoutHours: timeoutHours,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if h.Hub != nil && h.Hub.IsOnline(agentID) {
		if err := commandService.DispatchNewPendingForAgent(contextFromGin(c), agentID, 100); err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
	}
	writeDataResponse(c, http.StatusAccepted, gin.H{"command_id": command.ID, "message_id": msg.ID})
}

func (h *DockerHandler) queueBackupNow(c *gin.Context, agentID string, policy db.BackupPolicy) (gin.H, error) {
	var storage db.StorageConfig
	if err := h.DB.DB.First(&storage, "id = ?", policy.StorageID).Error; err != nil {
		return nil, err
	}
	payload, err := policyPushPayload(h.DB, policy, storage)
	if err != nil {
		return nil, err
	}
	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agentID, Policy: &payload})
	if err != nil {
		return nil, err
	}
	command, err := h.commandService().CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID: agentID, Type: protocol.TypeBackupNow, Message: *msg, TaskType: "backup", TaskState: commands.TaskStatusPending,
		PolicyID: policy.ID, StorageID: policy.StorageID, TimeoutHours: normalizedPolicyTimeoutHours(policy.TimeoutHours),
	})
	if err != nil {
		return nil, err
	}
	if h.Hub != nil && h.Hub.IsOnline(agentID) {
		if err := h.commandService().DispatchNewPendingForAgent(contextFromGin(c), agentID, 100); err != nil {
			return nil, err
		}
	}
	return gin.H{"command_id": command.ID, "message_id": msg.ID}, nil
}

func (h *DockerHandler) commandService() *commands.Service {
	if h.Commands != nil {
		return h.Commands
	}
	return commands.NewService(h.DB, h.Hub)
}

func (h *DockerHandler) publishPolicyChanged(agentID string, action string) {
	if h.EventBus == nil {
		return
	}
	h.EventBus.Publish(events.Event{Type: events.PolicyChanged, Payload: map[string]interface{}{"agent_id": agentID, "action": action}})
}

func dockerManifestHook(metadataDir string, data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	command := "mkdir -p " + shellQuote(metadataDir) + " && printf %s " + shellQuote(encoded) + " | base64 -d > " + shellQuote(filepath.Join(metadataDir, "manifest.json"))
	raw, _ := json.Marshal(policyHookInput{Command: command, TimeoutSeconds: 60})
	return string(raw)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func storageExistsByID(c *gin.Context, database *db.Database, id string) bool {
	var count int64
	if err := database.DB.Model(&db.StorageConfig{}).Where("id = ?", id).Count(&count).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return false
	}
	if count == 0 {
		writeErrorResponse(c, http.StatusBadRequest, "storage config not found")
		return false
	}
	return true
}
