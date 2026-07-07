package tasklogs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

func TestBufferStoresIncrementalRedactedLines(t *testing.T) {
	buffer := NewBufferWithLimits(10, 1024, time.Hour)
	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	buffer.now = func() time.Time { return now }

	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{AgentID: "spoofed", MessageID: "other", Sequence: 1, Line: "start token=secret"})
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 2, Line: "done"})

	snapshot := buffer.Get("agent-1", "msg-1", 1, 10)

	require.True(t, snapshot.Exists)
	require.Len(t, snapshot.Lines, 1)
	assert.Equal(t, int64(2), snapshot.LatestSequence)
	assert.Equal(t, "agent-1", snapshot.Lines[0].AgentID)
	assert.Equal(t, "msg-1", snapshot.Lines[0].MessageID)
	assert.Equal(t, "done", snapshot.Lines[0].Line)

	full := buffer.Get("agent-1", "msg-1", 0, 10)
	require.Len(t, full.Lines, 2)
	assert.Contains(t, full.Lines[0].Line, "token="+redact.Placeholder)
}

func TestBufferEnforcesLineAndByteLimits(t *testing.T) {
	buffer := NewBufferWithLimits(2, 6, time.Hour)

	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 1, Line: "12345"})
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 2, Line: "67890"})
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 3, Line: "abc"})

	snapshot := buffer.Get("agent-1", "msg-1", 0, 10)

	require.True(t, snapshot.Exists)
	assert.True(t, snapshot.Truncated)
	assert.Equal(t, int64(2), snapshot.DroppedLines)
	require.Len(t, snapshot.Lines, 1)
	assert.Equal(t, int64(3), snapshot.Lines[0].Sequence)
}

func TestBufferExpiresCompletedLogs(t *testing.T) {
	buffer := NewBufferWithLimits(10, 1024, time.Minute)
	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	buffer.now = func() time.Time { return now }
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 1, Line: "start"})
	buffer.MarkComplete("agent-1", "msg-1")
	now = now.Add(2 * time.Minute)

	snapshot := buffer.Get("agent-1", "msg-1", 0, 10)

	assert.True(t, snapshot.Exists)
	assert.True(t, snapshot.Expired)
	assert.Empty(t, snapshot.Lines)
	assert.False(t, buffer.Get("agent-1", "msg-1", 0, 10).Exists)
}
