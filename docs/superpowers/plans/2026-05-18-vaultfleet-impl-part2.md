### Task 5: Master WebSocket Hub

**Files:**
- `internal/master/ws/safeconn.go`
- `internal/master/ws/hub.go`
- `internal/master/ws/handler.go`
- `internal/master/events/events.go`
- `internal/master/ws/hub_test.go`
- `internal/master/ws/handler_test.go`
- `internal/master/events/events_test.go`

**Steps:**

- [ ] 5.1 — Create `internal/master/ws/safeconn.go`

```go
// internal/master/ws/safeconn.go
package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type SafeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{conn: conn}
}

func (sc *SafeConn) WriteJSON(v interface{}) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return sc.conn.WriteJSON(v)
}

func (sc *SafeConn) ReadJSON(v interface{}) error {
	return sc.conn.ReadJSON(v)
}

func (sc *SafeConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.Close()
}
```

- [ ] 5.2 — Create `internal/master/ws/hub.go`

```go
// internal/master/ws/hub.go
package ws

import (
	"sync"
	"time"
)

type AgentStatus struct {
	Conn       *SafeConn
	Online     bool
	LastSeenAt time.Time
}

type Hub struct {
	mu     sync.RWMutex
	agents map[string]*AgentStatus
}

func NewHub() *Hub {
	return &Hub{
		agents: make(map[string]*AgentStatus),
	}
}

func (h *Hub) Add(agentID string, conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agents[agentID] = &AgentStatus{
		Conn:       conn,
		Online:     true,
		LastSeenAt: time.Now(),
	}
}

func (h *Hub) Remove(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if status, ok := h.agents[agentID]; ok {
		status.Online = false
		status.Conn.Close()
		delete(h.agents, agentID)
	}
}

func (h *Hub) Send(agentID string, msg interface{}) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	status, ok := h.agents[agentID]
	if !ok {
		return ErrAgentNotConnected
	}
	return status.Conn.WriteJSON(msg)
}

func (h *Hub) IsOnline(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	status, ok := h.agents[agentID]
	return ok && status.Online
}

func (h *Hub) UpdateLastSeen(agentID string, t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if status, ok := h.agents[agentID]; ok {
		status.LastSeenAt = t
	}
}

func (h *Hub) GetAllAgents() map[string]*AgentStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]*AgentStatus, len(h.agents))
	for k, v := range h.agents {
		result[k] = v
	}
	return result
}

var ErrAgentNotConnected = &HubError{msg: "agent not connected"}

type HubError struct {
	msg string
}

func (e *HubError) Error() string { return e.msg }
```

- [ ] 5.3 — Create `internal/master/events/events.go`

```go
// internal/master/events/events.go
package events

import "sync"

type EventType string

const (
	PolicyChanged  EventType = "policy_changed"
	AgentOnline    EventType = "agent_online"
	AgentOffline   EventType = "agent_offline"
)

type Event struct {
	Type    EventType
	Payload interface{}
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

func NewBus() *Bus {
	return &Bus{
		handlers: make(map[EventType][]Handler),
	}
}

func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := make([]Handler, len(b.handlers[event.Type]))
	copy(handlers, b.handlers[event.Type])
	b.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}
```

- [ ] 5.4 — Create `internal/master/ws/handler.go`

```go
// internal/master/ws/handler.go
package ws

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type AgentAuthFunc func(token string) (agentID string, err error)
type PolicyLookupFunc func(agentID string) (*protocol.Message, bool)

type Handler struct {
	hub          *Hub
	eventBus     *events.Bus
	authAgent    AgentAuthFunc
	policyLookup PolicyLookupFunc
}

func NewHandler(hub *Hub, eventBus *events.Bus, authFn AgentAuthFunc, policyFn PolicyLookupFunc) *Handler {
	return &Handler{
		hub:          hub,
		eventBus:     eventBus,
		authAgent:    authFn,
		policyLookup: policyFn,
	}
}

func (h *Handler) HandleWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	agentID, err := h.authAgent(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	sc := NewSafeConn(conn)
	h.hub.Add(agentID, sc)
	h.eventBus.Publish(events.Event{Type: events.AgentOnline, Payload: agentID})

	if policyMsg, ok := h.policyLookup(agentID); ok {
		sc.WriteJSON(policyMsg)
	}

	defer func() {
		h.hub.Remove(agentID)
		h.eventBus.Publish(events.Event{Type: events.AgentOffline, Payload: agentID})
	}()

	h.readLoop(agentID, sc)
}

func (h *Handler) readLoop(agentID string, sc *SafeConn) {
	for {
		var msg protocol.Message
		if err := sc.ReadJSON(&msg); err != nil {
			return
		}
		h.dispatch(agentID, msg)
	}
}

func (h *Handler) dispatch(agentID string, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeHeartbeat:
		h.hub.UpdateLastSeen(agentID, timeNow())
	case protocol.TypePolicyAck:
		h.eventBus.Publish(events.Event{
			Type:    events.PolicyChanged,
			Payload: map[string]string{"agent_id": agentID, "action": "ack"},
		})
	case protocol.TypeTaskResult:
		h.eventBus.Publish(events.Event{
			Type:    EventType("task_result"),
			Payload: map[string]interface{}{"agent_id": agentID, "payload": msg.Payload},
		})
	}
}

type EventType = events.EventType

var timeNow = time.Now

// Required import not in the import block above — add `"time"` to imports.
```

