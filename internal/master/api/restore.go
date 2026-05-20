package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
	SnapshotID string `json:"snapshot_id" binding:"required"`
	TargetPath string `json:"target_path"`
	Target     string `json:"target"`
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

	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: request.SnapshotID,
		Target:     targetPath,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode restore request")
		return
	}

	commandService := h.commandService()
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID:    agentID,
		Type:       protocol.TypeRestoreReq,
		Message:    *msg,
		TaskType:   "restore",
		TaskState:  commands.TaskStatusPending,
		SnapshotID: request.SnapshotID,
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

func (h *RestoreHandler) commandService() *commands.Service {
	if h.Commands != nil {
		return h.Commands
	}
	return commands.NewService(h.DB, h.Hub)
}
