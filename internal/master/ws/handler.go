package ws

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

const maxMessageBytes = 1 << 20

var pongWait = 60 * time.Second

type AgentAuthFunc func(token string) (agentID string, err error)
type PolicyLookupFunc func(agentID string) (*protocol.Message, bool)

type Handler struct {
	hub          *Hub
	eventBus     *events.Bus
	authAgent    AgentAuthFunc
	policyLookup PolicyLookupFunc
	upgrader     websocket.Upgrader
}

var timeNow = time.Now

func NewHandler(hub *Hub, eventBus *events.Bus, authAgent AgentAuthFunc, policyLookup PolicyLookupFunc) *Handler {
	return &Handler{
		hub:          hub,
		eventBus:     eventBus,
		authAgent:    authAgent,
		policyLookup: policyLookup,
		upgrader: websocket.Upgrader{
			CheckOrigin: allowAgentOrigin,
		},
	}
}

func allowAgentOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return originURL.Host == r.Host
}

func (h *Handler) HandleWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "missing token"})
		return
	}

	agentID, err := h.authAgent(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid token"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	safeConn := NewSafeConn(conn)
	h.hub.Add(agentID, safeConn)
	defer h.cleanupConnection(agentID, safeConn)
	h.eventBus.Publish(events.Event{Type: events.AgentOnline, Payload: agentID})

	if h.policyLookup != nil {
		if msg, ok := h.policyLookup(agentID); ok && msg != nil {
			if err := safeConn.WriteJSON(msg); err != nil {
				return
			}
		}
	}

	h.readLoop(agentID, safeConn)
}

func (h *Handler) cleanupConnection(agentID string, conn *SafeConn) {
	if h.hub.RemoveIfCurrent(agentID, conn) {
		h.eventBus.Publish(events.Event{Type: events.AgentOffline, Payload: agentID})
	}
}

func (h *Handler) readLoop(agentID string, conn *SafeConn) {
	conn.SetReadLimit(maxMessageBytes)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		h.dispatch(agentID, msg)
	}
}

func (h *Handler) dispatch(agentID string, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeHeartbeat:
		now := timeNow()
		h.hub.UpdateLastSeen(agentID, now)
		h.hub.MarkOnline(agentID)
	case protocol.TypePolicyAck:
		h.eventBus.Publish(events.Event{
			Type: events.PolicyChanged,
			Payload: map[string]interface{}{
				"agent_id": agentID,
				"action":   "ack",
			},
		})
	case protocol.TypeTaskResult:
		h.eventBus.Publish(events.Event{
			Type: events.TaskResult,
			Payload: map[string]interface{}{
				"agent_id": agentID,
				"payload":  msg.Payload,
			},
		})
	case protocol.TypeDirBrowseResp:
		h.hub.HandleResponse(agentID, msg)
	}
}