Note: The `time` import is needed. The actual file will have the full correct import block:

```go
import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)
```

- [ ] 5.5 — Create `internal/master/ws/hub_test.go`

```go
// internal/master/ws/hub_test.go
package ws

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub_AddAndRemove(t *testing.T) {
	hub := NewHub()

	hub.Add("agent-1", &SafeConn{})
	assert.True(t, hub.IsOnline("agent-1"))

	hub.Remove("agent-1")
	assert.False(t, hub.IsOnline("agent-1"))
}

func TestHub_SendToOnlineAgent(t *testing.T) {
	// Uses a real WebSocket via httptest (see handler_test.go for full integration).
	// Here we test the error path for missing agent.
	hub := NewHub()
	err := hub.Send("nonexistent", map[string]string{"type": "ping"})
	require.Error(t, err)
	assert.Equal(t, ErrAgentNotConnected, err)
}

func TestHub_UpdateLastSeen(t *testing.T) {
	hub := NewHub()
	hub.Add("agent-1", &SafeConn{})

	now := time.Now()
	hub.UpdateLastSeen("agent-1", now)

	agents := hub.GetAllAgents()
	assert.Equal(t, now, agents["agent-1"].LastSeenAt)
}

func TestHub_GetAllAgents(t *testing.T) {
	hub := NewHub()
	hub.Add("agent-1", &SafeConn{})
	hub.Add("agent-2", &SafeConn{})

	all := hub.GetAllAgents()
	assert.Len(t, all, 2)
	assert.Contains(t, all, "agent-1")
	assert.Contains(t, all, "agent-2")
}
```

- [ ] 5.6 — Create `internal/master/events/events_test.go`

```go
// internal/master/events/events_test.go
package events

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBus_PublishSubscribe(t *testing.T) {
	bus := NewBus()
	var received []Event
	var mu sync.Mutex

	bus.Subscribe(PolicyChanged, func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	bus.Publish(Event{Type: PolicyChanged, Payload: "agent-1"})
	bus.Publish(Event{Type: PolicyChanged, Payload: "agent-2"})

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, received, 2)
	assert.Equal(t, "agent-1", received[0].Payload)
	assert.Equal(t, "agent-2", received[1].Payload)
}

func TestBus_NoSubscribers(t *testing.T) {
	bus := NewBus()
	// Should not panic
	bus.Publish(Event{Type: AgentOnline, Payload: "agent-1"})
}

func TestBus_MultipleSubscribers(t *testing.T) {
	bus := NewBus()
	count := 0
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		bus.Subscribe(AgentOffline, func(e Event) {
			mu.Lock()
			count++
			mu.Unlock()
		})
	}

	bus.Publish(Event{Type: AgentOffline, Payload: "agent-1"})

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, count)
}
```

- [ ] 5.7 — Create `internal/master/ws/handler_test.go`

```go
// internal/master/ws/handler_test.go
package ws

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

func setupTestHandler(authFn AgentAuthFunc, policyFn PolicyLookupFunc) (*httptest.Server, *Hub, *events.Bus) {
	gin.SetMode(gin.TestMode)
	hub := NewHub()
	bus := events.NewBus()
	h := NewHandler(hub, bus, authFn, policyFn)

	router := gin.New()
	router.GET("/ws/agent", h.HandleWebSocket)

	server := httptest.NewServer(router)
	return server, hub, bus
}

func wsURL(server *httptest.Server, token string) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent?token=" + token
}

func TestHandler_RejectsMissingToken(t *testing.T) {
	server, _, _ := setupTestHandler(
		func(token string) (string, error) { return "", errors.New("invalid") },
		func(agentID string) (*protocol.Message, bool) { return nil, false },
	)
	defer server.Close()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server, ""), nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandler_RejectsInvalidToken(t *testing.T) {
	server, _, _ := setupTestHandler(
		func(token string) (string, error) { return "", errors.New("invalid") },
		func(agentID string) (*protocol.Message, bool) { return nil, false },
	)
	defer server.Close()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server, "bad-token"), nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandler_AcceptsValidToken(t *testing.T) {
	server, hub, _ := setupTestHandler(
		func(token string) (string, error) {
			if token == "valid-token" {
				return "agent-1", nil
			}
			return "", errors.New("invalid")
		},
		func(agentID string) (*protocol.Message, bool) { return nil, false },
	)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server, "valid-token"), nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, hub.IsOnline("agent-1"))
}

func TestHandler_DispatchesHeartbeat(t *testing.T) {
	server, hub, _ := setupTestHandler(
		func(token string) (string, error) { return "agent-1", nil },
		func(agentID string) (*protocol.Message, bool) { return nil, false },
	)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server, "valid-token"), nil)
	require.NoError(t, err)
	defer conn.Close()

	msg := protocol.Message{Type: protocol.TypeHeartbeat, ID: "hb-1"}
	err = conn.WriteJSON(msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	agents := hub.GetAllAgents()
	assert.WithinDuration(t, time.Now(), agents["agent-1"].LastSeenAt, 1*time.Second)
}

func TestHandler_PushesPolicyOnConnect(t *testing.T) {
	policyMsg := &protocol.Message{
		Type:    protocol.TypePolicyPush,
		ID:      "policy-1",
		Payload: []byte(`{"schedule":"0 3 * * *"}`),
	}

	server, _, _ := setupTestHandler(
		func(token string) (string, error) { return "agent-1", nil },
		func(agentID string) (*protocol.Message, bool) { return policyMsg, true },
	)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server, "valid-token"), nil)
	require.NoError(t, err)
	defer conn.Close()

	var received protocol.Message
	err = conn.ReadJSON(&received)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypePolicyPush, received.Type)
}
```

