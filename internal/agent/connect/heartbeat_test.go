package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestHeartbeat_SendsAtInterval(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	received := make(chan protocol.Message, 8)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		for {
			var msg protocol.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			received <- msg
		}
	}))
	defer server.Close()

	client := NewClient(httpToWSURL(t, server.URL), "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		client.Run(ctx)
		close(runDone)
	}()

	collector := func() SystemInfo {
		return SystemInfo{
			OS:            "linux",
			Arch:          "amd64",
			CPUCount:      8,
			MemoryTotalMB: 16384,
			DiskTotalGB:   512,
			DiskUsedGB:    128,
			ResticVersion: "restic 0.16.0",
			RcloneVersion: "rclone 1.65.0",
		}
	}
	go RunHeartbeat(ctx, client, collector, 25*time.Millisecond)

	var messages []protocol.Message
	require.Eventually(t, func() bool {
		for len(messages) < 3 {
			select {
			case msg := <-received:
				messages = append(messages, msg)
			default:
				return false
			}
		}
		return true
	}, time.Second, 5*time.Millisecond)

	cancel()
	client.Close()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("client Run did not stop")
	}

	for _, msg := range messages[:3] {
		assert.Equal(t, protocol.TypeHeartbeat, msg.Type)
		assert.NotEmpty(t, msg.ID)
		payload, err := protocol.ParsePayload[protocol.HeartbeatPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, "restic 0.16.0", payload.ResticVersion)
		assert.Equal(t, "rclone 1.65.0", payload.RcloneVersion)
	}
}

func TestDefaultSystemInfoCollectorIncludesRuntimeFields(t *testing.T) {
	info := DefaultSystemInfoCollector()

	assert.NotEmpty(t, info.OS)
	assert.NotEmpty(t, info.Arch)
	assert.Positive(t, info.CPUCount)
}
