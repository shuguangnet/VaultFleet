package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/agent/executor"
	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/rcloneobscure"
)

const defaultTaskListLimit = 50
const maxTaskListLimit = 200

type TaskHandler struct {
	DB             *db.Database
	Hub            CommandHub
	Commands       *commands.Service
	ProgressGetter func(agentID string, messageID string) *protocol.BackupProgressPayload
}

type CommandHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type taskResponse struct {
	ID                  string                             `json:"id"`
	AgentID             string                             `json:"agent_id"`
	Type                string                             `json:"type"`
	Status              string                             `json:"status"`
	SnapshotID          string                             `json:"snapshot_id"`
	ArtifactPath        string                             `json:"artifact_path,omitempty"`
	ArtifactName        string                             `json:"artifact_name,omitempty"`
	ArtifactSize        int64                              `json:"artifact_size,omitempty"`
	ArtifactContentType string                             `json:"artifact_content_type,omitempty"`
	BackupMode          string                             `json:"backup_mode,omitempty"`
	ArchiveFormat       string                             `json:"archive_format,omitempty"`
	MessageID           string                             `json:"message_id,omitempty"`
	CommandID           string                             `json:"command_id,omitempty"`
	PolicyID            string                             `json:"policy_id,omitempty"`
	StorageID           string                             `json:"storage_id,omitempty"`
	StartedAt           *time.Time                         `json:"started_at"`
	FinishedAt          *time.Time                         `json:"finished_at"`
	DurationMs          int64                              `json:"duration_ms"`
	RepoSize            int64                              `json:"repo_size"`
	RepoSizeBytes       int64                              `json:"repository_size_bytes"`
	ErrorLog            string                             `json:"error_log,omitempty"`
	Error               string                             `json:"error,omitempty"`
	Progress            *protocol.BackupProgressPayload    `json:"progress,omitempty"`
	Docker              *protocol.DockerBackupMetadata     `json:"docker,omitempty"`
	Verification        *protocol.BackupVerificationResult `json:"verification,omitempty"`
	CreatedAt           time.Time                          `json:"created_at"`
	UpdatedAt           time.Time                          `json:"updated_at"`
}

func NewTaskHandler(database *db.Database, hub CommandHub) *TaskHandler {
	return &TaskHandler{DB: database, Hub: hub}
}

func RegisterTaskRoutes(rg *gin.RouterGroup, h *TaskHandler) {
	rg.GET("/tasks", h.List)
	rg.GET("/tasks/:id/download", h.DownloadArtifact)
	rg.POST("/tasks/:id/cancel", h.CancelTask)
	rg.POST("/agents/:id/backup-now", h.BackupNow)
	rg.POST("/policies/:id/verify-now", h.VerifyPolicyNow)
}