- [ ] 5.8 — Run tests (expect fail initially, then pass after implementation)

```bash
go test ./internal/master/ws/... -v
go test ./internal/master/events/... -v
```

- [ ] 5.9 — Verify all tests pass

```bash
go test ./internal/master/ws/... -run TestHub -v
go test ./internal/master/ws/... -run TestHandler -v
go test ./internal/master/events/... -run TestBus -v
```

- [ ] 5.10 — Commit

```bash
git add internal/master/ws/ internal/master/events/
git commit -m "feat(master): add WebSocket hub, safe conn, handler, and event bus

- SafeConn: thread-safe gorilla/websocket wrapper with write mutex
- Hub: connection registry with add/remove/send/online tracking
- Handler: Gin WS endpoint with token auth, read loop, message dispatch
- EventBus: in-process pub/sub for policy changes and agent status
- Full test coverage for hub operations, auth flow, and event pub/sub"
```

---

### Task 6: Agent Binary + WebSocket Client

**Files:**
- `cmd/agent/main.go`
- `internal/agent/connect/client.go`
- `internal/agent/connect/heartbeat.go`
- `internal/agent/policy/store.go`
- `internal/agent/connect/client_test.go`
- `internal/agent/connect/heartbeat_test.go`
- `internal/agent/policy/store_test.go`

**Steps:**

- [ ] 6.1 — Create `internal/agent/connect/client.go`

```go
// internal/agent/connect/client.go
package connect

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/gorilla/websocket"

	"vaultfleet/pkg/protocol"
)

const (
	InitialBackoff = 1 * time.Second
	MaxBackoff     = 5 * time.Minute
	BackoffFactor  = 2.0
)

type MessageHandler func(msg protocol.Message)

type Client struct {
	serverURL string
	token     string
	conn      *websocket.Conn
	handler   MessageHandler
	done      chan struct{}
}

func NewClient(serverURL, token string, handler MessageHandler) *Client {
	return &Client{
		serverURL: serverURL,
		token:     token,
		handler:   handler,
		done:      make(chan struct{}),
	}
}

func (c *Client) Run(ctx context.Context) {
	backoff := InitialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.connect(ctx)
		if err == nil {
			backoff = InitialBackoff
			c.readLoop(ctx)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = nextBackoff(backoff)
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	wsURL := c.serverURL + "/ws/agent?token=" + c.token
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		log.Printf("ws connect failed: %v", err)
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.conn.Close()
			return
		default:
		}

		var msg protocol.Message
		if err := c.conn.ReadJSON(&msg); err != nil {
			log.Printf("ws read error: %v", err)
			c.conn.Close()
			return
		}
		c.handler(msg)
	}
}

func (c *Client) Send(msg protocol.Message) error {
	if c.conn == nil {
		return ErrNotConnected
	}
	return c.conn.WriteJSON(msg)
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * BackoffFactor)
	if next > MaxBackoff {
		return MaxBackoff
	}
	return next
}

func BackoffForAttempt(attempt int) time.Duration {
	d := time.Duration(float64(InitialBackoff) * math.Pow(BackoffFactor, float64(attempt)))
	if d > MaxBackoff {
		return MaxBackoff
	}
	return d
}

var ErrNotConnected = &ClientError{msg: "not connected"}

type ClientError struct {
	msg string
}

func (e *ClientError) Error() string { return e.msg }
```

- [ ] 6.2 — Create `internal/agent/connect/heartbeat.go`

```go
// internal/agent/connect/heartbeat.go
package connect

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"vaultfleet/pkg/protocol"
)

const HeartbeatInterval = 30 * time.Second

type SystemInfo struct {
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	CPUCount      int     `json:"cpu_count"`
	MemoryTotalMB uint64  `json:"memory_total_mb"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
	DiskUsedGB    float64 `json:"disk_used_gb"`
	ResticVersion string  `json:"restic_version"`
	RcloneVersion string  `json:"rclone_version"`
}

type SystemInfoCollector func() SystemInfo

func DefaultSystemInfoCollector() SystemInfo {
	return SystemInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCount: runtime.NumCPU(),
	}
}

func RunHeartbeat(ctx context.Context, client *Client, collector SystemInfoCollector, interval time.Duration) {
	if interval == 0 {
		interval = HeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sendOne := func() {
		info := collector()
		payload, _ := json.Marshal(info)
		msg := protocol.Message{
			Type:    protocol.TypeHeartbeat,
			ID:      protocol.NewMsgID(),
			Payload: payload,
		}
		client.Send(msg)
	}

	sendOne()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendOne()
		}
	}
}
```

- [ ] 6.3 — Create `internal/agent/policy/store.go`

```go
// internal/agent/policy/store.go
package policy

import (
	"encoding/json"
	"os"
	"path/filepath"

	"vaultfleet/pkg/protocol"
)

const (
	DefaultDir         = "/etc/vaultfleet"
	PolicyFileName     = "policy.json"
	PendingResultsFile = "pending_results.json"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultDir
	}
	return &Store{dir: dir}
}

func (s *Store) SavePolicy(policy *protocol.PolicyPayload) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, PolicyFileName), data, 0600)
}

