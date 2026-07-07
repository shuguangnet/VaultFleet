package ws

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/events"
	"vaultfleet/internal/master/tasklogs"
	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

func TestDispatchTaskLogStoresRedactedPayload(t *testing.T) {
	handler := NewHandler(NewHub(), events.NewBus(), validTestAuth, noPolicy, nil)
	handler.TaskLogBuffer = tasklogs.NewBuffer()
	msg, err := protocol.NewMessage(protocol.TypeTaskLog, protocol.TaskLogPayload{
		AgentID:   "spoofed",
		MessageID: "",
		Sequence:  1,
		Line:      "running password=secret",
	})
	require.NoError(t, err)
	msg.ID = "msg-1"

	handler.dispatch("agent-1", *msg)

	snapshot := handler.TaskLogBuffer.Get("agent-1", "msg-1", 0, 10)
	require.True(t, snapshot.Exists)
	require.Len(t, snapshot.Lines, 1)
	assert.Equal(t, "agent-1", snapshot.Lines[0].AgentID)
	assert.Equal(t, "msg-1", snapshot.Lines[0].MessageID)
	assert.Contains(t, snapshot.Lines[0].Line, "password="+redact.Placeholder)
}

func TestDispatchTaskResultMarksTaskLogsComplete(t *testing.T) {
	handler := NewHandler(NewHub(), events.NewBus(), validTestAuth, noPolicy, nil)
	handler.TaskLogBuffer = tasklogs.NewBuffer()
	handler.TaskLogBuffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 1, Line: "start"})

	handler.dispatch("agent-1", protocol.Message{ID: "msg-1", Type: protocol.TypeTaskResult})

	snapshot := handler.TaskLogBuffer.Get("agent-1", "msg-1", 0, 10)
	assert.True(t, snapshot.Exists)
	assert.Len(t, snapshot.Lines, 1)
}