func (h *TaskHandler) VerifyPolicyNow(c *gin.Context) {
	policyID := c.Param("id")
	var policy db.BackupPolicy
	if err := h.DB.DB.First(&policy, "id = ?", policyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "policy not found"})
		return
	}
	if normalizeBackupMode(policy.BackupMode) == protocol.BackupModeArchive {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "archive backup verification is unsupported"})
		return
	}
	supportsVerification, err := agentHasCapability(h.DB, policy.AgentID, protocol.CapabilityBackupVerification)
	if err != nil || !supportsVerification {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "agent must be upgraded to support backup verification"})
		return
	}

	var storage db.StorageConfig
	if err := h.DB.DB.First(&storage, "id = ?", policy.StorageID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "load backup storage"})
		return
	}
	payload, err := policyPushPayload(h.DB, policy, storage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "build backup policy payload"})
		return
	}
	settings, err := unmarshalPolicyVerification(policy.Verification)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "decode verification settings"})
		return
	}
	if settings == nil {
		settings = &protocol.BackupVerificationSettings{Enabled: true, SampleCount: 10, TimeoutMinutes: 60}
	}
	verifyMsg, err := protocol.NewMessage(protocol.TypeBackupVerifyReq, protocol.BackupVerifyReqPayload{
		AgentID:      policy.AgentID,
		Policy:       &payload,
		Verification: settings,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "encode verification request"})
		return
	}

	commandService := h.commandService()
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID:      policy.AgentID,
		Type:         protocol.TypeBackupVerifyReq,
		Message:      *verifyMsg,
		TaskType:     "verify",
		TaskState:    commands.TaskStatusPending,
		PolicyID:     policy.ID,
		StorageID:    policy.StorageID,
		TimeoutHours: verificationTimeoutHours(settings.TimeoutMinutes),
	})
	if err != nil {
		log.Printf("create backup verification command failed for policy %s: %v", policy.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	if h.Hub != nil && h.Hub.IsOnline(policy.AgentID) {
		if err := commandService.DispatchNewPendingForAgent(contextFromGin(c), policy.AgentID, 100); err != nil {
			log.Printf("dispatch backup verification command failed for policy %s: %v", policy.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
			return
		}
	}

	writeDataResponse(c, http.StatusAccepted, gin.H{
		"command_id": command.ID,
		"message_id": verifyMsg.ID,
	})
}

func (h *TaskHandler) BackupNow(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	commandService := h.commandService()

	var timeoutHours int
	var policyID, storageID string
	var policy db.BackupPolicy
	var backupPolicyPayload *protocol.PolicyPushPayload
	if err := h.DB.DB.Where("agent_id = ?", agentID).Order("updated_at DESC").First(&policy).Error; err == nil {
		timeoutHours = normalizedPolicyTimeoutHours(policy.TimeoutHours)
		policyID = policy.ID
		storageID = policy.StorageID
		var storage db.StorageConfig
		if err := h.DB.DB.First(&storage, "id = ?", policy.StorageID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "load backup storage"})
			return
		}
		payload, err := policyPushPayload(h.DB, policy, storage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "build backup policy payload"})
			return
		}
		backupPolicyPayload = &payload
	}

	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agentID, Policy: backupPolicyPayload})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "encode backup request"})
		return
	}

	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID:      agentID,
		Type:         protocol.TypeBackupNow,
		Message:      *msg,
		TaskType:     "backup",
		TaskState:    commands.TaskStatusPending,
		PolicyID:     policyID,
		StorageID:    storageID,
		TimeoutHours: timeoutHours,
	})
	if err != nil {
		log.Printf("create backup_now command failed for agent %s: %v", agentID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	if h.Hub != nil && h.Hub.IsOnline(agentID) {
		if err := commandService.DispatchNewPendingForAgent(contextFromGin(c), agentID, 100); err != nil {
			log.Printf("dispatch backup_now command failed for agent %s: %v", agentID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
			return
		}
	}

	writeDataResponse(c, http.StatusAccepted, gin.H{
		"command_id": command.ID,
		"message_id": msg.ID,
	})
}

func (h *TaskHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("id")

	var history db.TaskHistory
	if err := h.DB.DB.First(&history, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "task not found"})
		return
	}

	if history.Status != commands.TaskStatusRunning && history.Status != commands.TaskStatusPending {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "task is not running or pending"})
		return
	}

	if history.CommandID == "" {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "task has no associated command"})
		return
	}

	commandService := h.commandService()
	result, err := commandService.CancelCommand(contextFromGin(c), history.CommandID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if result.NeedsWS {
		if h.Hub == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "agent websocket unavailable"})
			return
		}
		if !h.Hub.IsOnline(result.AgentID) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "agent is offline"})
			return
		}
		cancelMsg, err := protocol.NewMessage(protocol.TypeCancelTask, protocol.CancelTaskPayload{
			AgentID:   result.AgentID,
			MessageID: result.MessageID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "encode cancel command"})
			return
		}
		if err := h.Hub.Send(result.AgentID, *cancelMsg); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "send cancel command failed"})
			return
		}
	}

	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": "cancel requested"})
}

