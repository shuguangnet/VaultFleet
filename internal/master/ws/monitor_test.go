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
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-1", &SafeConn{})
	hub.UpdateLastSeen("agent-1", now.Add(-2*time.Minute))

	offlineEvents := subscribeOfflineEvents(bus)
	monitor := NewMonitorWithConfig(hub, bus, 10*time.Millisecond, time.Minute, func() time.Time {
		return now
	})

	monitor.scan()

	assert.Equal(t, []string{"agent-1"}, offlineEvents.snapshot())
	assert.False(t, hub.IsOnline("agent-1"))
}

func TestMonitor_IgnoresRecentlySeenAgent(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-alive", &SafeConn{})
	hub.UpdateLastSeen("agent-alive", now.Add(-30*time.Second))

	offlineEvents := subscribeOfflineEvents(bus)
	monitor := NewMonitorWithConfig(hub, bus, 10*time.Millisecond, time.Minute, func() time.Time {
		return now
	})

	monitor.scan()

	assert.Empty(t, offlineEvents.snapshot())
	assert.True(t, hub.IsOnline("agent-alive"))
}

func TestMonitor_MultipleAgentsMixedState(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-ok", &SafeConn{})
	hub.UpdateLastSeen("agent-ok", now)

	hub.Add("agent-stale", &SafeConn{})
	hub.UpdateLastSeen("agent-stale", now.Add(-3*time.Minute))

	hub.Add("agent-borderline", &SafeConn{})
	hub.UpdateLastSeen("agent-borderline", now.Add(-59*time.Second))

	hub.Add("agent-already-offline", &SafeConn{})
	hub.UpdateLastSeen("agent-already-offline", now.Add(-3*time.Minute))
	hub.MarkOffline("agent-already-offline")

	offlineEvents := subscribeOfflineEvents(bus)
	monitor := NewMonitorWithConfig(hub, bus, 10*time.Millisecond, time.Minute, func() time.Time {
		return now
	})

	monitor.scan()

	events := offlineEvents.snapshot()
	assert.Contains(t, events, "agent-stale")
	assert.NotContains(t, events, "agent-ok")
	assert.NotContains(t, events, "agent-borderline")
	assert.NotContains(t, events, "agent-already-offline")
	assert.True(t, hub.IsOnline("agent-ok"))
	assert.False(t, hub.IsOnline("agent-stale"))
	assert.True(t, hub.IsOnline("agent-borderline"))
	assert.False(t, hub.IsOnline("agent-already-offline"))
}

func TestMonitor_DoesNotPublishDuplicateOfflineEvents(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-stale", &SafeConn{})
	hub.UpdateLastSeen("agent-stale", now.Add(-2*time.Minute))

	offlineEvents := subscribeOfflineEvents(bus)
	monitor := NewMonitorWithConfig(hub, bus, 10*time.Millisecond, time.Minute, func() time.Time {
		return now
	})

	monitor.scan()
	monitor.scan()

	assert.Equal(t, []string{"agent-stale"}, offlineEvents.snapshot())
}

func TestMonitor_RunScansUntilContextCancelled(t *testing.T) {
	hub := NewHub()
	bus := events.NewBus()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	hub.Add("agent-1", &SafeConn{})
	hub.UpdateLastSeen("agent-1", now.Add(-2*time.Minute))

	offlineEvents := subscribeOfflineEvents(bus)
	monitor := NewMonitorWithConfig(hub, bus, 10*time.Millisecond, time.Minute, func() time.Time {
		return now
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go monitor.Run(ctx)

	require.Eventually(t, func() bool {
		return len(offlineEvents.snapshot()) == 1
	}, time.Second, 10*time.Millisecond)
}

type offlineEventRecorder struct {
	mu     sync.Mutex
	agents []string
}

func subscribeOfflineEvents(bus *events.Bus) *offlineEventRecorder {
	recorder := &offlineEventRecorder{}
	bus.Subscribe(events.AgentOffline, func(event events.Event) {
		agentID, ok := event.Payload.(string)
		if !ok {
			return
		}

		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		recorder.agents = append(recorder.agents, agentID)
	})
	return recorder
}

func (r *offlineEventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.agents...)
}
