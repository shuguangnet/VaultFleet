package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/tasklogs"
	"vaultfleet/pkg/protocol"
)

const (
	defaultTaskLogLimit = 200
	maxTaskLogLimit     = 1000
)

type taskLogResponse struct {
	AgentID        string                    `json:"agent_id"`
	MessageID      string                    `json:"message_id"`
	TaskID         string                    `json:"task_id,omitempty"`
	CommandID      string                    `json:"command_id,omitempty"`
	Status         string                    `json:"status"`
	Lines          []protocol.TaskLogPayload `json:"lines"`
	LatestSequence int64                     `json:"latest_sequence"`
	Truncated      bool                      `json:"truncated"`
	DroppedLines   int64                     `json:"dropped_lines"`
}

func (h *TaskHandler) GetLogs(c *gin.Context) {
	var history db.TaskHistory
	if err := h.DB.DB.First(&history, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "task not found")
			return
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	response := h.taskLogResponse(history.AgentID, history.MessageID, history.ID, history.CommandID, c)
	writeDataResponse(c, http.StatusOK, response)
}

func (h *CommandHandler) GetLogs(c *gin.Context) {
	var command db.AgentCommand
	if err := h.DB.DB.First(&command, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "command not found")
			return
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	response := taskLogResponseFor(c, h.DB, h.TaskLogs, command.AgentID, command.MessageID, "", command.ID)
	writeDataResponse(c, http.StatusOK, response)
}

func (h *TaskHandler) taskLogResponse(agentID string, messageID string, taskID string, commandID string, c *gin.Context) taskLogResponse {
	return taskLogResponseFor(c, h.DB, h.TaskLogs, agentID, messageID, taskID, commandID)
}

func taskLogResponseFor(c *gin.Context, database *db.Database, getter tasklogs.Getter, agentID string, messageID string, taskID string, commandID string) taskLogResponse {
	response := taskLogResponse{
		AgentID:   agentID,
		MessageID: messageID,
		TaskID:    taskID,
		CommandID: commandID,
		Status:    "empty",
		Lines:     []protocol.TaskLogPayload{},
	}
	if messageID == "" {
		response.Status = "missing_message_id"
		return response
	}
	if getter == nil {
		response.Status = unsupportedOrEmptyStatus(database, agentID)
		return response
	}

	snapshot := getter.Get(agentID, messageID, parseTaskLogAfter(c.Query("after")), parseTaskLogLimit(c.Query("limit")))
	response.Lines = snapshot.Lines
	response.LatestSequence = snapshot.LatestSequence
	response.Truncated = snapshot.Truncated
	response.DroppedLines = snapshot.DroppedLines
	switch {
	case snapshot.Expired:
		response.Status = "expired"
	case len(snapshot.Lines) > 0:
		response.Status = "available"
	case !snapshot.Exists:
		response.Status = unsupportedOrEmptyStatus(database, agentID)
	default:
		response.Status = "empty"
	}
	return response
}

func unsupportedOrEmptyStatus(database *db.Database, agentID string) string {
	supported, err := agentHasCapability(database, agentID, protocol.CapabilityLiveTaskLogs)
	if err == nil && !supported {
		return "unsupported_agent"
	}
	return "empty"
}

func parseTaskLogAfter(raw string) int64 {
	if raw == "" {
		return 0
	}
	after, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || after < 0 {
		return 0
	}
	return after
}

func parseTaskLogLimit(raw string) int {
	if raw == "" {
		return defaultTaskLogLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultTaskLogLimit
	}
	if limit > maxTaskLogLimit {
		return maxTaskLogLimit
	}
	return limit
}
