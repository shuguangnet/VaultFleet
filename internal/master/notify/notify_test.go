package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifyMessageLevels(t *testing.T) {
	levels := []Level{LevelInfo, LevelWarning, LevelError}
	require.Len(t, levels, 3)

	seen := make(map[Level]bool)
	for _, level := range levels {
		assert.NotEmpty(t, string(level))
		assert.False(t, seen[level], "duplicate level: %s", level)
		seen[level] = true
	}
}

func TestNotifyMessageCarriesNotificationFields(t *testing.T) {
	msg := NotifyMessage{
		Title:     "Backup Failed",
		Body:      "restic exit code 1 - repository locked",
		Level:     LevelError,
		AgentName: "Tokyo-1",
		Timestamp: time.Date(2026, 5, 18, 3, 0, 15, 0, time.UTC),
	}

	assert.Equal(t, "Backup Failed", msg.Title)
	assert.Equal(t, "restic exit code 1 - repository locked", msg.Body)
	assert.Equal(t, LevelError, msg.Level)
	assert.Equal(t, "Tokyo-1", msg.AgentName)
	assert.Equal(t, time.Date(2026, 5, 18, 3, 0, 15, 0, time.UTC), msg.Timestamp)
}

func TestNotifierInterface(t *testing.T) {
	var n Notifier = &mockNotifier{}
	msg := NotifyMessage{
		Title:     "Test",
		Body:      "test body",
		Level:     LevelInfo,
		AgentName: "Agent-1",
		Timestamp: time.Now(),
	}

	err := n.Send(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, "mock", n.Type())
	assert.Equal(t, []NotifyMessage{msg}, n.(*mockNotifier).sent)
}

func TestNotifierInterfacePropagatesSendErrors(t *testing.T) {
	expected := errors.New("send failed")
	var n Notifier = &mockNotifier{err: expected}

	err := n.Send(context.Background(), NotifyMessage{Title: "Test"})

	assert.ErrorIs(t, err, expected)
}

type mockNotifier struct {
	sent []NotifyMessage
	err  error
}

func (m *mockNotifier) Send(_ context.Context, msg NotifyMessage) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockNotifier) Type() string {
	return "mock"
}
