package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const snapshotRequestTimeout = 30 * time.Second

type SnapshotHandler struct {
	DB       *db.Database
	Hub      SnapshotHub
	Commands *commands.Service

	timeout     time.Duration
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type SnapshotHub interface {
	IsOnline(agentID string) bool
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type TaskResultCommandCompleter interface {
	CompleteTaskResult(ctx context.Context, agentID string, messageID string, result protocol.TaskResultPayload) error
}

type taskResultCommandCompleterWithApply interface {
	CompleteTaskResultWith(ctx context.Context, agentID string, messageID string, result protocol.TaskResultPayload, apply func(*gorm.DB) error) (bool, error)
}

type SnapshotListCommandCompleter interface {
	CompleteSnapshotListWith(ctx context.Context, agentID string, messageID string, result protocol.SnapshotListRespPayload, apply func(*gorm.DB) error) (bool, error)
	FailCommandOfType(ctx context.Context, agentID string, messageID string, commandType string, errorText string) error
}

type snapshotResponse struct {
	ID         string                         `json:"id"`
	SnapshotID string                         `json:"snapshot_id"`
	Timestamp  time.Time                      `json:"timestamp"`
	Time       time.Time                      `json:"time"`
	Paths      []string                       `json:"paths"`
	Hostname   string                         `json:"hostname"`
	Username   string                         `json:"username"`
	Size       int64                          `json:"size"`
	Docker     *protocol.DockerBackupMetadata `json:"docker,omitempty"`
}

type snapshotRefreshResponse struct {
	Count     int                `json:"count"`
	Snapshots []snapshotResponse `json:"snapshots"`
}

func NewSnapshotHandler(database *db.Database, hub SnapshotHub) *SnapshotHandler {
	handler := &SnapshotHandler{
		DB:      database,
		Hub:     hub,
		timeout: snapshotRequestTimeout,
	}
	handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		return handler.Hub.SendAndWait(agentID, msg, timeout)
	}
	return handler
}

func RegisterSnapshotRoutes(rg *gin.RouterGroup, h *SnapshotHandler) {
	rg.GET("/agents/:id/snapshots", h.ListSnapshots)
	rg.POST("/agents/:id/snapshots/refresh", h.RefreshSnapshots)
}

func (h *SnapshotHandler) ListSnapshots(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}

	snapshots, ok := h.loadSnapshotResponses(c, agentID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": snapshots})
}

func (h *SnapshotHandler) RefreshSnapshots(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	commandService := h.Commands
	if commandService == nil {
		writeErrorResponse(c, http.StatusInternalServerError, "command service not configured")
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: agentID})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode snapshot list request")
		return
	}
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID: agentID,
		Type:    protocol.TypeSnapshotListReq,
		Message: *msg,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeDataResponse(c, http.StatusAccepted, gin.H{
			"command_id": command.ID,
			"message_id": msg.ID,
		})
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		writeDataResponse(c, http.StatusAccepted, gin.H{
			"command_id": command.ID,
			"message_id": msg.ID,
		})
		return
	}

	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		_ = commandService.RecordDispatchFailure(context.Background(), command.ID, err)
		writeDataResponse(c, http.StatusAccepted, gin.H{
			"command_id": command.ID,
			"message_id": msg.ID,
		})
		return
	}
	if err := commandService.RecordDispatchSuccess(context.Background(), command.ID); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	select {
	case resp, ok := <-respCh:
		h.writeSnapshotRefreshResponse(c, commandService, agentID, msg.ID, resp, ok)
		return
	default:
	}

	select {
	case resp, ok := <-respCh:
		h.writeSnapshotRefreshResponse(c, commandService, agentID, msg.ID, resp, ok)
	case <-c.Request.Context().Done():
		go h.drainSnapshotRefreshResponse(respCh, commandService, agentID, msg.ID)
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func (h *SnapshotHandler) writeSnapshotRefreshResponse(c *gin.Context, commandService *commands.Service, agentID string, messageID string, resp protocol.Message, ok bool) {
	if !ok {
		_ = commandService.TimeoutCommand(context.Background(), agentID, messageID)
		writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
		return
	}
	payload, status, message := h.completeSnapshotRefreshResponse(context.Background(), commandService, agentID, messageID, resp)
	if status != http.StatusOK {
		writeErrorResponse(c, status, message)
		return
	}
	snapshots, loaded := h.loadSnapshotResponses(c, agentID)
	if !loaded {
		return
	}
	writeDataResponse(c, http.StatusOK, snapshotRefreshResponse{Count: len(payload.Snapshots), Snapshots: snapshots})
}

func (h *SnapshotHandler) drainSnapshotRefreshResponse(respCh <-chan protocol.Message, commandService *commands.Service, agentID string, messageID string) {
	resp, ok := <-respCh
	if !ok {
		_ = commandService.TimeoutCommand(context.Background(), agentID, messageID)
		return
	}
	_, _, _ = h.completeSnapshotRefreshResponse(context.Background(), commandService, agentID, messageID, resp)
}

func (h *SnapshotHandler) completeSnapshotRefreshResponse(ctx context.Context, commandService *commands.Service, agentID string, messageID string, resp protocol.Message) (*protocol.SnapshotListRespPayload, int, string) {
	if resp.Type != protocol.TypeSnapshotListResp {
		_ = commandService.FailCommandOfType(ctx, agentID, messageID, protocol.TypeSnapshotListReq, "invalid agent response")
		return nil, http.StatusBadGateway, "invalid agent response"
	}
	payload, err := protocol.ParsePayload[protocol.SnapshotListRespPayload](&resp)
	if err != nil {
		_ = commandService.FailCommandOfType(ctx, agentID, messageID, protocol.TypeSnapshotListReq, "invalid agent response")
		return nil, http.StatusBadGateway, "invalid agent response"
	}
	if payload.Error != "" {
		_ = commandService.FailCommandOfType(ctx, agentID, messageID, protocol.TypeSnapshotListReq, payload.Error)
		return nil, http.StatusBadGateway, payload.Error
	}
	completed, err := commandService.CompleteSnapshotListWith(ctx, agentID, messageID, *payload, func(tx *gorm.DB) error {
		return upsertSnapshotsDB(tx, agentID, payload.Snapshots)
	})
	if err != nil {
		_ = commandService.FailCommandOfType(ctx, agentID, messageID, protocol.TypeSnapshotListReq, "database error")
		return nil, http.StatusInternalServerError, "database error"
	}
	if !completed {
		return nil, http.StatusGatewayTimeout, "command is no longer active"
	}
	return payload, http.StatusOK, ""
}

func (h *SnapshotHandler) loadSnapshotResponses(c *gin.Context, agentID string) ([]snapshotResponse, bool) {
	var snapshots []db.Snapshot
	if err := h.DB.DB.Where("agent_id = ?", agentID).Order("timestamp DESC").Find(&snapshots).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return nil, false
	}

	responses := make([]snapshotResponse, 0, len(snapshots))
	for _, snapshot := range snapshots {
		response, err := newSnapshotResponse(snapshot)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "decode snapshot")
			return nil, false
		}
		dockerMetadata, err := h.loadSnapshotDockerMetadata(c, agentID, snapshot.SnapshotID)
		if err != nil {
			writeErrorResponse(c, http.StatusInternalServerError, "decode docker metadata")
			return nil, false
		}
		response.Docker = dockerMetadata
		responses = append(responses, response)
	}
	return responses, true
}

