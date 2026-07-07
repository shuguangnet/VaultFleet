package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/tasklogs"
)

const defaultCommandListLimit = 50
const maxCommandListLimit = 200

type CommandHandler struct {
	DB       *db.Database
	TaskLogs tasklogs.Getter
}

type commandResponse struct {
	ID              string     `json:"id"`
	AgentID         string     `json:"agent_id"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	MessageID       string     `json:"message_id"`
	Result          string     `json:"result,omitempty"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	Error           string     `json:"error,omitempty"`
	Attempts        int        `json:"attempts"`
	PolicyID        string     `json:"policy_id,omitempty"`
	PolicyUpdatedAt *time.Time `json:"policy_updated_at,omitempty"`
	StorageID       string     `json:"storage_id,omitempty"`
	DeadlineAt      *time.Time `json:"deadline_at"`
	DispatchedAt    *time.Time `json:"dispatched_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func NewCommandHandler(database *db.Database) *CommandHandler {
	return &CommandHandler{DB: database}
}

func RegisterCommandRoutes(rg *gin.RouterGroup, h *CommandHandler) {
	rg.GET("/commands/:id", h.Get)
	rg.GET("/commands/:id/logs", h.GetLogs)
	rg.GET("/agents/:id/commands", h.ListAgentCommands)
}

func (h *CommandHandler) Get(c *gin.Context) {
	var command db.AgentCommand
	if err := h.DB.DB.First(&command, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "command not found")
			return
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	writeDataResponse(c, http.StatusOK, newCommandResponse(command))
}

func (h *CommandHandler) ListAgentCommands(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}

	query := h.DB.DB.
		Where("agent_id = ?", agentID).
		Order("created_at DESC").
		Limit(parseCommandLimit(c.Query("limit")))
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if commandType := c.Query("type"); commandType != "" {
		query = query.Where("type = ?", commandType)
	}

	var commandRows []db.AgentCommand
	if err := query.Find(&commandRows).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}

	responses := make([]commandResponse, 0, len(commandRows))
	for _, command := range commandRows {
		responses = append(responses, newCommandResponse(command))
	}
	writeDataResponse(c, http.StatusOK, responses)
}

func parseCommandLimit(raw string) int {
	if raw == "" {
		return defaultCommandListLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultCommandListLimit
	}
	if limit > maxCommandListLimit {
		return maxCommandListLimit
	}
	return limit
}

func newCommandResponse(command db.AgentCommand) commandResponse {
	return commandResponse{
		ID:              command.ID,
		AgentID:         command.AgentID,
		Type:            command.Type,
		Status:          command.Status,
		MessageID:       command.MessageID,
		Result:          command.Result,
		ErrorMessage:    command.ErrorMessage,
		Error:           command.ErrorMessage,
		Attempts:        command.Attempts,
		PolicyID:        command.PolicyID,
		PolicyUpdatedAt: command.PolicyUpdatedAt,
		StorageID:       command.StorageID,
		DeadlineAt:      command.DeadlineAt,
		DispatchedAt:    command.DispatchedAt,
		CompletedAt:     command.CompletedAt,
		CreatedAt:       command.CreatedAt,
		UpdatedAt:       command.UpdatedAt,
	}
}
