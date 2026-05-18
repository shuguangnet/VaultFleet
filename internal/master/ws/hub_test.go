package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestHub_AddAndRemove(t *testing.T) {
	hub := NewHub()
	conn := &SafeConn{}

	beforeAdd := time.Now()
	hub.Add("agent-1", conn)

	agents := hub.GetAllAgents()
	status, ok := agents["agent-1"]
	require.True(t, ok)
	assert.Same(t, conn, status.Conn)
	assert.True(t, status.Online)
	assert.False(t, status.LastSeenAt.Before(beforeAdd))
	assert.True(t, hub.IsOnline("agent-1"))

	hub.Remove("agent-1")

	assert.False(t, hub.IsOnline("agent-1"))
	assert.NotContains(t, hub.GetAllAgents(), "agent-1")
}

func TestHub_SendToOnlineAgent(t *testing.T) {
	hub := NewHub()

	err := hub.Send("missing-agent", map[string]string{"type": "heartbeat"})

	assert.ErrorIs(t, err, ErrAgentNotConnected)
}

func TestHub_UpdateLastSeen(t *testing.T) {
	hub := NewHub()
	conn := &SafeConn{}
	initial := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 18, 9, 30, 0, 0, time.UTC)

	hub.Add("agent-1", conn)
	hub.UpdateLastSeen("agent-1", initial)
	hub.UpdateLastSeen("agent-1", updated)
	hub.UpdateLastSeen("missing-agent", time.Now())

	status := hub.GetAllAgents()["agent-1"]
	require.NotNil(t, status)
	assert.Equal(t, updated, status.LastSeenAt)
}

func TestHub_MarkOnlineAndMarkOffline(t *testing.T) {
	hub := NewHub()
	hub.Add("agent-1", &SafeConn{})

	assert.True(t, hub.MarkOffline("agent-1"))
	assert.False(t, hub.IsOnline("agent-1"))

	assert.False(t, hub.MarkOffline("agent-1"))
	assert.True(t, hub.MarkOnline("agent-1"))
	assert.True(t, hub.IsOnline("agent-1"))

	assert.False(t, hub.MarkOnline("agent-1"))
	assert.False(t, hub.MarkOffline("missing-agent"))
	assert.False(t, hub.MarkOnline("missing-agent"))
}

func TestHub_MarkOfflineIfStaleRechecksCurrentLastSeen(t *testing.T) {
	hub := NewHub()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-1", &SafeConn{})
	hub.UpdateLastSeen("agent-1", now.Add(-2*time.Minute))

	snapshot := hub.GetAllAgents()["agent-1"]
	require.NotNil(t, snapshot)
	require.True(t, now.Sub(snapshot.LastSeenAt) > time.Minute)

	hub.UpdateLastSeen("agent-1", now)

	assert.False(t, hub.MarkOfflineIfStale("agent-1", now, time.Minute))
	assert.True(t, hub.IsOnline("agent-1"))

	hub.UpdateLastSeen("agent-1", now.Add(-2*time.Minute))
	assert.True(t, hub.MarkOfflineIfStale("agent-1", now, time.Minute))
	assert.False(t, hub.IsOnline("agent-1"))
	assert.False(t, hub.MarkOfflineIfStale("agent-1", now, time.Minute))
}

func TestHub_GetAllAgents(t *testing.T) {
	hub := NewHub()
	firstConn := &SafeConn{}
	secondConn := &SafeConn{}

	hub.Add("agent-1", firstConn)
	hub.Add("agent-2", secondConn)

	agents := hub.GetAllAgents()

	require.Len(t, agents, 2)
	assert.Same(t, firstConn, agents["agent-1"].Conn)
	assert.Same(t, secondConn, agents["agent-2"].Conn)
	assert.True(t, agents["agent-1"].Online)
	assert.True(t, agents["agent-2"].Online)
}

func TestHub_GetAllAgentsReturnsStatusCopy(t *testing.T) {
	hub := NewHub()
	hub.Add("agent-1", &SafeConn{})

	agents := hub.GetAllAgents()
	require.Contains(t, agents, "agent-1")

	agents["agent-1"].Online = false
	delete(agents, "agent-1")

	assert.True(t, hub.IsOnline("agent-1"))
	assert.Contains(t, hub.GetAllAgents(), "agent-1")
}

func TestHub_AddReplacesAndClosesPreviousConnection(t *testing.T) {
	hub := NewHub()
	oldConn := &SafeConn{}
	newConn := &SafeConn{}

	hub.Add("agent-1", oldConn)
	hub.Add("agent-1", newConn)

	assert.ErrorIs(t, oldConn.WriteJSON(map[string]string{"type": "heartbeat"}), ErrNilConn)
	status := hub.GetAllAgents()["agent-1"]
	require.NotNil(t, status)
	assert.Same(t, newConn, status.Conn)
	assert.True(t, hub.IsOnline("agent-1"))
}

