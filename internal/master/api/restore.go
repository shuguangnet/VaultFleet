package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

type RestoreHandler struct {
	DB  *db.Database
	Hub RestoreHub
}

type RestoreHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type restoreRequest struct {
	SnapshotID string `json:"snapshot_id" binding:"required"`
	TargetPath string `json:"target_path" binding:"required"`
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeRestoreReq, protocol.RestoreReqPayload{
		SnapshotID: request.SnapshotID,
		Target:     request.TargetPath,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encode restore request"})
		return
	}

	startedAt := time.Now()
	history := db.TaskHistory{
		AgentID:    agentID,
		Type:       "restore",
		Status:     "running",
		SnapshotID: request.SnapshotID,
		StartedAt:  &startedAt,
	}
	if err := h.DB.DB.Create(&history).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if err := h.Hub.Send(agentID, *msg); err != nil {
		finishedAt := time.Now()
		updates := map[string]interface{}{
			"status":      "failed",
			"finished_at": &finishedAt,
			"duration_ms": finishedAt.Sub(startedAt).Milliseconds(),
			"error_log":   err.Error(),
		}
		if updateErr := h.DB.DB.Model(&history).Updates(updates).Error; updateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":    "restore started",
		"message_id": msg.ID,
	})
}