func (h *TaskHandler) DownloadArtifact(c *gin.Context) {
	taskID := c.Param("id")
	var history db.TaskHistory
	if err := h.DB.DB.First(&history, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "task not found"})
		return
	}
	if history.ArtifactPath == "" || history.ArtifactName == "" {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "task artifact not found"})
		return
	}
	artifactPath, err := ensureTaskArtifactAvailable(h.DB, &history)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "task artifact not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+history.ArtifactName+"\"")
	if history.ArtifactContentType != "" {
		c.Header("Content-Type", history.ArtifactContentType)
		c.File(artifactPath)
		return
	}
	c.File(artifactPath)
}

func ensureTaskArtifactAvailable(database *db.Database, history *db.TaskHistory) (string, error) {
	if history == nil {
		return "", fmt.Errorf("task history unavailable")
	}
	artifactPath, err := resolveArtifactPath(database, history.ArtifactPath)
	if err == nil {
		artifactName := strings.TrimSpace(history.ArtifactName)
		if artifactName == "" {
			artifactName = filepath.Base(filepath.FromSlash(history.ArtifactPath))
		}
		standardRelPath := filepath.ToSlash(filepath.Join("artifacts", safeArtifactPathComponent(history.AgentID), artifactName))
		if history.ArtifactPath == standardRelPath {
			return artifactPath, nil
		}
	}
	return repairTaskArtifact(database, history)
}