func TestHub_RemoveIfCurrentDoesNotRemoveReplacementConnection(t *testing.T) {
	hub := NewHub()
	oldConn := &SafeConn{}
	newConn := &SafeConn{}

	hub.Add("agent-1", oldConn)
	hub.Add("agent-1", newConn)
	hub.RemoveIfCurrent("agent-1", oldConn)

	status := hub.GetAllAgents()["agent-1"]
	require.NotNil(t, status)
	assert.Same(t, newConn, status.Conn)
	assert.True(t, hub.IsOnline("agent-1"))

	hub.RemoveIfCurrent("agent-1", newConn)

	assert.False(t, hub.IsOnline("agent-1"))
	assert.NotContains(t, hub.GetAllAgents(), "agent-1")
}

func TestHub_SendConcurrentWithRemoveIsRaceFree(t *testing.T) {
	hub := NewHub()
	hub.Add("agent-1", &SafeConn{})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				_ = hub.Send("agent-1", map[string]string{"type": "heartbeat"})
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for j := 0; j < 100; j++ {
			hub.Remove("agent-1")
			hub.Add("agent-1", &SafeConn{})
		}
	}()

	close(start)
	wg.Wait()
}

func TestHub_SendAndWaitReturnsMatchingBrowseResponse(t *testing.T) {
	hub := NewHub()
	clientConn := addTestWebSocketAgent(t, hub, "agent-1")
	request := protocol.Message{
		Type:    protocol.TypeDirBrowseReq,
		ID:      "browse-1",
		Payload: json.RawMessage(`{"path":"/etc","depth":2}`),
	}
	response := protocol.Message{
		Type:    protocol.TypeDirBrowseResp,
		ID:      "browse-1",
		Payload: json.RawMessage(`{"path":"/etc","entries":[]}`),
	}

	respCh, err := hub.SendAndWait("agent-1", request, 500*time.Millisecond)
	require.NoError(t, err)
	require.True(t, hub.HasWaiter("agent-1", "browse-1"))
	var sent protocol.Message
	require.NoError(t, clientConn.ReadJSON(&sent))
	assert.Equal(t, request, sent)

	assert.True(t, hub.HandleResponse("agent-1", response))

	select {
	case got := <-respCh:
		assert.Equal(t, response, got)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
	assert.False(t, hub.HasWaiter("agent-1", "browse-1"))
}

func TestHub_SendAndWaitCleansWaiterOnTimeout(t *testing.T) {
	hub := NewHub()
	clientConn := addTestWebSocketAgent(t, hub, "agent-1")

	respCh, err := hub.SendAndWait("agent-1", protocol.Message{
		Type:    protocol.TypeDirBrowseReq,
		ID:      "browse-timeout",
		Payload: json.RawMessage(`{"path":"/","depth":2}`),
	}, 10*time.Millisecond)

	require.NoError(t, err)
	var sent protocol.Message
	require.NoError(t, clientConn.ReadJSON(&sent))
	_, ok := <-respCh
	assert.False(t, ok)
	assert.False(t, hub.HasWaiter("agent-1", "browse-timeout"))
}

func TestHub_HandleResponseDoesNotSatisfyWrongWaiter(t *testing.T) {
	hub := NewHub()
	clientConn := addTestWebSocketAgent(t, hub, "agent-1")
	respCh, err := hub.SendAndWait("agent-1", protocol.Message{
		Type:    protocol.TypeDirBrowseReq,
		ID:      "browse-1",
		Payload: json.RawMessage(`{"path":"/","depth":2}`),
	}, 50*time.Millisecond)
	require.NoError(t, err)
	var sent protocol.Message
	require.NoError(t, clientConn.ReadJSON(&sent))

	matched := hub.HandleResponse("agent-1", protocol.Message{
		Type: protocol.TypeDirBrowseResp,
		ID:   "browse-2",
	})

	assert.False(t, matched)
	assert.True(t, hub.HasWaiter("agent-1", "browse-1"))
	select {
	case resp, ok := <-respCh:
		t.Fatalf("wrong waiter was satisfied: %#v ok=%v", resp, ok)
	default:
	}

	<-respCh
	assert.False(t, hub.HasWaiter("agent-1", "browse-1"))
}

func addTestWebSocketAgent(t *testing.T, hub *Hub, agentID string) *websocket.Conn {
	t.Helper()

	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConnCh <- conn
	}))
	t.Cleanup(server.Close)

	u := "ws" + server.URL[len("http"):]
	clientConn, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = clientConn.Close()
	})

	select {
	case serverConn := <-serverConnCh:
		hub.Add(agentID, NewSafeConn(serverConn))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket upgrade")
	}
	return clientConn
}