func (s *Store) LoadPolicy() (*protocol.PolicyPayload, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, PolicyFileName))
	if err != nil {
		return nil, err
	}
	var policy protocol.PolicyPayload
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *Store) SavePendingResults(results []protocol.TaskResultPayload) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, PendingResultsFile), data, 0600)
}

func (s *Store) LoadPendingResults() ([]protocol.TaskResultPayload, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, PendingResultsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []protocol.TaskResultPayload
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Store) ClearPendingResults() error {
	path := filepath.Join(s.dir, PendingResultsFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}
```

- [ ] 6.4 — Create `cmd/agent/main.go`

```go
// cmd/agent/main.go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v3"

	"vaultfleet/internal/agent/connect"
	"vaultfleet/internal/agent/policy"
	"vaultfleet/pkg/protocol"
)

type AgentConfig struct {
	Server     string `yaml:"server"`
	AgentID    string `yaml:"agent_id"`
	AgentToken string `yaml:"agent_token"`
}

func main() {
	configPath := flag.String("config", "/etc/vaultfleet/agent.yaml", "path to agent config file")
	server := flag.String("server", "", "master server URL (for enrollment)")
	token := flag.String("token", "", "enrollment token (for first-time registration)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		if *server == "" || *token == "" {
			log.Fatalf("no config found at %s and --server/--token not provided", *configPath)
		}
		cfg, err = enroll(*server, *token, *configPath)
		if err != nil {
			log.Fatalf("enrollment failed: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	store := policy.NewStore("")

	handler := func(msg protocol.Message) {
		switch msg.Type {
		case protocol.TypePolicyPush:
			var p protocol.PolicyPayload
			if err := protocol.UnmarshalPayload(msg.Payload, &p); err != nil {
				log.Printf("failed to parse policy: %v", err)
				return
			}
			if err := store.SavePolicy(&p); err != nil {
				log.Printf("failed to save policy: %v", err)
			}
			log.Printf("policy saved")
		}
	}

	wsURL := cfg.Server
	client := connect.NewClient(wsURL, cfg.AgentToken, handler)

	go connect.RunHeartbeat(ctx, client, connect.DefaultSystemInfoCollector, 0)

	client.Run(ctx)
}

func loadConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func enroll(server, token, configPath string) (*AgentConfig, error) {
	// Enrollment is implemented in Task 7
	return nil, ErrNotImplemented
}

var ErrNotImplemented = &agentError{msg: "enrollment not yet implemented"}

type agentError struct{ msg string }

func (e *agentError) Error() string { return e.msg }
```

- [ ] 6.5 — Create `internal/agent/connect/client_test.go`

```go
// internal/agent/connect/client_test.go
package connect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		name     string
		current  time.Duration
		expected time.Duration
	}{
		{"1s -> 2s", 1 * time.Second, 2 * time.Second},
		{"2s -> 4s", 2 * time.Second, 4 * time.Second},
		{"4s -> 8s", 4 * time.Second, 8 * time.Second},
		{"8s -> 16s", 8 * time.Second, 16 * time.Second},
		{"cap at 5min", 4 * time.Minute, MaxBackoff},
		{"already at max", MaxBackoff, MaxBackoff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextBackoff(tt.current)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestBackoffForAttempt(t *testing.T) {
	assert.Equal(t, 1*time.Second, BackoffForAttempt(0))
	assert.Equal(t, 2*time.Second, BackoffForAttempt(1))
	assert.Equal(t, 4*time.Second, BackoffForAttempt(2))
	assert.Equal(t, 8*time.Second, BackoffForAttempt(3))

	// Should cap at MaxBackoff
	result := BackoffForAttempt(100)
	assert.Equal(t, MaxBackoff, result)
}

func TestBackoffSequence(t *testing.T) {
	backoff := InitialBackoff
	expected := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		64 * time.Second,
		128 * time.Second,
		256 * time.Second,
		MaxBackoff,
	}

	for i, exp := range expected {
		backoff = nextBackoff(backoff)
		assert.Equal(t, exp, backoff, "step %d", i)
	}
}
```

- [ ] 6.6 — Create `internal/agent/connect/heartbeat_test.go`

```go
// internal/agent/connect/heartbeat_test.go
package connect

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestHeartbeat_SendsAtInterval(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var received []protocol.Message
	var mu sync.Mutex

	router := gin.New()
	router.GET("/ws/agent", func(c *gin.Context) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			var msg protocol.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
		}
	})

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	collector := func() SystemInfo {
		return SystemInfo{OS: "linux", Arch: "amd64", CPUCount: 4}
	}

	client := NewClient(wsURL, "test-token", func(msg protocol.Message) {})
	go client.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	go RunHeartbeat(ctx, client, collector, 100*time.Millisecond)

	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// Should have sent at least 3 heartbeats in 350ms with 100ms interval (immediate + 2 ticks)
	assert.GreaterOrEqual(t, len(received), 3)

	// Verify heartbeat content
	if len(received) > 0 {
		assert.Equal(t, protocol.TypeHeartbeat, received[0].Type)
		var info SystemInfo
		err := json.Unmarshal(received[0].Payload, &info)
		require.NoError(t, err)
		assert.Equal(t, "linux", info.OS)
		assert.Equal(t, 4, info.CPUCount)
	}
}
```

- [ ] 6.7 — Create `internal/agent/policy/store_test.go`

```go
// internal/agent/policy/store_test.go
package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestStore_SaveAndLoadPolicy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	policy := &protocol.PolicyPayload{
		AgentID:         "agent-123",
		BackupDirs:      []string{"/etc", "/home"},
		ExcludePatterns: []string{"*.log"},
		Schedule:        "0 3 * * *",
	}

	err := store.SavePolicy(policy)
	require.NoError(t, err)

	loaded, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, policy.AgentID, loaded.AgentID)
	assert.Equal(t, policy.BackupDirs, loaded.BackupDirs)
	assert.Equal(t, policy.ExcludePatterns, loaded.ExcludePatterns)
	assert.Equal(t, policy.Schedule, loaded.Schedule)
}