func resolveArtifactPath(database *db.Database, storedPath string) (string, error) {
	if storedPath == "" {
		return "", fmt.Errorf("empty artifact path")
	}
	if filepath.IsAbs(storedPath) {
		if _, err := os.Stat(storedPath); err != nil {
			return "", err
		}
		return storedPath, nil
	}
	if database == nil {
		return "", fmt.Errorf("database unavailable")
	}
	baseDir := database.DataDir
	if baseDir == "" {
		baseDir = "."
	}
	candidate := filepath.Join(baseDir, filepath.FromSlash(storedPath))
	if _, err := os.Stat(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func repairTaskArtifact(database *db.Database, history *db.TaskHistory) (string, error) {
	if database == nil || history == nil {
		return "", fmt.Errorf("database unavailable")
	}
	artifactName := strings.TrimSpace(history.ArtifactName)
	if artifactName == "" {
		artifactName = filepath.Base(filepath.FromSlash(history.ArtifactPath))
	}
	if artifactName == "." || artifactName == string(filepath.Separator) || artifactName == "" {
		return "", fmt.Errorf("invalid artifact name")
	}

	stdRelPath := filepath.ToSlash(filepath.Join("artifacts", safeArtifactPathComponent(history.AgentID), artifactName))
	stdAbsPath := filepath.Join(database.DataDir, filepath.FromSlash(stdRelPath))
	if _, err := os.Stat(stdAbsPath); err == nil {
		if history.ArtifactPath != stdRelPath {
			_ = database.DB.Model(&db.TaskHistory{}).Where("id = ?", history.ID).Update("artifact_path", stdRelPath).Error
			history.ArtifactPath = stdRelPath
		}
		return stdAbsPath, nil
	}
	if fetchedPath, err := fetchTaskArtifactFromStorage(database, history, artifactName, stdRelPath, stdAbsPath); err == nil {
		return fetchedPath, nil
	}

	for _, candidate := range candidateArtifactPaths(database, history, artifactName) {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(stdAbsPath), 0o755); err != nil {
			return "", err
		}
		if err := copyFile(candidate, stdAbsPath); err != nil {
			return "", err
		}
		if err := database.DB.Model(&db.TaskHistory{}).Where("id = ?", history.ID).Update("artifact_path", stdRelPath).Error; err != nil {
			return "", err
		}
		history.ArtifactPath = stdRelPath
		if history.ArtifactSize == 0 {
			history.ArtifactSize = info.Size()
			_ = database.DB.Model(&db.TaskHistory{}).Where("id = ?", history.ID).Update("artifact_size", info.Size()).Error
		}
		return stdAbsPath, nil
	}
	return "", fmt.Errorf("artifact not found")
}

func fetchTaskArtifactFromStorage(database *db.Database, history *db.TaskHistory, artifactName string, stdRelPath string, stdAbsPath string) (string, error) {
	if database == nil || history == nil || strings.TrimSpace(history.StorageID) == "" {
		return "", fmt.Errorf("storage unavailable")
	}
	var storage db.StorageConfig
	if err := database.DB.First(&storage, "id = ?", history.StorageID).Error; err != nil {
		return "", err
	}
	repoPath, err := repoPathForTaskHistory(database, history)
	if err != nil {
		return "", err
	}
	configValues := map[string]string{}
	if strings.TrimSpace(storage.RcloneConfig) != "" {
		if err := json.Unmarshal([]byte(storage.RcloneConfig), &configValues); err != nil {
			return "", err
		}
	}
	tempDir, err := os.MkdirTemp("", "vaultfleet-artifact-rclone-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)
	confPath := filepath.Join(tempDir, "rclone.conf")
	if err := writeArtifactRcloneConf(confPath, storage.RcloneType, configValues); err != nil {
		return "", err
	}
	runner := executor.PlainRunner{RcloneConfPath: confPath, RepoPath: repoPath}
	remoteArtifactPath := strings.TrimSpace(history.ArtifactPath)
	if remoteArtifactPath == "" {
		remoteArtifactPath = filepath.ToSlash(filepath.Join("artifacts", artifactName))
	}
	if err := runner.CopyFileFromRemote(context.Background(), remoteArtifactPath, stdAbsPath); err != nil {
		return "", err
	}
	info, err := os.Stat(stdAbsPath)
	if err != nil {
		return "", err
	}
	updates := map[string]any{"artifact_path": stdRelPath}
	if history.ArtifactSize == 0 {
		updates["artifact_size"] = info.Size()
		history.ArtifactSize = info.Size()
	}
	if err := database.DB.Model(&db.TaskHistory{}).Where("id = ?", history.ID).Updates(updates).Error; err != nil {
		return "", err
	}
	history.ArtifactPath = stdRelPath
	return stdAbsPath, nil
}

func repoPathForTaskHistory(database *db.Database, history *db.TaskHistory) (string, error) {
	if history == nil {
		return "", fmt.Errorf("task history unavailable")
	}
	if strings.TrimSpace(history.PolicyID) != "" {
		var policy db.BackupPolicy
		if err := database.DB.First(&policy, "id = ?", history.PolicyID).Error; err == nil && strings.TrimSpace(policy.RepoPath) != "" {
			return strings.TrimSpace(policy.RepoPath), nil
		}
	}
	if strings.TrimSpace(history.CommandID) != "" {
		var command db.AgentCommand
		if err := database.DB.First(&command, "id = ?", history.CommandID).Error; err == nil && strings.TrimSpace(command.PolicyID) != "" {
			var policy db.BackupPolicy
			if err := database.DB.First(&policy, "id = ?", command.PolicyID).Error; err == nil && strings.TrimSpace(policy.RepoPath) != "" {
				return strings.TrimSpace(policy.RepoPath), nil
			}
		}
	}
	var latest db.BackupPolicy
	if err := database.DB.Where("agent_id = ? AND storage_id = ?", history.AgentID, history.StorageID).Order("updated_at DESC").First(&latest).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(latest.RepoPath) == "" {
		return "", fmt.Errorf("repo path unavailable")
	}
	return strings.TrimSpace(latest.RepoPath), nil
}

func writeArtifactRcloneConf(path string, rcloneType string, config map[string]string) error {
	var builder strings.Builder
	builder.WriteString("[vaultfleet]\n")
	builder.WriteString("type = ")
	builder.WriteString(rcloneType)
	builder.WriteString("\n")
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if rcloneType == "s3" && key == "bucket" {
			continue
		}
		value, err := rcloneobscure.ConfigValue(key, config[key], false)
		if err != nil {
			return err
		}
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func candidateArtifactPaths(database *db.Database, history *db.TaskHistory, artifactName string) []string {
	seen := map[string]struct{}{}
	appendUnique := func(paths []string, candidate string) []string {
		if strings.TrimSpace(candidate) == "" {
			return paths
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			return paths
		}
		seen[candidate] = struct{}{}
		return append(paths, candidate)
	}

	paths := make([]string, 0, 6)
	if filepath.IsAbs(history.ArtifactPath) {
		paths = appendUnique(paths, history.ArtifactPath)
	}
	if database != nil && database.DataDir != "" {
		paths = appendUnique(paths, filepath.Join(database.DataDir, filepath.FromSlash(history.ArtifactPath)))
		paths = appendUnique(paths, filepath.Join(database.DataDir, "artifacts", artifactName))
		paths = appendUnique(paths, filepath.Join(database.DataDir, "artifacts", safeArtifactPathComponent(history.AgentID), artifactName))
	}
	paths = appendUnique(paths, filepath.Join("/etc/vaultfleet/artifacts", artifactName))
	paths = appendUnique(paths, filepath.Join("/etc/vaultfleet/artifacts", safeArtifactPathComponent(history.AgentID), artifactName))
	return paths
}

func (h *TaskHandler) commandService() *commands.Service {
	if h.Commands != nil {
		return h.Commands
	}
	return commands.NewService(h.DB, h.Hub)
}

func contextFromGin(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

func (h *TaskHandler) List(c *gin.Context) {
	limit := parseTaskLimit(c.Query("limit"))
	query := h.DB.DB.Order("created_at DESC").Limit(limit)
	if agentID := c.Query("agent_id"); agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}
	if taskType := c.Query("type"); taskType != "" {
		query = query.Where("type = ?", taskType)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	var histories []db.TaskHistory
	if err := query.Find(&histories).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"ok": true, "data": []taskResponse{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	responses := make([]taskResponse, 0, len(histories))
	for _, history := range histories {
		response := newTaskResponse(history)
		if h.ProgressGetter != nil && taskCanIncludeProgress(history) {
			response.Progress = h.ProgressGetter(history.AgentID, history.MessageID)
		}
		responses = append(responses, response)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": responses})
}

func taskCanIncludeProgress(history db.TaskHistory) bool {
	if history.Type != "backup" {
		return false
	}
	if history.MessageID == "" {
		return false
	}
	return history.Status == commands.TaskStatusRunning || history.Status == commands.TaskStatusPending
}

func parseTaskLimit(raw string) int {
	if raw == "" {
		return defaultTaskListLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultTaskListLimit
	}
	if limit > maxTaskListLimit {
		return maxTaskListLimit
	}
	return limit
}

func verificationTimeoutHours(timeoutMinutes int) int {
	if timeoutMinutes <= 0 {
		return 1
	}
	hours := timeoutMinutes / 60
	if timeoutMinutes%60 != 0 {
		hours++
	}
	if hours < 1 {
		return 1
	}
	return hours
}

func newTaskResponse(history db.TaskHistory) taskResponse {
	response := taskResponse{
		ID:                  history.ID,
		AgentID:             history.AgentID,
		Type:                history.Type,
		Status:              history.Status,
		SnapshotID:          history.SnapshotID,
		ArtifactPath:        history.ArtifactPath,
		ArtifactName:        history.ArtifactName,
		ArtifactSize:        history.ArtifactSize,
		ArtifactContentType: history.ArtifactContentType,
		BackupMode:          history.BackupMode,
		ArchiveFormat:       history.ArchiveFormat,
		MessageID:           history.MessageID,
		CommandID:           history.CommandID,
		PolicyID:            history.PolicyID,
		StorageID:           history.StorageID,
		StartedAt:           history.StartedAt,
		FinishedAt:          history.FinishedAt,
		DurationMs:          history.DurationMs,
		RepoSize:            history.RepoSize,
		RepoSizeBytes:       history.RepoSize,
		ErrorLog:            history.ErrorLog,
		Error:               history.ErrorLog,
		CreatedAt:           history.CreatedAt,
		UpdatedAt:           history.UpdatedAt,
	}
	if history.Docker != "" {
		var metadata protocol.DockerBackupMetadata
		if err := json.Unmarshal([]byte(history.Docker), &metadata); err == nil {
			response.Docker = &metadata
		}
	}
	if history.Verification != "" {
		var verification protocol.BackupVerificationResult
		if err := json.Unmarshal([]byte(history.Verification), &verification); err == nil {
			response.Verification = &verification
		}
	}
	return response
}
