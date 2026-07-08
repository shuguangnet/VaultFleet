package ws

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/tasklogs"
	"vaultfleet/pkg/protocol"
)

const (
	maxMessageBytes = 1 << 20
	defaultPongWait = 60 * time.Second
)

type AgentAuthFunc func(token string) (agentID string, err error)
type PolicyLookupFunc func(agentID string) (*protocol.Message, bool)
type TaskResultProcessorFunc func(agentID string, msg protocol.Message) error
type PolicyAckProcessorFunc func(agentID string, msg protocol.Message) error
type SnapshotListResponseProcessorFunc func(agentID string, msg protocol.Message) error
type PendingCommandDispatcherFunc func(agentID string) error
type Handler struct {
	hub                           *Hub
	eventBus                      *events.Bus
	authAgent                     AgentAuthFunc
	policyLookup                  PolicyLookupFunc
	taskResultProcess             TaskResultProcessorFunc
	PolicyAckProcessor            PolicyAckProcessorFunc
	SnapshotListResponseProcessor SnapshotListResponseProcessorFunc
	PendingCommandDispatcher      PendingCommandDispatcherFunc
	AgentStateUpdater             func(agentID string, status string, lastSeenAt *time.Time) error
	HeartbeatStateUpdater         func(agentID string, status string, lastSeenAt *time.Time, heartbeat *protocol.HeartbeatPayload) error
	upgrader                      websocket.Upgrader
	capabilityDispatchMu          sync.Mutex
	capabilityDispatches          map[string]struct{}
	now                           func() time.Time
	pongWait                      time.Duration
	MasterVersion                 string
	GitHubRepo                    string
	ProgressCache                 *BackupProgressCache
	TaskLogBuffer                 *tasklogs.Buffer
	versionNotifyMu               sync.Mutex
	versionNotifyTimes            map[string]time.Time
}

func NewHandler(hub *Hub, eventBus *events.Bus, authAgent AgentAuthFunc, policyLookup PolicyLookupFunc, taskResultProcess TaskResultProcessorFunc) *Handler {
	return &Handler{
		hub:                  hub,
		eventBus:             eventBus,
		authAgent:            authAgent,
		policyLookup:         policyLookup,
		taskResultProcess:    taskResultProcess,
		capabilityDispatches: make(map[string]struct{}),
		now:                  time.Now,
		pongWait:             defaultPongWait,
		versionNotifyTimes:   make(map[string]time.Time),
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
	h.resetCapabilityDispatch(agentID)
	now := h.now()
	h.hub.UpdateLastSeen(agentID, now)
	h.updateAgentState(agentID, "online", &now)
	defer h.cleanupConnection(agentID, safeConn)
	h.eventBus.Publish(events.Event{Type: events.AgentOnline, Payload: agentID})

	if h.policyLookup != nil {
		if msg, ok := h.policyLookup(agentID); ok && msg != nil {
			if err := safeConn.WriteJSON(msg); err != nil {
				return
			}
		}
	}

	if h.PendingCommandDispatcher != nil {
		h.dispatchPendingCommands(agentID)
	}

	h.readLoop(agentID, safeConn)
}

func (h *Handler) dispatchPendingCommands(agentID string) {
	if h.PendingCommandDispatcher == nil {
		return
	}
	if err := h.PendingCommandDispatcher(agentID); err != nil {
		log.Printf("dispatch pending commands for agent %s failed: %v", agentID, err)
	}
}

func (h *Handler) cleanupConnection(agentID string, conn *SafeConn) {
	removed, wasOnline := h.hub.RemoveIfCurrent(agentID, conn)
	if !removed {
		return
	}

	h.resetCapabilityDispatch(agentID)
	h.resetVersionNotify(agentID)
	if h.ProgressCache != nil {
		h.ProgressCache.DeleteAgent(agentID)
	}
	if wasOnline {
		h.updateAgentState(agentID, "offline", nil)
		h.eventBus.Publish(events.Event{Type: events.AgentOffline, Payload: agentID})
	}
}

func (h *Handler) resetCapabilityDispatch(agentID string) {
	h.capabilityDispatchMu.Lock()
	delete(h.capabilityDispatches, agentID)
	h.capabilityDispatchMu.Unlock()
}

func (h *Handler) markCapabilityDispatch(agentID string) bool {
	h.capabilityDispatchMu.Lock()
	defer h.capabilityDispatchMu.Unlock()
	if _, dispatched := h.capabilityDispatches[agentID]; dispatched {
		return false
	}
	h.capabilityDispatches[agentID] = struct{}{}
	return true
}

func (h *Handler) readLoop(agentID string, conn *SafeConn) {
	conn.SetReadLimit(maxMessageBytes)
	_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.pongWait))
	})

	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
		h.dispatch(agentID, msg)
	}
}