func TestStore_LoadPolicy_NotExists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.LoadPolicy()
	assert.Error(t, err)
}

func TestStore_SaveAndLoadPendingResults(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	results := []protocol.TaskResultPayload{
		{AgentID: "agent-1", Type: "backup", Status: "success", SnapshotID: "snap-1", DurationMS: 5000},
		{AgentID: "agent-1", Type: "backup", Status: "failed", ErrorLog: "disk full"},
	}

	err := store.SavePendingResults(results)
	require.NoError(t, err)

	loaded, err := store.LoadPendingResults()
	require.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "snap-1", loaded[0].SnapshotID)
	assert.Equal(t, "disk full", loaded[1].ErrorLog)
}

func TestStore_LoadPendingResults_NoFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	results, err := store.LoadPendingResults()
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestStore_ClearPendingResults(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	results := []protocol.TaskResultPayload{
		{AgentID: "agent-1", Status: "success"},
	}
	err := store.SavePendingResults(results)
	require.NoError(t, err)

	err = store.ClearPendingResults()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, PendingResultsFile))
	assert.True(t, os.IsNotExist(err))
}

func TestStore_PolicyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	policy := &protocol.PolicyPayload{AgentID: "agent-123"}
	err := store.SavePolicy(policy)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, PolicyFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
```

- [ ] 6.8 — Run tests (expect fail initially, then pass after implementation)

```bash
go test ./internal/agent/connect/... -v
go test ./internal/agent/policy/... -v
```

- [ ] 6.9 — Verify all tests pass

```bash
go test ./internal/agent/connect/... -run TestNextBackoff -v
go test ./internal/agent/connect/... -run TestBackoffForAttempt -v
go test ./internal/agent/connect/... -run TestBackoffSequence -v
go test ./internal/agent/connect/... -run TestHeartbeat_SendsAtInterval -v
go test ./internal/agent/policy/... -run TestStore -v
```

- [ ] 6.10 — Commit

```bash
git add cmd/agent/ internal/agent/
git commit -m "feat(agent): add agent binary, WebSocket client, heartbeat, and policy store

- cmd/agent/main.go: entry point with config loading, signal handling
- connect/client.go: WebSocket dial with exponential backoff (1s→5min cap)
- connect/heartbeat.go: periodic system info reporting (OS, CPU, memory)
- policy/store.go: load/save policy.json and pending_results.json
- Tests cover backoff logic, heartbeat interval, policy round-trip"
```

---

### Task 7: Agent Registration Flow (End-to-End)

**Files:**
- `cmd/agent/main.go` (extend enrollment)
- `internal/agent/enroll/enroll.go`
- `internal/master/api/enroll.go` (POST /api/agent/enroll endpoint)
- `internal/master/ws/handler.go` (extend: push policy on connect if synced=false)
- `internal/agent/enroll/enroll_test.go`
- `internal/master/api/enroll_test.go`
- `internal/master/ws/handler_test.go` (extend)

**Steps:**

- [ ] 7.1 — Create `internal/agent/enroll/enroll.go`

```go
// internal/agent/enroll/enroll.go
package enroll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type EnrollRequest struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type EnrollResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

type AgentConfig struct {
	Server     string `yaml:"server"`
	AgentID    string `yaml:"agent_id"`
	AgentToken string `yaml:"agent_token"`
}

