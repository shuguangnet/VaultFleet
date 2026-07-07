package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

func TestTaskLogEmitterRedactsSplitsAndSequencesLines(t *testing.T) {
	var sent []protocol.Message
	emitter := newTaskLogEmitter("agent-1", "msg-1", "backup", func(msg protocol.Message) error {
		sent = append(sent, msg)
		return nil
	})

	emitter.Stdout("hook", "first token=secret\nsecond Authorization: Bearer abc")

	require.Len(t, sent, 2)
	first, err := protocol.ParsePayload[protocol.TaskLogPayload](&sent[0])
	require.NoError(t, err)
	second, err := protocol.ParsePayload[protocol.TaskLogPayload](&sent[1])
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeTaskLog, sent[0].Type)
	assert.Equal(t, "msg-1", sent[0].ID)
	assert.Equal(t, int64(1), first.Sequence)
	assert.Equal(t, int64(2), second.Sequence)
	assert.Equal(t, "agent-1", first.AgentID)
	assert.Equal(t, "msg-1", first.MessageID)
	assert.Equal(t, "backup", first.TaskType)
	assert.Equal(t, "hook", first.Phase)
	assert.Equal(t, "stdout", first.Stream)
	assert.Contains(t, first.Line, "token="+redact.Placeholder)
	assert.Contains(t, second.Line, "Authorization: Bearer "+redact.Placeholder)
}

func TestTaskLogEmitterTruncatesLongUTF8Lines(t *testing.T) {
	var sent []protocol.Message
	emitter := newTaskLogEmitter("agent-1", "msg-1", "backup", func(msg protocol.Message) error {
		sent = append(sent, msg)
		return nil
	})

	emitter.Info("backup", strings.Repeat("界", maxTaskLogLineBytes))

	require.Len(t, sent, 1)
	payload, err := protocol.ParsePayload[protocol.TaskLogPayload](&sent[0])
	require.NoError(t, err)
	assert.True(t, payload.Truncated)
	assert.LessOrEqual(t, len(payload.Line), maxTaskLogLineBytes)
	assert.True(t, strings.HasPrefix(strings.Repeat("界", maxTaskLogLineBytes), payload.Line))
}

func TestTaskLogEmitterIgnoresMissingMessageIDForOldMasterTolerance(t *testing.T) {
	called := false
	emitter := newTaskLogEmitter("agent-1", "", "backup", func(protocol.Message) error {
		called = true
		return nil
	})

	emitter.Info("backup", "line")

	assert.False(t, called)
}
