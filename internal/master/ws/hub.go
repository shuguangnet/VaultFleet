package ws

import (
	"errors"
	"sync"
	"time"

	"vaultfleet/pkg/protocol"
)

var ErrAgentNotConnected = errors.New("agent not connected")

type AgentStatus struct {
	Conn       *SafeConn
	Online     bool
	LastSeenAt time.Time
}

type Hub struct {
	mu      sync.RWMutex
	agents  map[string]*AgentStatus
	waiters map[string]map[string]*responseWaiter
}

type responseWaiter struct {
	ch           chan protocol.Message
	done         chan struct{}
	expectedType string
	once         sync.Once
}

func NewHub() *Hub {
	return &Hub{
		agents:  make(map[string]*AgentStatus),
		waiters: make(map[string]map[string]*responseWaiter),
	}
}

func (h *Hub) Add(agentID string, conn *SafeConn) {
	h.mu.Lock()
	previous := h.agents[agentID]

	h.agents[agentID] = &AgentStatus{
		Conn:       conn,
		Online:     true,
		LastSeenAt: time.Now(),
	}
	h.mu.Unlock()

	if previous != nil && previous.Conn != nil && previous.Conn != conn {
		_ = previous.Conn.Close()
	}
}

func (h *Hub) Remove(agentID string) {
	h.mu.Lock()
	status, ok := h.agents[agentID]
	if ok {
		status.Online = false
		delete(h.agents, agentID)
	}
	h.mu.Unlock()

	if ok && status.Conn != nil {
		_ = status.Conn.Close()
	}
}

func (h *Hub) Send(agentID string, msg interface{}) error {
	h.mu.RLock()
	status, ok := h.agents[agentID]
	var conn *SafeConn
	online := false
	if ok && status != nil {
		conn = status.Conn
		online = status.Online
	}
	h.mu.RUnlock()

	if !ok || !online || conn == nil {
		return ErrAgentNotConnected
	}

	return conn.WriteJSON(msg)
}

func (h *Hub) SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
	if msg.ID == "" {
		return nil, errors.New("message id required")
	}

	respCh := make(chan protocol.Message, 1)
	expectedType, err := expectedResponseType(msg.Type)
	if err != nil {
		return nil, err
	}
	waiter := &responseWaiter{
		ch:           respCh,
		done:         make(chan struct{}),
		expectedType: expectedType,
	}
	h.mu.Lock()
	if h.waiters[agentID] == nil {
		h.waiters[agentID] = make(map[string]*responseWaiter)
	}
	h.waiters[agentID][msg.ID] = waiter
	h.mu.Unlock()

	if err := h.Send(agentID, msg); err != nil {
		h.removeWaiter(agentID, msg.ID, waiter)
		waiter.finish()
		return nil, err
	}

	go h.timeoutWaiter(agentID, msg.ID, waiter, timeout)
	return respCh, nil
}

func (h *Hub) HandleResponse(agentID string, msg protocol.Message) bool {
	h.mu.Lock()
	waitersByAgent := h.waiters[agentID]
	waiter := waitersByAgent[msg.ID]
	if waiter == nil {
		h.mu.Unlock()
		return false
	}
	if waiter.expectedType != "" && msg.Type != waiter.expectedType {
		h.mu.Unlock()
		return false
	}
	delete(waitersByAgent, msg.ID)
	if len(waitersByAgent) == 0 {
		delete(h.waiters, agentID)
	}
	h.mu.Unlock()

	waiter.finish()
	waiter.ch <- msg
	close(waiter.ch)
	return true
}

func expectedResponseType(requestType string) (string, error) {
	switch requestType {
	case protocol.TypeDirBrowseReq:
		return protocol.TypeDirBrowseResp, nil
	case protocol.TypeSnapshotListReq:
		return protocol.TypeSnapshotListResp, nil
	case protocol.TypeCollectLogsReq:
		return protocol.TypeCollectLogsResp, nil
	default:
		return "", errors.New("unsupported request type for response waiter")
	}
}

func (h *Hub) HasWaiter(agentID string, msgID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.waiters[agentID] != nil && h.waiters[agentID][msgID] != nil
}

func (h *Hub) timeoutWaiter(agentID string, msgID string, waiter *responseWaiter, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		if h.removeWaiter(agentID, msgID, waiter) {
			close(waiter.ch)
			waiter.finish()
		}
	case <-waiter.done:
	}
}

func (h *Hub) removeWaiter(agentID string, msgID string, waiter *responseWaiter) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	waitersByAgent := h.waiters[agentID]
	if waitersByAgent == nil || waitersByAgent[msgID] != waiter {
		return false
	}
	delete(waitersByAgent, msgID)
	if len(waitersByAgent) == 0 {
		delete(h.waiters, agentID)
	}
	return true
}

func (w *responseWaiter) finish() {
	w.once.Do(func() {
		close(w.done)
	})
}

func (h *Hub) RemoveIfCurrent(agentID string, conn *SafeConn) bool {
	h.mu.Lock()
	status, ok := h.agents[agentID]
	if !ok || status == nil || status.Conn != conn {
		h.mu.Unlock()
		return false
	}
	wasOnline := status.Online
	status.Online = false
	delete(h.agents, agentID)
	h.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	return wasOnline
}

func (h *Hub) IsOnline(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status, ok := h.agents[agentID]
	return ok && status != nil && status.Online
}

func (h *Hub) UpdateLastSeen(agentID string, t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if status, ok := h.agents[agentID]; ok && status != nil {
		status.LastSeenAt = t
	}
}

func (h *Hub) MarkOffline(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	status, ok := h.agents[agentID]
	if !ok || status == nil || !status.Online {
		return false
	}

	status.Online = false
	return true
}

func (h *Hub) MarkOfflineIfStale(agentID string, now time.Time, threshold time.Duration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	status, ok := h.agents[agentID]
	if !ok || status == nil || !status.Online {
		return false
	}
	if now.Sub(status.LastSeenAt) <= threshold {
		return false
	}

	status.Online = false
	return true
}

func (h *Hub) MarkOnline(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	status, ok := h.agents[agentID]
	if !ok || status == nil || status.Online {
		return false
	}

	status.Online = true
	return true
}

func (h *Hub) GetAllAgents() map[string]*AgentStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agents := make(map[string]*AgentStatus, len(h.agents))
	for agentID, status := range h.agents {
		if status == nil {
			agents[agentID] = nil
			continue
		}
		statusCopy := *status
		agents[agentID] = &statusCopy
	}
	return agents
}

func (h *Hub) OnlineAgentCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, status := range h.agents {
		if status != nil && status.Online {
			count++
		}
	}
	return count
}