func Enroll(serverURL, enrollToken, configPath string) (*AgentConfig, error) {
	hostname, _ := os.Hostname()

	reqBody := EnrollRequest{
		Token:    enrollToken,
		Hostname: hostname,
		OS:       "linux",
		Arch:     detectArch(),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := serverURL + "/api/agent/enroll"
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("enroll request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("enroll failed (status %d): %s", resp.StatusCode, string(body))
	}

	var enrollResp EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&enrollResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	cfg := &AgentConfig{
		Server:     serverURL,
		AgentID:    enrollResp.AgentID,
		AgentToken: enrollResp.AgentToken,
	}

	if err := saveConfig(cfg, configPath); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return cfg, nil
}

func saveConfig(cfg *AgentConfig, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func detectArch() string {
	// In production, use runtime.GOARCH
	return "amd64"
}
```

- [ ] 7.2 — Create `internal/master/api/enroll.go`

```go
// internal/master/api/enroll.go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"vaultfleet/internal/master/db"
)

type EnrollRequest struct {
	Token    string `json:"token" binding:"required"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type EnrollResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

func (s *Server) HandleEnroll(c *gin.Context) {
	var req EnrollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	agent, err := s.db.FindAgentByEnrollToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired enrollment token"})
		return
	}

	if agent.AgentToken != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "token already used"})
		return
	}

	agentToken := generateAgentToken()
	now := time.Now()

	agent.AgentToken = agentToken
	agent.Status = "offline"
	agent.UpdatedAt = now
	if req.Hostname != "" && agent.Name == "" {
		agent.Name = req.Hostname
	}

	if err := s.db.UpdateAgent(agent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save agent"})
		return
	}

	c.JSON(http.StatusOK, EnrollResponse{
		AgentID:    agent.ID,
		AgentToken: agentToken,
	})
}

func generateAgentToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "ak_" + hex.EncodeToString(b)
}

// Server holds dependencies for API handlers.
type Server struct {
	db db.Repository
}

func NewServer(repo db.Repository) *Server {
	return &Server{db: repo}
}
```

- [ ] 7.3 — Update `cmd/agent/main.go` — replace the `enroll` function

```go
// In cmd/agent/main.go, replace the enroll stub:

func enroll(server, token, configPath string) (*AgentConfig, error) {
	cfg, err := enrollpkg.Enroll(server, token, configPath)
	if err != nil {
		return nil, err
	}
	return &AgentConfig{
		Server:     cfg.Server,
		AgentID:    cfg.AgentID,
		AgentToken: cfg.AgentToken,
	}, nil
}
```

Add import: `enrollpkg "vaultfleet/internal/agent/enroll"`

- [ ] 7.4 — Create `internal/agent/enroll/enroll_test.go`

```go
// internal/agent/enroll/enroll_test.go
package enroll

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEnroll_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/agent/enroll", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req EnrollRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "ek_test123", req.Token)

		resp := EnrollResponse{
			AgentID:    "agent-uuid-1",
			AgentToken: "ak_returned_token",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	cfg, err := Enroll(server.URL, "ek_test123", configPath)
	require.NoError(t, err)
	assert.Equal(t, "agent-uuid-1", cfg.AgentID)
	assert.Equal(t, "ak_returned_token", cfg.AgentToken)
	assert.Equal(t, server.URL, cfg.Server)

	// Verify config file was written
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var saved AgentConfig
	err = yaml.Unmarshal(data, &saved)
	require.NoError(t, err)
	assert.Equal(t, "ak_returned_token", saved.AgentToken)
}

func TestEnroll_InvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid or expired enrollment token"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	_, err := Enroll(server.URL, "bad-token", configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestEnroll_AlreadyUsedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"token already used"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	_, err := Enroll(server.URL, "used-token", configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 409")
}

func TestEnroll_ConfigFilePermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EnrollResponse{AgentID: "a1", AgentToken: "ak_test"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "subdir", "agent.yaml")

	_, err := Enroll(server.URL, "ek_test", configPath)
	require.NoError(t, err)

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
```

- [ ] 7.5 — Create `internal/master/api/enroll_test.go`

```go
// internal/master/api/enroll_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

type mockRepo struct {
	agents map[string]*db.Agent
}

func newMockRepo() *mockRepo {
	return &mockRepo{agents: make(map[string]*db.Agent)}
}

func (m *mockRepo) FindAgentByEnrollToken(token string) (*db.Agent, error) {
	for _, a := range m.agents {
		if a.EnrollToken == token {
			return a, nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *mockRepo) UpdateAgent(agent *db.Agent) error {
	m.agents[agent.ID] = agent
	return nil
}

func (m *mockRepo) FindAgentByToken(token string) (*db.Agent, error) {
	for _, a := range m.agents {
		if a.AgentToken == token {
			return a, nil
		}
	}
	return nil, db.ErrNotFound
}

func setupEnrollTest(repo db.Repository) *httptest.Server {
	gin.SetMode(gin.TestMode)
	s := NewServer(repo)
	router := gin.New()
	router.POST("/api/agent/enroll", s.HandleEnroll)
	return httptest.NewServer(router)
}

func TestHandleEnroll_Success(t *testing.T) {
	repo := newMockRepo()
	repo.agents["agent-1"] = &db.Agent{
		ID:          "agent-1",
		EnrollToken: "ek_valid123",
		AgentToken:  "",
		CreatedAt:   time.Now(),
	}

	server := setupEnrollTest(repo)
	defer server.Close()

	body, _ := json.Marshal(EnrollRequest{Token: "ek_valid123", Hostname: "tokyo-1"})
	resp, err := http.Post(server.URL+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var enrollResp EnrollResponse
	err = json.NewDecoder(resp.Body).Decode(&enrollResp)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", enrollResp.AgentID)
	assert.Contains(t, enrollResp.AgentToken, "ak_")
	assert.Len(t, enrollResp.AgentToken, 51) // "ak_" + 48 hex chars

	// Verify agent was updated in repo
	updated := repo.agents["agent-1"]
	assert.Equal(t, enrollResp.AgentToken, updated.AgentToken)
	assert.Equal(t, "tokyo-1", updated.Name)
}

func TestHandleEnroll_InvalidToken(t *testing.T) {
	repo := newMockRepo()
	server := setupEnrollTest(repo)
	defer server.Close()

	body, _ := json.Marshal(EnrollRequest{Token: "ek_nonexistent"})
	resp, err := http.Post(server.URL+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandleEnroll_TokenAlreadyUsed(t *testing.T) {
	repo := newMockRepo()
	repo.agents["agent-1"] = &db.Agent{
		ID:          "agent-1",
		EnrollToken: "ek_used",
		AgentToken:  "ak_already_set",
	}

	server := setupEnrollTest(repo)
	defer server.Close()

	body, _ := json.Marshal(EnrollRequest{Token: "ek_used"})
	resp, err := http.Post(server.URL+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestHandleEnroll_MissingToken(t *testing.T) {
	repo := newMockRepo()
	server := setupEnrollTest(repo)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"hostname": "test"})
	resp, err := http.Post(server.URL+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
```

- [ ] 7.6 — Add integration test: full enrollment → WS connect → policy push

```go
// internal/master/ws/handler_test.go (append)

func TestHandler_FullRegistrationFlow(t *testing.T) {
	// 1. Setup: Mock an agent that just enrolled (synced=false, has a pending policy)
	policyMsg := &protocol.Message{
		Type:    protocol.TypePolicyPush,
		ID:      "policy-initial",
		Payload: []byte(`{"agent_id":"agent-new","schedule":"0 4 * * *"}`),
	}

	server, hub, bus := setupTestHandler(
		func(token string) (string, error) {
			if token == "ak_freshly_enrolled" {
				return "agent-new", nil
			}
			return "", errors.New("invalid")
		},
		func(agentID string) (*protocol.Message, bool) {
			if agentID == "agent-new" {
				return policyMsg, true
			}
			return nil, false
		},
	)
	defer server.Close()

	// Track online event
	var onlineEvents []string
	var mu sync.Mutex
	bus.Subscribe(events.AgentOnline, func(e events.Event) {
		mu.Lock()
		onlineEvents = append(onlineEvents, e.Payload.(string))
		mu.Unlock()
	})

	// 2. Agent connects with freshly enrolled token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server, "ak_freshly_enrolled"), nil)
	require.NoError(t, err)
	defer conn.Close()

	// 3. Agent should receive policy push immediately
	var received protocol.Message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = conn.ReadJSON(&received)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypePolicyPush, received.Type)
	assert.Equal(t, "policy-initial", received.ID)

	// 4. Verify agent is tracked online
	time.Sleep(50 * time.Millisecond)
	assert.True(t, hub.IsOnline("agent-new"))

	// 5. Verify event was published
	mu.Lock()
	assert.Contains(t, onlineEvents, "agent-new")
	mu.Unlock()
}
```

- [ ] 7.7 — Run tests

```bash
go test ./internal/agent/enroll/... -v
go test ./internal/master/api/... -run TestHandleEnroll -v
go test ./internal/master/ws/... -run TestHandler_FullRegistrationFlow -v
```

- [ ] 7.8 — Verify all tests pass

```bash
go test ./internal/agent/enroll/... -v
go test ./internal/master/api/... -v
go test ./internal/master/ws/... -v
```

- [ ] 7.9 — Commit

```bash
git add cmd/agent/ internal/agent/enroll/ internal/master/api/enroll.go internal/master/api/enroll_test.go internal/master/ws/handler.go internal/master/ws/handler_test.go
git commit -m "feat: implement agent enrollment flow with end-to-end policy push

- Agent enrollment: POST /api/agent/enroll exchanges one-time token for agent_token
- Agent config saved to /etc/vaultfleet/agent.yaml on successful enrollment
- Master auto-pushes policy on WebSocket connect if agent has pending sync
- Full e2e test: enroll → token exchange → WS connect → policy auto-push
- Token reuse prevention (409 Conflict on duplicate enrollment)"
```

---

### Task 8: Heartbeat + Offline Detection

**Files:**
- `internal/master/ws/monitor.go`
- `internal/master/ws/hub.go` (extend: update last_seen_at)
- `internal/master/ws/monitor_test.go`

**Steps:**

- [ ] 8.1 — Create `internal/master/ws/monitor.go`

```go
// internal/master/ws/monitor.go
package ws

import (
	"context"
	"log"
	"time"

	"vaultfleet/internal/master/events"
)

const (
	MonitorInterval  = 10 * time.Second
	OfflineThreshold = 60 * time.Second
)

type Monitor struct {
	hub      *Hub
	eventBus *events.Bus
	interval time.Duration
	threshold time.Duration
	nowFunc  func() time.Time
}

func NewMonitor(hub *Hub, eventBus *events.Bus) *Monitor {
	return &Monitor{
		hub:       hub,
		eventBus:  eventBus,
		interval:  MonitorInterval,
		threshold: OfflineThreshold,
		nowFunc:   time.Now,
	}
}

func NewMonitorWithConfig(hub *Hub, eventBus *events.Bus, interval, threshold time.Duration, nowFunc func() time.Time) *Monitor {
	return &Monitor{
		hub:       hub,
		eventBus:  eventBus,
		interval:  interval,
		threshold: threshold,
		nowFunc:   nowFunc,
	}
}

func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scan()
		}
	}
}

func (m *Monitor) scan() {
	now := m.nowFunc()
	agents := m.hub.GetAllAgents()

	for agentID, status := range agents {
		if !status.Online {
			continue
		}
		if now.Sub(status.LastSeenAt) > m.threshold {
			log.Printf("agent %s offline: last seen %v ago", agentID, now.Sub(status.LastSeenAt))
			m.hub.MarkOffline(agentID)
			m.eventBus.Publish(events.Event{
				Type:    events.AgentOffline,
				Payload: agentID,
			})
		}
	}
}
```

- [ ] 8.2 — Extend `internal/master/ws/hub.go` — add `MarkOffline` method

```go
// Add to internal/master/ws/hub.go:

func (h *Hub) MarkOffline(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if status, ok := h.agents[agentID]; ok {
		status.Online = false
	}
}
```

- [ ] 8.3 — Extend `internal/master/ws/handler.go` — update `dispatch` for heartbeat

The existing dispatch already calls `h.hub.UpdateLastSeen(agentID, timeNow())` on heartbeat. Ensure this also marks the agent back online if it was marked offline (reconnection scenario):

```go
// Update the heartbeat case in dispatch method:

case protocol.TypeHeartbeat:
	now := timeNow()
	h.hub.UpdateLastSeen(agentID, now)
	h.hub.MarkOnline(agentID)
```

Add `MarkOnline` to hub:

```go
// Add to internal/master/ws/hub.go:

func (h *Hub) MarkOnline(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if status, ok := h.agents[agentID]; ok {
		status.Online = true
	}
}
```

- [ ] 8.4 — Create `internal/master/ws/monitor_test.go`

```go
// internal/master/ws/monitor_test.go
package ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/events"
)

func TestMonitor_DetectsOfflineAgent(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()

	// Add agent with last_seen 2 minutes ago
	hub.Add("agent-1", &SafeConn{})
	hub.UpdateLastSeen("agent-1", time.Now().Add(-2*time.Minute))

	var offlineAgents []string
	var mu sync.Mutex
	bus.Subscribe(events.AgentOffline, func(e events.Event) {
		mu.Lock()
		offlineAgents = append(offlineAgents, e.Payload.(string))
		mu.Unlock()
	})

	monitor := NewMonitorWithConfig(hub, bus, 50*time.Millisecond, 60*time.Second, time.Now)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go monitor.Run(ctx)

	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, offlineAgents, "agent-1")
	assert.False(t, hub.IsOnline("agent-1"))
}

func TestMonitor_IgnoresRecentlySeenAgent(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()

	// Add agent with last_seen just now
	hub.Add("agent-alive", &SafeConn{})
	hub.UpdateLastSeen("agent-alive", time.Now())

	var offlineAgents []string
	var mu sync.Mutex
	bus.Subscribe(events.AgentOffline, func(e events.Event) {
		mu.Lock()
		offlineAgents = append(offlineAgents, e.Payload.(string))
		mu.Unlock()
	})

	monitor := NewMonitorWithConfig(hub, bus, 50*time.Millisecond, 60*time.Second, time.Now)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go monitor.Run(ctx)

	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, offlineAgents)
	assert.True(t, hub.IsOnline("agent-alive"))
}

func TestMonitor_AgentComesBackOnline(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()

	// Agent goes offline
	hub.Add("agent-1", &SafeConn{})
	hub.UpdateLastSeen("agent-1", time.Now().Add(-2*time.Minute))

	monitor := NewMonitorWithConfig(hub, bus, 50*time.Millisecond, 60*time.Second, time.Now)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go monitor.Run(ctx)
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	// Agent was marked offline
	assert.False(t, hub.IsOnline("agent-1"))

	// Simulate reconnection: hub.Add re-adds with Online=true
	hub.Add("agent-1", &SafeConn{})
	assert.True(t, hub.IsOnline("agent-1"))
}

func TestMonitor_MultipleAgentsMixedState(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()

	hub.Add("agent-ok", &SafeConn{})
	hub.UpdateLastSeen("agent-ok", time.Now())

	hub.Add("agent-stale", &SafeConn{})
	hub.UpdateLastSeen("agent-stale", time.Now().Add(-3*time.Minute))

	hub.Add("agent-borderline", &SafeConn{})
	hub.UpdateLastSeen("agent-borderline", time.Now().Add(-59*time.Second))

	var offlineAgents []string
	var mu sync.Mutex
	bus.Subscribe(events.AgentOffline, func(e events.Event) {
		mu.Lock()
		offlineAgents = append(offlineAgents, e.Payload.(string))
		mu.Unlock()
	})

	monitor := NewMonitorWithConfig(hub, bus, 50*time.Millisecond, 60*time.Second, time.Now)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go monitor.Run(ctx)
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	assert.Contains(t, offlineAgents, "agent-stale")
	assert.NotContains(t, offlineAgents, "agent-ok")
	assert.NotContains(t, offlineAgents, "agent-borderline")

	assert.True(t, hub.IsOnline("agent-ok"))
	assert.False(t, hub.IsOnline("agent-stale"))
	assert.True(t, hub.IsOnline("agent-borderline"))
}

func TestHub_MarkOnlineAfterHeartbeat(t *testing.T) {
	hub := NewHub()

	hub.Add("agent-1", &SafeConn{})
	hub.MarkOffline("agent-1")
	assert.False(t, hub.IsOnline("agent-1"))

	hub.MarkOnline("agent-1")
	assert.True(t, hub.IsOnline("agent-1"))
}
```

- [ ] 8.5 — Run tests (expect fail initially, then pass after implementation)

```bash
go test ./internal/master/ws/... -run TestMonitor -v
go test ./internal/master/ws/... -run TestHub_MarkOnline -v
```

- [ ] 8.6 — Verify all tests pass

```bash
go test ./internal/master/ws/... -v
```

- [ ] 8.7 — Commit

```bash
git add internal/master/ws/monitor.go internal/master/ws/monitor_test.go internal/master/ws/hub.go internal/master/ws/handler.go
git commit -m "feat(master): add heartbeat monitoring and offline detection

- Monitor goroutine scans agents every 10s, marks offline if no heartbeat >60s
- Hub extended with MarkOffline/MarkOnline for state transitions
- Agent reconnection resets online status via MarkOnline on heartbeat
- Offline events published to event bus for notification system
- Tests: offline detection, alive agent ignored, reconnection recovery, mixed states"
```
