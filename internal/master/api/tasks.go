package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const defaultTaskListLimit = 50
const maxTaskListLimit = 200

type TaskHandler struct {
	DB       *db.Database
	Hub      CommandHub
	Commands *commands.Service
}

type CommandHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type taskResponse struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	SnapshotID string     `json:"snapshot_id"`
	MessageID  string     `json:"message_id,omitempty"`
	CommandID  string     `json:"command_id,omitempty"`
	PolicyID   string     `json:"policy_id,omitempty"`
	StorageID  string     `json:"storage_id,omitempty"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
	RepoSize   int64      `json:"repo_size"`
	ErrorLog   string     `json:"error_log,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func NewTaskHandler(database *db.Database, hub CommandHub) *TaskHandler {
	return &TaskHandler{DB: database, Hub: hub}
}

func RegisterTaskRoutes(rg *gin.RouterGroup, h *TaskHandler) {
	rg.GET("/tasks", h.List)
	rg.POST("/agents/:id/backup-now", h.BackupNow)
}

func (h *TaskHandler) BackupNow(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeBackupNow, protocol.BackupNowPayload{AgentID: agentID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "encode backup request"})
		return
	}
	commandService := h.commandService()
	command, err := commandService.CreateCommand(contextFromGin(c), commands.CreateCommandInput{
		AgentID:   agentID,
		Type:      protocol.TypeBackupNow,
		Message:   *msg,
		TaskType:  "backup",
		TaskState: commands.TaskStatusPending,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	if h.Hub != nil && h.Hub.IsOnline(agentID) {
		if err := commandService.DispatchNewPendingForAgent(contextFromGin(c), agentID, 100); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
			return
		}
	}

	writeDataResponse(c, http.StatusAccepted, gin.H{
		"command_id": command.ID,
		"message_id": msg.ID,
	})
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
		responses = append(responses, newTaskResponse(history))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": responses})
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

func newTaskResponse(history db.TaskHistory) taskResponse {
	return taskResponse{
		ID:         history.ID,
		AgentID:    history.AgentID,
		Type:       history.Type,
		Status:     history.Status,
		SnapshotID: history.SnapshotID,
		MessageID:  history.MessageID,
		CommandID:  history.CommandID,
		PolicyID:   history.PolicyID,
		StorageID:  history.StorageID,
		StartedAt:  history.StartedAt,
		FinishedAt: history.FinishedAt,
		DurationMs: history.DurationMs,
		RepoSize:   history.RepoSize,
		ErrorLog:   history.ErrorLog,
		CreatedAt:  history.CreatedAt,
		UpdatedAt:  history.UpdatedAt,
	}
}