func (h *Handler) dispatch(agentID string, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeHeartbeat:
		now := h.now()
		h.hub.UpdateLastSeen(agentID, now)
		h.hub.MarkOnline(agentID)
		h.updateAgentState(agentID, "online", &now)
		heartbeat, parseErr := protocol.ParsePayload[protocol.HeartbeatPayload](&msg)
		if parseErr == nil && (heartbeat.AgentVersion != "" || len(heartbeat.Capabilities) > 0) {
			if h.HeartbeatStateUpdater != nil {
				if err := h.HeartbeatStateUpdater(agentID, "online", &now, heartbeat); err != nil {
					log.Printf("update agent %s heartbeat state failed: %v", agentID, err)
				} else if len(heartbeat.Capabilities) > 0 {
					if h.markCapabilityDispatch(agentID) {
						h.dispatchPendingCommands(agentID)
					}
				}
			}
			if heartbeat.AgentVersion != "" && isAgentReleaseVersion(h.MasterVersion) && heartbeat.AgentVersion != h.MasterVersion {
				h.notifyVersionIfCooldown(agentID)
			}
		}
	case protocol.TypePolicyAck:
		if h.PolicyAckProcessor != nil {
			if err := h.PolicyAckProcessor(agentID, msg); err != nil {
				log.Printf("process policy ack failed for agent %s: %v", agentID, err)
			}
		}
		h.eventBus.Publish(events.Event{
			Type: events.PolicyChanged,
			Payload: map[string]interface{}{
				"agent_id": agentID,
				"action":   "ack",
			},
		})
	case protocol.TypeTaskResult:
		if h.ProgressCache != nil {
			h.ProgressCache.Delete(agentID, msg.ID)
		}
		if h.TaskLogBuffer != nil {
			h.TaskLogBuffer.MarkComplete(agentID, msg.ID)
		}
		if h.taskResultProcess != nil {
			if err := h.taskResultProcess(agentID, msg); err != nil {
				log.Printf("process task result failed for agent %s: %v", agentID, err)
				return
			}
		}
		h.eventBus.Publish(events.Event{
			Type: events.TaskResult,
			Payload: map[string]interface{}{
				"agent_id": agentID,
				"payload":  msg.Payload,
			},
		})
	case protocol.TypeBackupProgress:
		if h.ProgressCache != nil {
			if progress, err := protocol.ParsePayload[protocol.BackupProgressPayload](&msg); err == nil {
				progress.AgentID = agentID
				h.ProgressCache.Set(agentID, msg.ID, progress)
			}
		}
	case protocol.TypeTaskLog:
		if h.TaskLogBuffer != nil {
			if payload, err := protocol.ParsePayload[protocol.TaskLogPayload](&msg); err == nil {
				messageID := payload.MessageID
				if messageID == "" {
					messageID = msg.ID
				}
				h.TaskLogBuffer.Add(agentID, messageID, *payload)
			}
		}
	case protocol.TypeDirBrowseResp, protocol.TypeDirSizeResp, protocol.TypeDockerDiscoveryResp, protocol.TypeDatabaseDiscoveryResp, protocol.TypeSnapshotListResp, protocol.TypeSnapshotBrowseResp, protocol.TypeRestorePreflightResp, protocol.TypeCollectLogsResp, protocol.TypeUpdateAgentResp:
		handled := h.hub.HandleResponse(agentID, msg)
		if !handled && msg.Type == protocol.TypeSnapshotListResp && h.SnapshotListResponseProcessor != nil {
			if err := h.SnapshotListResponseProcessor(agentID, msg); err != nil {
				log.Printf("process snapshot list response failed for agent %s: %v", agentID, err)
			}
		}
	}
}

const versionNotifyCooldown = time.Hour

func (h *Handler) notifyVersionIfCooldown(agentID string) {
	h.versionNotifyMu.Lock()
	last, ok := h.versionNotifyTimes[agentID]
	now := h.now()
	if ok && now.Sub(last) < versionNotifyCooldown {
		h.versionNotifyMu.Unlock()
		return
	}
	h.versionNotifyTimes[agentID] = now
	h.versionNotifyMu.Unlock()

	msg, err := protocol.NewMessage(protocol.TypeVersionInfo, protocol.VersionInfoPayload{
		Version:    h.MasterVersion,
		GitHubRepo: h.GitHubRepo,
	})
	if err != nil {
		log.Printf("create version info message failed: %v", err)
		return
	}
	if err := h.hub.Send(agentID, msg); err != nil {
		log.Printf("send version info to agent %s failed: %v", agentID, err)
	}
}

func (h *Handler) resetVersionNotify(agentID string) {
	h.versionNotifyMu.Lock()
	delete(h.versionNotifyTimes, agentID)
	h.versionNotifyMu.Unlock()
}

func isAgentReleaseVersion(version string) bool {
	return strings.HasPrefix(strings.TrimSpace(version), "v")
}

func (h *Handler) updateAgentState(agentID string, status string, lastSeenAt *time.Time) {
	if h.AgentStateUpdater == nil {
		return
	}
	if err := h.AgentStateUpdater(agentID, status, lastSeenAt); err != nil {
		log.Printf("update agent %s state failed: %v", agentID, err)
	}
}