func (h *SnapshotHandler) loadSnapshotDockerMetadata(c *gin.Context, agentID string, snapshotID string) (*protocol.DockerBackupMetadata, error) {
	var history db.TaskHistory
	err := h.DB.DB.
		Where("agent_id = ? AND type = ? AND status = ? AND snapshot_id = ? AND docker <> ?", agentID, "backup", commands.TaskStatusSuccess, snapshotID, "").
		Order("finished_at DESC, created_at DESC").
		First(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metadata protocol.DockerBackupMetadata
	if err := json.Unmarshal([]byte(history.Docker), &metadata); err != nil {
		return nil, nil
	}
	return &metadata, nil
}

func newSnapshotResponse(snapshot db.Snapshot) (snapshotResponse, error) {
	var paths []string
	if snapshot.Paths != "" {
		if err := json.Unmarshal([]byte(snapshot.Paths), &paths); err != nil {
			return snapshotResponse{}, err
		}
	}
	return snapshotResponse{
		ID:         snapshot.SnapshotID,
		SnapshotID: snapshot.SnapshotID,
		Timestamp:  snapshot.Timestamp,
		Time:       snapshot.Timestamp,
		Paths:      paths,
		Hostname:   "",
		Username:   "",
		Size:       snapshot.Size,
	}, nil
}

func NewTaskResultProcessor(database *db.Database, completer ...TaskResultCommandCompleter) func(agentID string, msg protocol.Message) error {
	return func(agentID string, msg protocol.Message) error {
		result, err := protocol.ParsePayload[protocol.TaskResultPayload](&msg)
		if err != nil {
			return err
		}
		if len(completer) > 0 && completer[0] != nil {
			if withApply, ok := completer[0].(taskResultCommandCompleterWithApply); ok {
				completed, err := withApply.CompleteTaskResultWith(context.Background(), agentID, msg.ID, *result, func(tx *gorm.DB) error {
					return upsertCommandLinkedBackupSnapshotsDB(tx, agentID, msg.ID, *result)
				})
				if err != nil {
					return err
				}
				if completed || commandExistsByMessage(database, agentID, msg.ID) {
					return nil
				}
			} else if err := completer[0].CompleteTaskResult(context.Background(), agentID, msg.ID, *result); err != nil {
				return err
			} else if commandExistsByMessage(database, agentID, msg.ID) {
				return nil
			}
		}
		return recordTaskResult(database, agentID, msg.ID, *result)
	}
}

func commandExistsByMessage(database *db.Database, agentID string, messageID string) bool {
	if database == nil || database.DB == nil || messageID == "" {
		return false
	}
	query := database.DB.Model(&db.AgentCommand{}).Where("message_id = ?", messageID)
	if agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func NewSnapshotListResponseProcessor(database *db.Database, completer SnapshotListCommandCompleter) func(agentID string, msg protocol.Message) error {
	return func(agentID string, msg protocol.Message) error {
		payload, err := protocol.ParsePayload[protocol.SnapshotListRespPayload](&msg)
		if err != nil {
			return err
		}
		if payload.Error != "" {
			if completer == nil {
				return nil
			}
			return completer.FailCommandOfType(context.Background(), agentID, msg.ID, protocol.TypeSnapshotListReq, payload.Error)
		}
		if completer == nil {
			return upsertSnapshots(database, agentID, payload.Snapshots)
		}
		_, err = completer.CompleteSnapshotListWith(context.Background(), agentID, msg.ID, *payload, func(tx *gorm.DB) error {
			return upsertSnapshotsDB(tx, agentID, payload.Snapshots)
		})
		if err != nil {
			_ = completer.FailCommandOfType(context.Background(), agentID, msg.ID, protocol.TypeSnapshotListReq, "database error")
			return err
		}
		return nil
	}
}

func upsertCommandLinkedBackupSnapshots(database *db.Database, agentID string, messageID string, result protocol.TaskResultPayload) error {
	if result.TaskType != "backup" || result.Status != "success" || len(result.Snapshots) == 0 || messageID == "" {
		return nil
	}
	if database == nil || database.DB == nil {
		return errors.New("database not configured")
	}
	if agentID == "" {
		agentID = result.AgentID
	}
	linked, err := commandLinkedTaskHistoryExists(database.DB, agentID, messageID)
	if err != nil || !linked {
		return err
	}
	return upsertSnapshotsDB(database.DB, agentID, result.Snapshots)
}

func upsertCommandLinkedBackupSnapshotsDB(gormDB *gorm.DB, agentID string, messageID string, result protocol.TaskResultPayload) error {
	if result.TaskType != "backup" || result.Status != "success" || len(result.Snapshots) == 0 || messageID == "" {
		return nil
	}
	if gormDB == nil {
		return errors.New("database not configured")
	}
	if agentID == "" {
		agentID = result.AgentID
	}
	linked, err := commandLinkedTaskHistoryExists(gormDB, agentID, messageID)
	if err != nil || !linked {
		return err
	}
	return upsertSnapshotsDB(gormDB, agentID, result.Snapshots)
}

func commandLinkedTaskHistoryExists(gormDB *gorm.DB, agentID string, messageID string) (bool, error) {
	var history db.TaskHistory
	err := gormDB.
		Where("agent_id = ? AND message_id = ? AND command_id <> ?", agentID, messageID, "").
		First(&history).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func recordTaskResult(database *db.Database, agentID string, messageID string, result protocol.TaskResultPayload) error {
	if database == nil || database.DB == nil {
		return errors.New("database not configured")
	}
	if agentID == "" {
		agentID = result.AgentID
	}
	localizedResult, err := localizeTaskResultArtifact(database, agentID, result)
	if err != nil {
		return err
	}
	if messageID != "" {
		linked, err := commandLinkedTaskHistoryExists(database.DB, agentID, messageID)
		if err != nil {
			return err
		}
		if linked {
			return nil
		}
	}
	if localizedResult.TaskType == "restore" {
		return completeRestoreTaskResult(database, agentID, messageID, localizedResult)
	}
	if localizedResult.TaskType == "backup" && localizedResult.Status == "success" && len(localizedResult.Snapshots) > 0 {
		return database.DB.Transaction(func(tx *gorm.DB) error {
			if err := createTaskHistory(tx, agentID, messageID, localizedResult); err != nil {
				return err
			}
			return upsertSnapshotsDB(tx, agentID, localizedResult.Snapshots)
		})
	}
	return createTaskHistory(database.DB, agentID, messageID, localizedResult)
}

func localizeTaskResultArtifact(database *db.Database, agentID string, result protocol.TaskResultPayload) (protocol.TaskResultPayload, error) {
	if database == nil {
		return result, errors.New("database not configured")
	}
	if !strings.EqualFold(strings.TrimSpace(result.BackupMode), protocol.BackupModeArchive) {
		return result, nil
	}
	if result.ArtifactPath == "" {
		return result, nil
	}

	sourcePath := filepath.Clean(result.ArtifactPath)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return result, fmt.Errorf("stat artifact: %w", err)
	}
	if info.IsDir() {
		return result, fmt.Errorf("artifact path is directory: %s", sourcePath)
	}

	artifactName := strings.TrimSpace(result.ArtifactName)
	if artifactName == "" {
		artifactName = filepath.Base(sourcePath)
		result.ArtifactName = artifactName
	}
	artifactDir := filepath.Join(database.DataDir, "artifacts", safeArtifactPathComponent(agentID))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return result, fmt.Errorf("create artifact dir: %w", err)
	}
	destPath := filepath.Join(artifactDir, artifactName)
	if err := copyFile(sourcePath, destPath); err != nil {
		return result, fmt.Errorf("copy artifact: %w", err)
	}
	relPath, err := filepath.Rel(database.DataDir, destPath)
	if err != nil {
		return result, fmt.Errorf("relativize artifact: %w", err)
	}
	result.ArtifactPath = filepath.ToSlash(relPath)
	result.ArtifactSize = info.Size()
	return result, nil
}

func safeArtifactPathComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown-agent"
	}
	replacer := strings.NewReplacer("/", "_", `\\`, "_", ":", "_", "..", "_")
	cleaned := replacer.Replace(trimmed)
	if cleaned == "" {
		return "unknown-agent"
	}
	return cleaned
}

