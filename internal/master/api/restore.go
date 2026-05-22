package api

import (
	"errors"
	"net/http"

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
	SnapshotID   string   `json:"snapshot_id" binding:"required"`
	TargetPath   string   `json:"target_path"`
	Target       string   `json:"target"`
	IncludePaths []string `json:"include_paths"`
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
	if targetPath == "" {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	snapshotID, ok := h.resolveSnapshotID(c, agentID, request.SnapshotID)
	if !ok {
		return
	}
	msgType := protocol.TypeRestoreReq
	if len(request.IncludePaths) > 0 {
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
		IncludePaths: request.IncludePaths,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode restore request")
		return
	}

	commandService := h.commandService()
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID:    agentID,
		Type:       msgType,
		Message:    *msg,
		TaskType:   "restore",
		TaskState:  commands.TaskStatusPending,
		SnapshotID: snapshotID,
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
