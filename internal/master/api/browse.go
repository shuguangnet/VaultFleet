package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const browseRequestTimeout = 15 * time.Second

type BrowseHandler struct {
	DB  *db.Database
	Hub BrowseHub

	timeout     time.Duration
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type BrowseHub interface {
	IsOnline(agentID string) bool
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type browseAgentRequest struct {
	Path  string `json:"path" binding:"required"`
	Depth int    `json:"depth"`
}

func NewBrowseHandler(database *db.Database, hub BrowseHub) *BrowseHandler {
	handler := &BrowseHandler{
		DB:      database,
		Hub:     hub,
		timeout: browseRequestTimeout,
	}
	handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		return handler.Hub.SendAndWait(agentID, msg, timeout)
	}
	return handler
}

func RegisterBrowseRoutes(rg *gin.RouterGroup, h *BrowseHandler) {
	rg.POST("/agents/:id/browse", h.BrowseAgent)
}

func (h *BrowseHandler) BrowseAgent(c *gin.Context) {
	agentID := c.Param("id")
	if !h.agentExists(c, agentID) {
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	var request browseAgentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if request.Depth <= 0 || request.Depth > 3 {
		request.Depth = 2
	}

	msg, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{
		Path:  request.Path,
		Depth: request.Depth,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encode browse request"})
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}
	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "timeout waiting for agent response"})
			return
		}
		payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid agent response"})
			return
		}
		c.JSON(http.StatusOK, payload)
	case <-c.Request.Context().Done():
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "request cancelled"})
	}
}

func (h *BrowseHandler) agentExists(c *gin.Context, agentID string) bool {
	var agent db.Agent
	if err := h.DB.DB.First(&agent, "id = ?", agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
			return false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return false
	}
	return true
}