func copyFile(sourcePath string, destPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}
	if err := destFile.Sync(); err != nil {
		return err
	}
	return destFile.Close()
}

func createTaskHistory(gormDB *gorm.DB, agentID string, messageID string, result protocol.TaskResultPayload) error {
	startedAt := result.StartedAt
	finishedAt := result.FinishedAt
	rawDocker, err := marshalDockerBackupMetadata(result.Docker)
	if err != nil {
		return err
	}
	rawDatabase, err := marshalDatabaseBackupMetadata(result.Database)
	if err != nil {
		return err
	}
	rawVerification, err := marshalBackupVerificationResult(result.Verification)
	if err != nil {
		return err
	}
	rawManifest, err := marshalBackupContentManifest(result.Manifest)
	if err != nil {
		return err
	}
	history := db.TaskHistory{
		AgentID:             agentID,
		Type:                result.TaskType,
		Status:              result.Status,
		SnapshotID:          result.SnapshotID,
		ArtifactPath:        result.ArtifactPath,
		ArtifactName:        result.ArtifactName,
		ArtifactSize:        result.ArtifactSize,
		ArtifactContentType: result.ArtifactContentType,
		BackupMode:          result.BackupMode,
		ArchiveFormat:       result.ArchiveFormat,
		MessageID:           messageID,
		Docker:              rawDocker,
		Database:            rawDatabase,
		Verification:        rawVerification,
		Manifest:            rawManifest,
		StartedAt:           &startedAt,
		FinishedAt:          &finishedAt,
		DurationMs:          result.DurationMs,
		RepoSize:            result.RepoSize,
		ErrorLog:            result.ErrorLog,
	}
	if result.StartedAt.IsZero() {
		history.StartedAt = nil
	}
	if result.FinishedAt.IsZero() {
		history.FinishedAt = nil
	}
	return gormDB.Create(&history).Error
}

