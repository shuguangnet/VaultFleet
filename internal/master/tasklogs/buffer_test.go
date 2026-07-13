package tasklogs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
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

func TestPersistentBufferRestoresLogsAfterRestart(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	first := NewPersistentBufferWithLimits(database, 10, 1024, time.Hour)
	first.now = func() time.Time { return now }
	first.Add("agent-1", "scheduled-msg-1", protocol.TaskLogPayload{
		TaskType: "backup", Sequence: 1, Level: "info", Phase: "init", Stream: "system", Line: "start",
	})
	first.Add("agent-1", "scheduled-msg-1", protocol.TaskLogPayload{
		TaskType: "backup", Sequence: 2, Level: "info", Phase: "backup", Stream: "stdout", Line: "uploaded",
	})
	first.MarkComplete("agent-1", "scheduled-msg-1")
	var persisted int64
	require.NoError(t, database.DB.Model(&db.TaskLog{}).Where("agent_id = ? AND message_id = ?", "agent-1", "scheduled-msg-1").Count(&persisted).Error)
	require.Equal(t, int64(2), persisted)

	restarted := NewPersistentBufferWithLimits(database, 10, 1024, time.Hour)
	require.NoError(t, database.DB.Model(&db.TaskLog{}).Where("agent_id = ? AND message_id = ?", "agent-1", "scheduled-msg-1").Count(&persisted).Error)
	require.Equal(t, int64(2), persisted)
	restarted.now = func() time.Time { return now.Add(30 * time.Minute) }
	snapshot := restarted.Get("agent-1", "scheduled-msg-1", 0, 10)

	require.True(t, snapshot.Exists)
	require.Len(t, snapshot.Lines, 2)
	assert.Equal(t, int64(2), snapshot.LatestSequence)
	assert.Equal(t, "start", snapshot.Lines[0].Line)
	assert.Equal(t, "uploaded", snapshot.Lines[1].Line)
	assert.Equal(t, "scheduled-msg-1", snapshot.Lines[1].MessageID)

	restarted.now = func() time.Time { return now.Add(2 * time.Hour) }
	expired := restarted.Get("agent-1", "scheduled-msg-1", 0, 10)
	assert.True(t, expired.Exists)
	assert.True(t, expired.Expired)

	afterExpiry := NewPersistentBufferWithLimits(database, 10, 1024, time.Hour)
	assert.False(t, afterExpiry.Get("agent-1", "scheduled-msg-1", 0, 10).Exists)
}

func TestPersistentBufferRestoresTruncationState(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	buffer := NewPersistentBufferWithLimits(database, 2, 1024, time.Hour)
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 1, Line: "one"})
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 2, Line: "two"})
	buffer.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 3, Line: "three"})

	restarted := NewPersistentBufferWithLimits(database, 2, 1024, time.Hour)
	snapshot := restarted.Get("agent-1", "msg-1", 0, 10)

	require.Len(t, snapshot.Lines, 2)
	assert.Equal(t, int64(1), snapshot.DroppedLines)
	assert.True(t, snapshot.Truncated)
	assert.Equal(t, int64(2), snapshot.Lines[0].Sequence)
}

func TestPersistentBufferAppendsAfterRestartWithoutDroppingEarlierLines(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	first := NewPersistentBufferWithLimits(database, 10, 1024, time.Hour)
	first.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 1, Line: "before restart"})

	restarted := NewPersistentBufferWithLimits(database, 10, 1024, time.Hour)
	restarted.Add("agent-1", "msg-1", protocol.TaskLogPayload{Sequence: 2, Line: "after restart"})

	snapshot := restarted.Get("agent-1", "msg-1", 0, 10)
	require.Len(t, snapshot.Lines, 2)
	assert.Equal(t, "before restart", snapshot.Lines[0].Line)
	assert.Equal(t, "after restart", snapshot.Lines[1].Line)
}
