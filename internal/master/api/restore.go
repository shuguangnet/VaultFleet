package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

type RestoreHandler struct {
	DB          *db.Database
	Hub         RestoreHub
	Commands    *commands.Service
	timeout     time.Duration
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type RestoreHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type restorePreflightHub interface {
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type restoreRequest struct {
	SnapshotID      string                         `json:"snapshot_id" binding:"required"`
	SourceAgentID   string                         `json:"source_agent_id"`
	TargetPath      string                         `json:"target_path"`
	Target          string                         `json:"target"`
	IncludePaths    []string                       `json:"include_paths"`
	RestoreMode     string                         `json:"restore_mode"`
	Docker          *protocol.DockerRestoreRequest `json:"docker"`
	DockerSourceID  string                         `json:"docker_source_id"`
	DockerSourceIDs []string                       `json:"docker_source_ids"`
}

func NewRestoreHandler(database *db.Database, hub RestoreHub) *RestoreHandler {
	handler := &RestoreHandler{DB: database, Hub: hub, timeout: 30 * time.Second}
	if waitHub, ok := hub.(restorePreflightHub); ok {
		handler.sendAndWait = waitHub.SendAndWait
	}
	return handler
}

func RegisterRestoreRoutes(rg *gin.RouterGroup, h *RestoreHandler) {
	rg.POST("/agents/:id/restore", h.Restore)
	rg.POST("/agents/:id/restore/preflight", h.Preflight)
}

func (h *RestoreHandler) Preflight(c *gin.Context) {
	targetAgentID := c.Param("id")
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
	sourceAgentID := strings.TrimSpace(request.SourceAgentID)
	if sourceAgentID == "" {
		sourceAgentID = targetAgentID
	}
	checks := []protocol.RestorePreflightCheck{}

	if exists, err := h.agentExists(contextFromGin(c), targetAgentID); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	} else if !exists {
		checks = append(checks, restorePreflightCheck("target_agent_exists", protocol.RestorePreflightSeverityError, "target agent not found", targetAgentID))
		h.writePreflightReport(c, "", checks)
		return
	}
	checks = append(checks, restorePreflightCheck("target_agent_exists", protocol.RestorePreflightSeverityInfo, "target agent exists", targetAgentID))

	if sourceExists, err := h.agentExists(contextFromGin(c), sourceAgentID); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	} else if !sourceExists {
		checks = append(checks, restorePreflightCheck("source_agent_exists", protocol.RestorePreflightSeverityError, "source agent not found", sourceAgentID))
		h.writePreflightReport(c, "", checks)
		return
	}
	checks = append(checks, restorePreflightCheck("source_agent_exists", protocol.RestorePreflightSeverityInfo, "source agent exists", sourceAgentID))

	if h.Hub == nil || !h.Hub.IsOnline(targetAgentID) {
		checks = append(checks, restorePreflightCheck("target_agent_online", protocol.RestorePreflightSeverityError, "target agent is offline", targetAgentID))
		h.writePreflightReport(c, "", checks)
		return
	}
	checks = append(checks, restorePreflightCheck("target_agent_online", protocol.RestorePreflightSeverityInfo, "target agent is online", targetAgentID))

	supportsPreflight, err := agentHasCapability(h.DB, targetAgentID, protocol.CapabilityRestorePreflight)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if !supportsPreflight {
		checks = append(checks, restorePreflightCheck("restore_preflight_capability", protocol.RestorePreflightSeverityError, "target agent does not support restore preflight; upgrade the Agent", targetAgentID))
		h.writePreflightReport(c, "", checks)
		return
	}
	checks = append(checks, restorePreflightCheck("restore_preflight_capability", protocol.RestorePreflightSeverityInfo, "target agent supports restore preflight", targetAgentID))

	snapshotID, found, err := h.resolveKnownSnapshotID(contextFromGin(c), sourceAgentID, request.SnapshotID)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if !found {
		checks = append(checks, restorePreflightCheck("source_snapshot_found", protocol.RestorePreflightSeverityError, "source snapshot not found", request.SnapshotID))
		h.writePreflightReport(c, "", checks)
		return
	}
	checks = append(checks, restorePreflightCheck("source_snapshot_found", protocol.RestorePreflightSeverityInfo, "source snapshot found", snapshotID))

	dockerRestore := restoreMode == protocol.RestoreModeDockerContainer || request.Docker != nil
	includePaths := append([]string(nil), request.IncludePaths...)
	var dockerRequest *protocol.DockerRestoreRequest
	if dockerRestore {
		supportsDockerRestore, err := agentHasCapability(h.DB, targetAgentID, protocol.CapabilityDockerContainerRestore)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		if !supportsDockerRestore {
			checks = append(checks, restorePreflightCheck("docker_restore_capability", protocol.RestorePreflightSeverityError, "target agent does not support Docker container restore", targetAgentID))
		} else {
			checks = append(checks, restorePreflightCheck("docker_restore_capability", protocol.RestorePreflightSeverityInfo, "target agent supports Docker container restore", targetAgentID))
		}
		resolvedDocker, err := h.resolveDockerRestoreRequest(c, sourceAgentID, snapshotID, request)
		if err != nil {
			checks = append(checks, restorePreflightCheck("docker_metadata", protocol.RestorePreflightSeverityError, err.Error(), snapshotID))
		} else {
			dockerRequest = &resolvedDocker
			checks = append(checks, h.dockerBatchCapabilityChecks(targetAgentID, resolvedDocker)...)
			checks = append(checks, dockerBatchConflictChecks(resolvedDocker)...)
			dockerPaths := dockerRestoreIncludePaths(resolvedDocker)
			if len(dockerPaths) == 0 {
				checks = append(checks, restorePreflightCheck("docker_restore_paths", protocol.RestorePreflightSeverityError, "docker metadata has no restore paths", snapshotID))
			} else {
				includePaths = appendUniqueRestoreStrings(includePaths, dockerPaths...)
				checks = append(checks, restorePreflightCheck("docker_metadata", protocol.RestorePreflightSeverityInfo, "docker metadata found", snapshotID))
			}
		}
		targetPath = "/"
	} else if strings.TrimSpace(targetPath) == "" {
		checks = append(checks, restorePreflightCheck("target_path_required", protocol.RestorePreflightSeverityError, "target path is required for file restore", ""))
	}

	if len(includePaths) > 0 {
		supportsSelectiveRestore, err := agentHasCapability(h.DB, targetAgentID, protocol.CapabilityRestoreIncludePaths)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "database error")
			return
		}
		if !supportsSelectiveRestore {
			checks = append(checks, restorePreflightCheck("restore_include_paths_capability", protocol.RestorePreflightSeverityError, "target agent does not support selective restore", targetAgentID))
		} else {
			checks = append(checks, restorePreflightCheck("restore_include_paths_capability", protocol.RestorePreflightSeverityInfo, "target agent supports selective restore", targetAgentID))
		}
	}

	if restorePreflightHasError(checks) {
		h.writePreflightReport(c, snapshotID, checks)
		return
	}

	agentChecks := h.runAgentRestorePreflight(c, targetAgentID, protocol.RestorePreflightReqPayload{
		AgentID:      targetAgentID,
		SnapshotID:   snapshotID,
		Target:       targetPath,
		IncludePaths: includePaths,
		RestoreMode:  restoreMode,
		Docker:       dockerRequest,
	})
	checks = append(checks, agentChecks...)
	h.writePreflightReport(c, snapshotID, checks)
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
	sourceAgentID := strings.TrimSpace(request.SourceAgentID)
	if sourceAgentID == "" {
		sourceAgentID = agentID
	}
	if sourceAgentID != agentID && !agentExistsByID(c, h.DB, sourceAgentID) {
		return
	}
	snapshotID, ok := h.resolveSnapshotID(c, sourceAgentID, request.SnapshotID)
	if !ok {
		return
	}
	restorePolicy, err := h.restoreSourcePolicy(contextFromGin(c), sourceAgentID, snapshotID)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "resolve source backup configuration")
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
		resolvedDocker, err := h.resolveDockerRestoreRequest(c, sourceAgentID, snapshotID, request)
		if err != nil {
			writeErrorResponse(c, http.StatusBadRequest, err.Error())
			return
		}
		dockerRequest = &resolvedDocker
		if len(resolvedDocker.Sources) > 1 {
			supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerMultiContainerRestore)
			if err != nil {
				writeErrorResponse(c, http.StatusInternalServerError, "database error")
				return
			}
			if !supported {
				writeErrorResponse(c, http.StatusBadRequest, "agent does not support multi-container restore; upgrade the Agent")
				return
			}
		}
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
		SnapshotID:    snapshotID,
		SourceAgentID: sourceAgentID,
		Target:        targetPath,
		IncludePaths:  includePaths,
		RestoreMode:   restoreMode,
		Docker:        dockerRequest,
		Policy:        restorePolicy,
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