func marshalBackupVerificationResult(result *protocol.BackupVerificationResult) (string, error) {
	if result == nil {
		return "", nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal verification result: %w", err)
	}
	return string(raw), nil
}

func marshalBackupContentManifest(manifest *protocol.BackupContentManifest) (string, error) {
	if manifest == nil {
		return "", nil
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshal backup content manifest: %w", err)
	}
	return string(raw), nil
}

func marshalDockerBackupMetadata(metadata *protocol.DockerBackupMetadata) (string, error) {
	if metadata == nil {
		return "", nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal docker metadata: %w", err)
	}
	return string(raw), nil
}

func marshalDatabaseBackupMetadata(metadata *protocol.DatabaseBackupMetadata) (string, error) {
	if metadata == nil {
		return "", nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal database metadata: %w", err)
	}
	return string(raw), nil
}

func completeRestoreTaskResult(database *db.Database, agentID string, messageID string, result protocol.TaskResultPayload) error {
	startedAt := result.StartedAt
	finishedAt := result.FinishedAt
	if messageID != "" {
		var history db.TaskHistory
		err := database.DB.
			Where("agent_id = ? AND type = ? AND status = ? AND message_id = ?", agentID, "restore", "running", messageID).
			First(&history).Error
		if err == nil {
			history.Status = result.Status
			history.DurationMs = result.DurationMs
			history.RepoSize = result.RepoSize
			history.ErrorLog = result.ErrorLog
			if result.SnapshotID != "" {
				history.SnapshotID = result.SnapshotID
			}
			if !result.StartedAt.IsZero() {
				history.StartedAt = &startedAt
			}
			if !result.FinishedAt.IsZero() {
				history.FinishedAt = &finishedAt
			}
			return database.DB.Save(&history).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	history := db.TaskHistory{
		AgentID:             agentID,
		Type:                result.TaskType,
		Status:              result.Status,
		SnapshotID:          result.SnapshotID,
		ArtifactPath:        result.ArtifactPath,
		ArtifactName:        result.ArtifactName,
		ArtifactSize:        result.ArtifactSize,
		ArtifactContentType: result.ArtifactContentType,
		BackupMode:          result.BackupMode,
		ArchiveFormat:       result.ArchiveFormat,
		MessageID:           messageID,
		StartedAt:           &startedAt,
		FinishedAt:          &finishedAt,
		DurationMs:          result.DurationMs,
		RepoSize:            result.RepoSize,
		ErrorLog:            result.ErrorLog,
	}
	if result.StartedAt.IsZero() {
		history.StartedAt = nil
	}
	if result.FinishedAt.IsZero() {
		history.FinishedAt = nil
	}
	return database.DB.Create(&history).Error
}

func upsertSnapshots(database *db.Database, agentID string, snapshots []protocol.SnapshotInfo) error {
	if database == nil || database.DB == nil {
		return errors.New("database not configured")
	}
	return upsertSnapshotsDB(database.DB, agentID, snapshots)
}

func upsertSnapshotsDB(gormDB *gorm.DB, agentID string, snapshots []protocol.SnapshotInfo) error {
	for _, snapshotInfo := range snapshots {
		if snapshotInfo.ID == "" {
			continue
		}
		paths, err := json.Marshal(snapshotInfo.Paths)
		if err != nil {
			return err
		}

		snapshot := db.Snapshot{
			AgentID:    agentID,
			SnapshotID: snapshotInfo.ID,
			Timestamp:  snapshotInfo.Time,
			Paths:      string(paths),
			Size:       snapshotInfo.Size,
		}
		err = gormDB.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "agent_id"},
				{Name: "snapshot_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"timestamp", "paths", "size"}),
		}).Create(&snapshot).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func agentExistsByID(c *gin.Context, database *db.Database, agentID string) bool {
	var agent db.Agent
	if err := database.DB.First(&agent, "id = ?", agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "agent not found")
			return false
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return false
	}
	return true
}
