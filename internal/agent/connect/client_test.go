package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{name: "zero starts initial", current: 0, want: InitialBackoff},
		{name: "double one second", current: time.Second, want: 2 * time.Second},
		{name: "double two seconds", current: 2 * time.Second, want: 4 * time.Second},
		{name: "cap at max", current: 4 * time.Minute, want: MaxBackoff},
		{name: "already max", current: MaxBackoff, want: MaxBackoff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextBackoff(tt.current))
		})
	}
}

func TestBackoffForAttempt(t *testing.T) {
	assert.Equal(t, 1*time.Second, BackoffForAttempt(0))
	assert.Equal(t, 2*time.Second, BackoffForAttempt(1))
	assert.Equal(t, 4*time.Second, BackoffForAttempt(2))
	assert.Equal(t, 8*time.Second, BackoffForAttempt(3))
	assert.Equal(t, MaxBackoff, BackoffForAttempt(100))
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
		MaxBackoff,
	}

	for _, want := range expected {
		backoff = nextBackoff(backoff)
		assert.Equal(t, want, backoff)
	}
}

func TestAgentWebSocketURLNormalizesSchemesAndToken(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		token     string
		want      string
	}{
		{name: "ws remains ws", serverURL: "ws://master.example:8080", token: "abc123", want: "ws://master.example:8080/ws/agent?token=abc123"},
		{name: "http becomes ws", serverURL: "http://master.example", token: "abc123", want: "ws://master.example/ws/agent?token=abc123"},
		{name: "https becomes wss and keeps base path", serverURL: "https://master.example/base", token: "a b&c", want: "wss://master.example/base/ws/agent?token=a+b%26c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agentWebSocketURL(tt.serverURL, tt.token)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientConnectReadLoopAndSend(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverReceived := make(chan protocol.Message, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ws/agent", r.URL.Path)
		assert.Equal(t, "test-token", r.URL.Query().Get("token"))

		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		outbound, err := protocol.NewMessage(protocol.TypeHeartbeat, protocol.HeartbeatPayload{ResticVersion: "server"})
		require.NoError(t, err)
		require.NoError(t, conn.WriteJSON(outbound))

		var inbound protocol.Message
		if err := conn.ReadJSON(&inbound); err == nil {
			serverReceived <- inbound
		}
	}))
	defer server.Close()

	serverURL := httpToWSURL(t, server.URL)
	clientReceived := make(chan protocol.Message, 1)
	client := NewClient(serverURL, "test-token", func(msg protocol.Message) {
		clientReceived <- msg
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(runDone)
	}()
	defer func() {
		cancel()
		client.Close()
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Fatal("client Run did not stop")
		}
	}()

	select {
	case msg := <-clientReceived:
		assert.Equal(t, protocol.TypeHeartbeat, msg.Type)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message from server")
	}

	msg, err := protocol.NewMessage(protocol.TypeHeartbeat, protocol.HeartbeatPayload{ResticVersion: "client"})
	require.NoError(t, err)
	require.NoError(t, client.Send(*msg))

	select {
	case got := <-serverReceived:
		assert.Equal(t, protocol.TypeHeartbeat, got.Type)
		payload, err := protocol.ParsePayload[protocol.HeartbeatPayload](&got)
		require.NoError(t, err)
		assert.Equal(t, "client", payload.ResticVersion)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message from client")
	}
}

func TestClientSendReturnsErrNotConnected(t *testing.T) {
	client := NewClient("ws://example.invalid", "token", nil)

	err := client.Send(protocol.Message{Type: protocol.TypeHeartbeat})

	assert.ErrorIs(t, err, ErrNotConnected)
}

func httpToWSURL(t *testing.T, rawURL string) string {
	t.Helper()

	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	u.Scheme = "ws"
	return u.String()
}
