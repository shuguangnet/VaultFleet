package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type enrollAgentRequest struct {
	EnrollToken string `json:"enroll_token" binding:"required"`
	SystemInfo  string `json:"system_info"`
}

func (h *AgentHandler) Enroll(c *gin.Context) {
	var request enrollAgentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid request"})
		return
	}

	var agent db.Agent
	if err := h.DB.DB.First(&agent, "enroll_token = ?", request.EnrollToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid enrollment token"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	if agent.AgentToken != "" {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "agent already enrolled"})
		return
	}

	agentToken := generateToken("ak_")
	agent.AgentToken = agentToken
	agent.EnrollToken = ""
	agent.SystemInfo = request.SystemInfo

	if err := h.DB.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"agent_id":    agent.ID,
			"agent_token": agentToken,
		},
	})
}
