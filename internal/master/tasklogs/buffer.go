package tasklogs

import (
	"sync"
	"time"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 512 * 1024
	DefaultTTL      = 24 * time.Hour
)

type Getter interface {
	Get(agentID string, messageID string, after int64, limit int) Snapshot
}

type Snapshot struct {
	Lines          []protocol.TaskLogPayload
	LatestSequence int64
	Truncated      bool
	DroppedLines   int64
	Exists         bool
	Expired        bool
}

type Buffer struct {
	mu       sync.RWMutex
	entries  map[key]*entry
	maxLines int
	maxBytes int
	ttl      time.Duration
	now      func() time.Time
}

type key struct {
	agentID   string
	messageID string
}

type entry struct {
	lines          []protocol.TaskLogPayload
	bytes          int
	latestSequence int64
	droppedLines   int64
	updatedAt      time.Time
	completedAt    *time.Time
}

func NewBuffer() *Buffer {
	return NewBufferWithLimits(DefaultMaxLines, DefaultMaxBytes, DefaultTTL)
}

func NewBufferWithLimits(maxLines int, maxBytes int, ttl time.Duration) *Buffer {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Buffer{
		entries:  make(map[key]*entry),
		maxLines: maxLines,
		maxBytes: maxBytes,
		ttl:      ttl,
		now:      time.Now,
	}
}

func (b *Buffer) Add(agentID string, messageID string, payload protocol.TaskLogPayload) {
	if b == nil || agentID == "" || messageID == "" {
		return
	}
	now := b.currentTime()
	payload.AgentID = agentID
	payload.MessageID = messageID
	payload.Line = redact.Text(payload.Line)
	if payload.Timestamp.IsZero() {
		payload.Timestamp = now.UTC()
	}
	if payload.Line == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries == nil {
		b.entries = make(map[key]*entry)
	}
	k := key{agentID: agentID, messageID: messageID}
	item := b.entries[k]
	if item == nil {
		item = &entry{}
		b.entries[k] = item
	}
	item.lines = append(item.lines, payload)
	item.bytes += len(payload.Line)
	if payload.Sequence > item.latestSequence {
		item.latestSequence = payload.Sequence
	}
	item.updatedAt = now
	item.completedAt = nil
	item.enforceLimits(b.maxLines, b.maxBytes)
}

func (b *Buffer) MarkComplete(agentID string, messageID string) {
	if b == nil || agentID == "" || messageID == "" {
		return
	}
	now := b.currentTime()
	b.mu.Lock()
	if item := b.entryLocked(agentID, messageID); item != nil {
		item.completedAt = &now
		item.updatedAt = now
	}
	b.mu.Unlock()
}

func (b *Buffer) Get(agentID string, messageID string, after int64, limit int) Snapshot {
	if b == nil || agentID == "" || messageID == "" {
		return Snapshot{}
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	now := b.currentTime()
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.entryLocked(agentID, messageID)
	if item == nil {
		return Snapshot{}
	}
	if b.isExpired(item, now) {
		delete(b.entries, key{agentID: agentID, messageID: messageID})
		return Snapshot{Exists: true, Expired: true, LatestSequence: item.latestSequence, Truncated: item.droppedLines > 0, DroppedLines: item.droppedLines}
	}

	lines := make([]protocol.TaskLogPayload, 0, limit)
	for _, line := range item.lines {
		if line.Sequence <= after {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= limit {
			break
		}
	}
	return Snapshot{
		Lines:          lines,
		LatestSequence: item.latestSequence,
		Truncated:      item.droppedLines > 0,
		DroppedLines:   item.droppedLines,
		Exists:         true,
	}
}

func (b *Buffer) DeleteAgent(agentID string) {
	if b == nil || agentID == "" {
		return
	}
	b.mu.Lock()
	for k := range b.entries {
		if k.agentID == agentID {
			delete(b.entries, k)
		}
	}
	b.mu.Unlock()
}

func (b *Buffer) currentTime() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *Buffer) entryLocked(agentID string, messageID string) *entry {
	if b.entries == nil {
		return nil
	}
	return b.entries[key{agentID: agentID, messageID: messageID}]
}

func (b *Buffer) isExpired(item *entry, now time.Time) bool {
	if item == nil || b.ttl <= 0 {
		return false
	}
	if item.completedAt != nil {
		return now.Sub(*item.completedAt) > b.ttl
	}
	if !item.updatedAt.IsZero() {
		return now.Sub(item.updatedAt) > b.ttl
	}
	return false
}

func (e *entry) enforceLimits(maxLines int, maxBytes int) {
	for len(e.lines) > maxLines || e.bytes > maxBytes {
		if len(e.lines) == 0 {
			e.bytes = 0
			return
		}
		e.bytes -= len(e.lines[0].Line)
		e.lines = e.lines[1:]
		e.droppedLines++
	}
}
