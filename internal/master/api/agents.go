package api

import (
	"errors"
	"net/http"

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

func (h *AgentHandler) Create(c *gin.Context) {
	var request createAgentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid request"})
		return
	}

	agent := db.Agent{
		Name:        request.Name,
		EnrollToken: generateToken("ek_"),
		Status:      "offline",
	}
	if err := h.DB.DB.Create(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
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

	c.JSON(http.StatusOK, gin.H{"ok": true, "data": agents})
}

func (h *AgentHandler) Get(c *gin.Context) {
	agent, ok := h.findAgentByID(c, c.Param("id"))
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "data": agent})
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
	agent, ok := h.findAgentByID(c, c.Param("id"))
	if !ok {
		return
	}

	agent.EnrollToken = generateToken("ek_")
	agent.AgentToken = ""
	if err := h.DB.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           agent.ID,
			"enroll_token": agent.EnrollToken,
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
