package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

type RestoreHandler struct {
	DB       *db.Database
	Hub      RestoreHub
	Commands *commands.Service
}

type RestoreHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type restoreRequest struct {
	SnapshotID     string                         `json:"snapshot_id" binding:"required"`
	TargetPath     string                         `json:"target_path"`
	Target         string                         `json:"target"`
	IncludePaths   []string                       `json:"include_paths"`
	RestoreMode    string                         `json:"restore_mode"`
	Docker         *protocol.DockerRestoreRequest `json:"docker"`
	DockerSourceID string                         `json:"docker_source_id"`
}

func NewRestoreHandler(database *db.Database, hub RestoreHub) *RestoreHandler {
	return &RestoreHandler{DB: database, Hub: hub}
}

func RegisterRestoreRoutes(rg *gin.RouterGroup, h *RestoreHandler) {
	rg.POST("/agents/:id/restore", h.Restore)
}

func (h *RestoreHandler) Restore(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}

	var request restoreRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	targetPath := request.TargetPath
	if targetPath == "" {
		targetPath = request.Target
	}
	restoreMode := strings.TrimSpace(request.RestoreMode)
	if restoreMode == "" {
		restoreMode = protocol.RestoreModeFiles
	}
	dockerRestore := restoreMode == protocol.RestoreModeDockerContainer || request.Docker != nil
	if targetPath == "" && !dockerRestore {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	snapshotID, ok := h.resolveSnapshotID(c, agentID, request.SnapshotID)
	if !ok {
		return
	}

	var dockerRequest *protocol.DockerRestoreRequest
	includePaths := append([]string(nil), request.IncludePaths...)
	if dockerRestore {
		supportsDockerRestore, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerContainerRestore)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		if !supportsDockerRestore {
			writeErrorResponse(c, http.StatusBadRequest, "agent does not support Docker container restore")
			return
		}
		resolvedDocker, err := h.resolveDockerRestoreRequest(c, agentID, snapshotID, request)
		if err != nil {
			writeErrorResponse(c, http.StatusBadRequest, err.Error())
			return
		}
		dockerRequest = &resolvedDocker
		dockerPaths := dockerRestoreIncludePaths(resolvedDocker)
		if len(dockerPaths) == 0 {
			writeErrorResponse(c, http.StatusBadRequest, "docker metadata has no restore paths")
			return
		}
		includePaths = appendUniqueRestoreStrings(includePaths, dockerPaths...)
		targetPath = "/"
	}

	msgType := protocol.TypeRestoreReq
	if len(includePaths) > 0 {
		supportsSelectiveRestore, err := agentHasCapability(h.DB, agentID, protocol.CapabilityRestoreIncludePaths)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		if !supportsSelectiveRestore {
			writeErrorResponse(c, http.StatusBadRequest, "agent does not support selective restore")
			return
		}
		msgType = protocol.TypeSelectiveRestoreReq
	}

	msg, err := protocol.NewMessage(msgType, protocol.RestoreReqPayload{
		SnapshotID:   snapshotID,
		Target:       targetPath,
		IncludePaths: includePaths,
		RestoreMode:  restoreMode,
		Docker:       dockerRequest,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode restore request")
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
		AgentID:      agentID,
		Type:         msgType,
		Message:      *msg,
		TaskType:     "restore",
		TaskState:    commands.TaskStatusPending,
		SnapshotID:   snapshotID,
		PolicyID:     policyID,
		StorageID:    storageID,
		TimeoutHours: timeoutHours,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	responseMessage := "restore queued"
	if h.Hub != nil && h.Hub.IsOnline(agentID) {
		if err := commandService.DispatchNewPendingForAgent(contextFromGin(c), agentID, 100); err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		var dispatchedCommand db.AgentCommand
		if err := h.DB.DB.WithContext(contextFromGin(c)).First(&dispatchedCommand, "id = ?", command.ID).Error; err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		if dispatchedCommand.Status == commands.CommandStatusRunning {
			responseMessage = "restore started"
		}
	}

	writeDataResponse(c, http.StatusAccepted, gin.H{
		"message":    responseMessage,
		"command_id": command.ID,
		"message_id": msg.ID,
	})
}

func (h *RestoreHandler) resolveDockerRestoreRequest(c *gin.Context, agentID string, snapshotID string, request restoreRequest) (protocol.DockerRestoreRequest, error) {
	var history db.TaskHistory
	err := h.DB.DB.WithContext(contextFromGin(c)).
		Where("agent_id = ? AND type = ? AND status = ? AND snapshot_id = ? AND docker <> ?", agentID, "backup", commands.TaskStatusSuccess, snapshotID, "").
		Order("finished_at DESC, created_at DESC").
		First(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return protocol.DockerRestoreRequest{}, errors.New("docker metadata not found for snapshot")
	}
	if err != nil {
		return protocol.DockerRestoreRequest{}, errors.New("database error")
	}
	var metadata protocol.DockerBackupMetadata
	if err := json.Unmarshal([]byte(history.Docker), &metadata); err != nil {
		return protocol.DockerRestoreRequest{}, errors.New("decode docker metadata")
	}
	return filterDockerRestoreRequest(protocol.DockerRestoreRequest{Sources: metadata.Sources}, request.DockerSourceID)
}

func filterDockerRestoreRequest(request protocol.DockerRestoreRequest, sourceID string) (protocol.DockerRestoreRequest, error) {
	if strings.TrimSpace(sourceID) == "" {
		if len(request.Sources) == 0 {
			return protocol.DockerRestoreRequest{}, errors.New("docker metadata has no sources")
		}
		return request, nil
	}
	var filtered []protocol.DockerResolvedSource
	for _, source := range request.Sources {
		if source.ContainerID == sourceID || source.Name == sourceID || source.Selection.ContainerID == sourceID || source.Selection.Name == sourceID {
			filtered = append(filtered, source)
			continue
		}
		if source.Selection.ComposeProject != "" && source.Selection.ComposeService != "" && source.Selection.ComposeProject+"/"+source.Selection.ComposeService == sourceID {
			filtered = append(filtered, source)
		}
	}
	if len(filtered) == 0 {
		return protocol.DockerRestoreRequest{}, errors.New("docker source not found for snapshot")
	}
	return protocol.DockerRestoreRequest{Sources: filtered}, nil
}

func dockerRestoreIncludePaths(request protocol.DockerRestoreRequest) []string {
	var paths []string
	for _, source := range request.Sources {
		paths = append(paths, source.ResolvedPaths...)
	}
	return appendUniqueRestoreStrings(nil, paths...)
}

func appendUniqueRestoreStrings(values []string, more ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(more))
	result := make([]string, 0, len(values)+len(more))
	for _, value := range append(values, more...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (h *RestoreHandler) resolveSnapshotID(c *gin.Context, agentID string, requestedID string) (string, bool) {
	var snapshot db.Snapshot
	err := h.DB.DB.WithContext(contextFromGin(c)).
		Where("agent_id = ? AND (id = ? OR snapshot_id = ?)", agentID, requestedID, requestedID).
		First(&snapshot).Error
	if err == nil {
		return snapshot.SnapshotID, true
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return requestedID, true
	}
	writeErrorResponse(c, http.StatusInternalServerError, "database error")
	return "", false
}

func (h *RestoreHandler) commandService() *commands.Service {
	if h.Commands != nil {
		return h.Commands
	}
	return commands.NewService(h.DB, h.Hub)
}