func (h *RestoreHandler) restoreSourcePolicy(ctx context.Context, sourceAgentID string, snapshotID string) (*protocol.PolicyPushPayload, error) {
	var history db.TaskHistory
	historyErr := h.DB.DB.WithContext(ctx).
		Where("agent_id = ? AND type = ? AND status = ? AND snapshot_id = ?", sourceAgentID, "backup", commands.TaskStatusSuccess, snapshotID).
		Order("finished_at DESC, created_at DESC").
		First(&history).Error

	var policy db.BackupPolicy
	if historyErr == nil && strings.TrimSpace(history.PolicyID) != "" {
		if err := h.DB.DB.WithContext(ctx).First(&policy, "id = ?", history.PolicyID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
	} else {
		if historyErr != nil && !errors.Is(historyErr, gorm.ErrRecordNotFound) {
			return nil, historyErr
		}
		if err := h.DB.DB.WithContext(ctx).Where("agent_id = ?", sourceAgentID).First(&policy).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
	}

	storageID := policy.StorageID
	if historyErr == nil && strings.TrimSpace(history.StorageID) != "" {
		storageID = history.StorageID
	}
	var storage db.StorageConfig
	if err := h.DB.DB.WithContext(ctx).First(&storage, "id = ?", storageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	payload, err := policyPushPayload(h.DB, policy, storage)
	if err != nil {
		return nil, err
	}
	if historyErr == nil {
		var manifest protocol.BackupContentManifest
		if strings.TrimSpace(history.Manifest) != "" && json.Unmarshal([]byte(history.Manifest), &manifest) == nil {
			if strings.TrimSpace(manifest.Policy.Repository) != "" {
				payload.Storage.RepoPath = strings.TrimSpace(manifest.Policy.Repository)
			}
			if strings.TrimSpace(manifest.Policy.BackupMode) != "" {
				payload.BackupMode = manifest.Policy.BackupMode
			}
			if strings.TrimSpace(manifest.Policy.ArchiveFormat) != "" {
				payload.ArchiveFormat = manifest.Policy.ArchiveFormat
			}
		}
		if strings.TrimSpace(history.BackupMode) != "" {
			payload.BackupMode = history.BackupMode
		}
		if strings.TrimSpace(history.ArchiveFormat) != "" {
			payload.ArchiveFormat = history.ArchiveFormat
		}
	}
	return &payload, nil
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
	sourceIDs, err := normalizedDockerSourceIDs(request)
	if err != nil {
		return protocol.DockerRestoreRequest{}, err
	}
	return filterDockerRestoreRequest(protocol.DockerRestoreRequest{Sources: metadata.Sources}, sourceIDs)
}

func (h *RestoreHandler) runAgentRestorePreflight(c *gin.Context, agentID string, payload protocol.RestorePreflightReqPayload) []protocol.RestorePreflightCheck {
	msg, err := protocol.NewMessage(protocol.TypeRestorePreflightReq, payload)
	if err != nil {
		return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_encode", protocol.RestorePreflightSeverityError, "encode restore preflight request failed", err.Error())}
	}
	wait := h.sendAndWait
	if wait == nil {
		if waitHub, ok := h.Hub.(restorePreflightHub); ok {
			wait = waitHub.SendAndWait
		}
	}
	if wait == nil {
		return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_dispatch", protocol.RestorePreflightSeverityError, "restore preflight is not available on this Master", "")}
	}
	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_dispatch", protocol.RestorePreflightSeverityError, "send restore preflight request failed", err.Error())}
	}
	select {
	case resp, ok := <-respCh:
		if !ok {
			return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_timeout", protocol.RestorePreflightSeverityError, "timeout waiting for target agent preflight response", "")}
		}
		if resp.Type != protocol.TypeRestorePreflightResp {
			return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_response", protocol.RestorePreflightSeverityError, "invalid agent preflight response type", resp.Type)}
		}
		agentPayload, err := protocol.ParsePayload[protocol.RestorePreflightRespPayload](&resp)
		if err != nil {
			return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_response", protocol.RestorePreflightSeverityError, "decode agent preflight response failed", err.Error())}
		}
		if agentPayload.Error != "" {
			return append(agentPayload.Checks, restorePreflightCheck("restore_preflight_agent_error", protocol.RestorePreflightSeverityError, "target agent reported restore preflight error", agentPayload.Error))
		}
		return agentPayload.Checks
	case <-c.Request.Context().Done():
		return []protocol.RestorePreflightCheck{restorePreflightCheck("restore_preflight_cancelled", protocol.RestorePreflightSeverityError, "restore preflight request was cancelled", "")}
	}
}

func (h *RestoreHandler) writePreflightReport(c *gin.Context, snapshotID string, checks []protocol.RestorePreflightCheck) {
	payload := protocol.RestorePreflightRespPayload{
		AgentID:    c.Param("id"),
		SnapshotID: snapshotID,
		Status:     restorePreflightStatus(checks),
		Checks:     checks,
	}
	writeDataResponse(c, http.StatusOK, payload)
}

func restorePreflightStatus(checks []protocol.RestorePreflightCheck) string {
	if restorePreflightHasError(checks) {
		return protocol.RestorePreflightStatusFailed
	}
	return protocol.RestorePreflightStatusPassed
}

func restorePreflightHasError(checks []protocol.RestorePreflightCheck) bool {
	for _, check := range checks {
		if check.Severity == protocol.RestorePreflightSeverityError {
			return true
		}
	}
	return false
}

func restorePreflightCheck(code string, severity string, message string, detail string) protocol.RestorePreflightCheck {
	return protocol.RestorePreflightCheck{
		Code:     code,
		Severity: severity,
		Message:  message,
		Detail:   detail,
	}
}

const maxDockerRestoreSources = 50

func normalizedDockerSourceIDs(request restoreRequest) ([]string, error) {
	values := request.DockerSourceIDs
	if len(values) == 0 && strings.TrimSpace(request.DockerSourceID) != "" {
		values = []string{request.DockerSourceID}
	}
	if len(values) == 0 {
		return nil, errors.New("docker source selection is required")
	}
	if len(values) > maxDockerRestoreSources {
		return nil, errors.New("docker source selection exceeds limit")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("docker source selection contains an empty id")
		}
		if _, ok := seen[value]; ok {
			return nil, errors.New("docker source selection contains duplicates")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func filterDockerRestoreRequest(request protocol.DockerRestoreRequest, sourceIDs []string) (protocol.DockerRestoreRequest, error) {
	if len(request.Sources) == 0 {
		return protocol.DockerRestoreRequest{}, errors.New("docker metadata has no sources")
	}
	if len(sourceIDs) == 0 {
		return protocol.DockerRestoreRequest{}, errors.New("docker source selection is required")
	}
	if len(sourceIDs) > maxDockerRestoreSources {
		return protocol.DockerRestoreRequest{}, errors.New("docker source selection exceeds limit")
	}
	selected := make(map[string]int, len(sourceIDs))
	for index, sourceID := range sourceIDs {
		selected[sourceID] = index
	}
	filtered := make([]protocol.DockerResolvedSource, 0, len(sourceIDs))
	found := make([]bool, len(sourceIDs))
	for _, source := range request.Sources {
		matchedIndex := -1
		for _, candidate := range dockerSourceIdentifiers(source) {
			index, ok := selected[candidate]
			if !ok {
				continue
			}
			if found[index] || matchedIndex >= 0 {
				return protocol.DockerRestoreRequest{}, errors.New("docker source selection resolves more than once")
			}
			matchedIndex = index
		}
		if matchedIndex >= 0 {
			filtered = append(filtered, source)
			found[matchedIndex] = true
		}
	}
	for _, ok := range found {
		if !ok {
			return protocol.DockerRestoreRequest{}, errors.New("docker source not found for snapshot")
		}
	}
	return protocol.DockerRestoreRequest{Sources: filtered}, nil
}

func (h *RestoreHandler) dockerBatchCapabilityChecks(agentID string, request protocol.DockerRestoreRequest) []protocol.RestorePreflightCheck {
	if len(request.Sources) <= 1 {
		return nil
	}
	supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerMultiContainerRestore)
	if err != nil {
		return []protocol.RestorePreflightCheck{restorePreflightCheck("docker_multi_restore_capability", protocol.RestorePreflightSeverityError, "check multi-container restore capability failed", err.Error())}
	}
	if !supported {
		return []protocol.RestorePreflightCheck{restorePreflightCheck("docker_multi_restore_capability", protocol.RestorePreflightSeverityError, "target agent does not support multi-container restore; upgrade the Agent", agentID)}
	}
	return []protocol.RestorePreflightCheck{restorePreflightCheck("docker_multi_restore_capability", protocol.RestorePreflightSeverityInfo, "target agent supports multi-container restore", agentID)}
}

func dockerBatchConflictChecks(request protocol.DockerRestoreRequest) []protocol.RestorePreflightCheck {
	type owner struct {
		id   string
		name string
	}
	seen := map[string]owner{}
	var checks []protocol.RestorePreflightCheck
	for _, source := range request.Sources {
		current := owner{id: protocol.DockerSourceID(source), name: protocol.DockerSourceName(source)}
		keys := []struct {
			kind  string
			value string
		}{
			{kind: "container name", value: strings.TrimSpace(source.Name)},
		}
		project := strings.TrimSpace(source.Compose.Project)
		service := strings.TrimSpace(source.Compose.Service)
		if project != "" && service != "" {
			keys = append(keys, struct{ kind, value string }{kind: "Compose service", value: project + "/" + service})
		}
		for _, path := range source.ResolvedPaths {
			keys = append(keys, struct{ kind, value string }{kind: "restore path", value: strings.TrimSpace(path)})
		}
		for _, port := range source.Ports {
			if strings.TrimSpace(port.HostPort) != "" {
				keys = append(keys, struct{ kind, value string }{kind: "host port", value: strings.TrimSpace(port.HostIP) + ":" + strings.TrimSpace(port.HostPort) + "/" + strings.TrimSpace(port.Protocol)})
			}
		}
		for _, key := range keys {
			if key.value == "" {
				continue
			}
			lookup := key.kind + "\x00" + key.value
			if previous, ok := seen[lookup]; ok && previous.id != current.id {
				check := restorePreflightCheck("docker_batch_conflict", protocol.RestorePreflightSeverityWarning, "selected Docker sources share a "+key.kind, key.value+" is also used by "+previous.name)
				check.SourceID = current.id
				check.SourceName = current.name
				checks = append(checks, check)
				continue
			}
			seen[lookup] = current
		}
	}
	return checks
}

func dockerSourceIdentifiers(source protocol.DockerResolvedSource) []string {
	values := []string{source.ContainerID, source.Selection.ContainerID, source.Name, source.Selection.Name}
	project := strings.TrimSpace(source.Compose.Project)
	if project == "" {
		project = strings.TrimSpace(source.Selection.ComposeProject)
	}
	service := strings.TrimSpace(source.Compose.Service)
	if service == "" {
		service = strings.TrimSpace(source.Selection.ComposeService)
	}
	if project != "" && service != "" {
		values = append(values, project+"/"+service)
	}
	return appendUniqueRestoreStrings(nil, values...)
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
	snapshotID, found, err := h.resolveSnapshotIDValue(contextFromGin(c), agentID, requestedID)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return "", false
	}
	if !found {
		return requestedID, true
	}
	return snapshotID, true
}

func (h *RestoreHandler) resolveSnapshotIDValue(ctx context.Context, agentID string, requestedID string) (string, bool, error) {
	var snapshot db.Snapshot
	err := h.DB.DB.WithContext(ctx).
		Where("agent_id = ? AND (id = ? OR snapshot_id = ?)", agentID, requestedID, requestedID).
		First(&snapshot).Error
	if err == nil {
		return snapshot.SnapshotID, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return "", false, err
}

func (h *RestoreHandler) resolveKnownSnapshotID(ctx context.Context, agentID string, requestedID string) (string, bool, error) {
	return h.resolveSnapshotIDValue(ctx, agentID, requestedID)
}

func (h *RestoreHandler) agentExists(ctx context.Context, agentID string) (bool, error) {
	var count int64
	if err := h.DB.DB.WithContext(ctx).Model(&db.Agent{}).Where("id = ?", agentID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *RestoreHandler) commandService() *commands.Service {
	if h.Commands != nil {
		return h.Commands
	}
	return commands.NewService(h.DB, h.Hub)
}
