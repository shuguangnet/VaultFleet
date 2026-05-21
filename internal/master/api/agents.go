package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type AgentHandler struct {
	DB *db.Database
}

func NewAgentHandler(database *db.Database) *AgentHandler {
	return &AgentHandler{DB: database}
}

type createAgentRequest struct {
	Name string `json:"name" binding:"required"`
}

type agentResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	LastSeen   *time.Time `json:"last_seen"`
	SystemInfo string     `json:"system_info"`
	Hostname   string     `json:"hostname"`
	OS         string     `json:"os"`
	Arch       string     `json:"arch"`
	Version    string     `json:"version"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (h *AgentHandler) Create(c *gin.Context) {
	var request createAgentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid request"})
		return
	}

	var agent db.Agent
	err := withGeneratedToken("ek_", func(token string) error {
		agent = db.Agent{
			Name:        request.Name,
			EnrollToken: token,
			Status:      "offline",
		}
		return h.DB.DB.Create(&agent).Error
	})
	if err != nil {
		status := http.StatusInternalServerError
		message := "token generation failed"
		if !isTokenGenerationError(err) {
			message = "database error"
		}

		c.JSON(status, gin.H{"ok": false, "error": message})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           agent.ID,
			"name":         agent.Name,
			"enroll_token": agent.EnrollToken,
		},
	})
}

func (h *AgentHandler) List(c *gin.Context) {
	agents := []db.Agent{}
	if err := h.DB.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "data": agentResponses(agents)})
}

func (h *AgentHandler) Get(c *gin.Context) {
	agent, ok := h.findAgentByID(c, c.Param("id"))
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "data": newAgentResponse(agent)})
}

func (h *AgentHandler) Delete(c *gin.Context) {
	result := h.DB.DB.Delete(&db.Agent{}, "id = ?", c.Param("id"))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AgentHandler) RegenerateToken(c *gin.Context) {
	id := c.Param("id")
	var enrollToken string

	err := withGeneratedToken("ek_", func(token string) error {
		result := h.DB.DB.Model(&db.Agent{}).
			Where("id = ?", id).
			Select("enroll_token", "agent_token").
			Updates(map[string]any{
				"enroll_token": token,
				"agent_token":  "",
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		enrollToken = token
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
		case isTokenGenerationError(err):
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "token generation failed"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           id,
			"enroll_token": enrollToken,
		},
	})
}

func (h *AgentHandler) GetInstallToken(c *gin.Context) {
	agent, ok := h.findAgentByID(c, c.Param("id"))
	if !ok {
		return
	}

	enrolled := agent.EnrollToken == ""
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           agent.ID,
			"enroll_token": agent.EnrollToken,
			"enrolled":     enrolled,
		},
	})
}

func (h *AgentHandler) findAgentByID(c *gin.Context, id string) (db.Agent, bool) {
	var agent db.Agent
	if err := h.DB.DB.First(&agent, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
			return db.Agent{}, false
		}

		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return db.Agent{}, false
	}

	return agent, true
}

func newAgentResponse(agent db.Agent) agentResponse {
	systemInfo := parseAgentSystemInfo(agent.SystemInfo)
	return agentResponse{
		ID:         agent.ID,
		Name:       agent.Name,
		Status:     agent.Status,
		LastSeenAt: agent.LastSeenAt,
		LastSeen:   agent.LastSeenAt,
		SystemInfo: agent.SystemInfo,
		Hostname:   systemInfo.Hostname,
		OS:         systemInfo.OS,
		Arch:       systemInfo.Arch,
		Version:    systemInfo.Version,
		CreatedAt:  agent.CreatedAt,
		UpdatedAt:  agent.UpdatedAt,
	}
}

type agentSystemInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
}

func parseAgentSystemInfo(raw string) agentSystemInfo {
	var info agentSystemInfo
	if raw == "" {
		return info
	}
	_ = json.Unmarshal([]byte(raw), &info)
	return info
}

func agentResponses(agents []db.Agent) []agentResponse {
	responses := make([]agentResponse, 0, len(agents))
	for _, agent := range agents {
		responses = append(responses, newAgentResponse(agent))
	}
	return responses
}

const tokenGenerationAttempts = 3

type tokenGenerationError struct {
	err error
}

func (e tokenGenerationError) Error() string {
	return e.err.Error()
}

func (e tokenGenerationError) Unwrap() error {
	return e.err
}

func withGeneratedToken(prefix string, use func(string) error) error {
	var lastErr error
	for range tokenGenerationAttempts {
		token, err := tokenGenerator(prefix)
		if err != nil {
			return tokenGenerationError{err: err}
		}

		err = use(token)
		if err == nil {
			return nil
		}
		if !isUniqueConstraintError(err) {
			return err
		}

		lastErr = err
	}

	return lastErr
}

func isTokenGenerationError(err error) bool {
	var tokenErr tokenGenerationError
	return errors.As(err, &tokenErr)
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
