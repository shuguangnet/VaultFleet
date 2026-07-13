package tasklogs

import (
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vaultfleet/internal/master/db"
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
	database *db.Database
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

func NewPersistentBuffer(database *db.Database) *Buffer {
	return NewPersistentBufferWithLimits(database, DefaultMaxLines, DefaultMaxBytes, DefaultTTL)
}

func NewPersistentBufferWithLimits(database *db.Database, maxLines int, maxBytes int, ttl time.Duration) *Buffer {
	buffer := NewBufferWithLimits(maxLines, maxBytes, ttl)
	buffer.database = database
	buffer.pruneExpiredPersistentLogs()
	return buffer
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
	if b.entries == nil {
		b.entries = make(map[key]*entry)
	}
	k := key{agentID: agentID, messageID: messageID}
	item := b.entries[k]
	if item == nil {
		item = b.loadPersistentEntry(agentID, messageID)
		if item == nil {
			item = &entry{}
		}
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
	oldestSequence := int64(0)
	if len(item.lines) > 0 {
		oldestSequence = item.lines[0].Sequence
	}
	latestSequence := item.latestSequence
	droppedLines := item.droppedLines
	b.mu.Unlock()

	b.persistLine(payload, latestSequence, droppedLines, oldestSequence, now)
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
	b.markPersistentComplete(agentID, messageID, now)
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
		item = b.loadPersistentEntry(agentID, messageID)
		if item != nil {
			b.entries[key{agentID: agentID, messageID: messageID}] = item
		}
	}
	if item == nil {
		return Snapshot{}
	}
	if b.isExpired(item, now) {
		delete(b.entries, key{agentID: agentID, messageID: messageID})
		b.deletePersistentLogs(agentID, messageID)
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
	if b.database != nil && b.database.DB != nil {
		if err := b.database.DB.Where("agent_id = ?", agentID).Delete(&db.TaskLog{}).Error; err != nil {
			log.Printf("delete persistent task logs for agent %s failed: %v", agentID, err)
		}
	}
}

func (b *Buffer) persistLine(payload protocol.TaskLogPayload, latestSequence int64, droppedLines int64, oldestSequence int64, now time.Time) {
	if b == nil || b.database == nil || b.database.DB == nil {
		return
	}
	record := db.TaskLog{
		AgentID:        payload.AgentID,
		MessageID:      payload.MessageID,
		Sequence:       payload.Sequence,
		TaskType:       payload.TaskType,
		Timestamp:      payload.Timestamp,
		Level:          payload.Level,
		Phase:          payload.Phase,
		Stream:         payload.Stream,
		Line:           payload.Line,
		Truncated:      payload.Truncated,
		LatestSequence: latestSequence,
		DroppedLines:   droppedLines,
		LogUpdatedAt:   now,
		CreatedAt:      now,
	}
	err := b.database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "agent_id"}, {Name: "message_id"}, {Name: "sequence"}},
			DoUpdates: clause.Assignments(map[string]any{
				"task_type":       record.TaskType,
				"timestamp":       record.Timestamp,
				"level":           record.Level,
				"phase":           record.Phase,
				"stream":          record.Stream,
				"line":            record.Line,
				"truncated":       record.Truncated,
				"latest_sequence": record.LatestSequence,
				"dropped_lines":   record.DroppedLines,
				"log_updated_at":  record.LogUpdatedAt,
				"completed_at":    nil,
			}),
		}).Create(&record).Error; err != nil {
			return err
		}
		if oldestSequence > 0 {
			return tx.Where("agent_id = ? AND message_id = ? AND sequence < ?", payload.AgentID, payload.MessageID, oldestSequence).Delete(&db.TaskLog{}).Error
		}
		return nil
	})
	if err != nil {
		log.Printf("persist task log %s/%s/%d failed: %v", payload.AgentID, payload.MessageID, payload.Sequence, err)
	}
}

func (b *Buffer) loadPersistentEntry(agentID string, messageID string) *entry {
	if b == nil || b.database == nil || b.database.DB == nil {
		return nil
	}
	var records []db.TaskLog
	if err := b.database.DB.Where("agent_id = ? AND message_id = ?", agentID, messageID).Order("sequence ASC").Find(&records).Error; err != nil {
		log.Printf("load persistent task logs %s/%s failed: %v", agentID, messageID, err)
		return nil
	}
	if len(records) == 0 {
		return nil
	}
	item := &entry{lines: make([]protocol.TaskLogPayload, 0, len(records))}
	for _, record := range records {
		line := protocol.TaskLogPayload{
			AgentID:   record.AgentID,
			MessageID: record.MessageID,
			TaskType:  record.TaskType,
			Sequence:  record.Sequence,
			Timestamp: record.Timestamp,
			Level:     record.Level,
			Phase:     record.Phase,
			Stream:    record.Stream,
			Line:      record.Line,
			Truncated: record.Truncated,
		}
		item.lines = append(item.lines, line)
		item.bytes += len(line.Line)
	}
	latest := records[len(records)-1]
	item.latestSequence = latest.LatestSequence
	item.droppedLines = latest.DroppedLines
	item.updatedAt = latest.LogUpdatedAt
	item.completedAt = latest.CompletedAt
	return item
}

func (b *Buffer) markPersistentComplete(agentID string, messageID string, now time.Time) {
	if b == nil || b.database == nil || b.database.DB == nil {
		return
	}
	if err := b.database.DB.Model(&db.TaskLog{}).
		Where("agent_id = ? AND message_id = ?", agentID, messageID).
		Updates(map[string]any{"completed_at": now, "log_updated_at": now}).Error; err != nil {
		log.Printf("mark persistent task logs complete %s/%s failed: %v", agentID, messageID, err)
	}
}

func (b *Buffer) deletePersistentLogs(agentID string, messageID string) {
	if b == nil || b.database == nil || b.database.DB == nil {
		return
	}
	if err := b.database.DB.Where("agent_id = ? AND message_id = ?", agentID, messageID).Delete(&db.TaskLog{}).Error; err != nil {
		log.Printf("delete persistent task logs %s/%s failed: %v", agentID, messageID, err)
	}
}

func (b *Buffer) pruneExpiredPersistentLogs() {
	if b == nil || b.database == nil || b.database.DB == nil || b.ttl <= 0 {
		return
	}
	cutoff := b.currentTime().Add(-b.ttl)
	if err := b.database.DB.Where("completed_at < ? OR (completed_at IS NULL AND log_updated_at < ?)", cutoff, cutoff).Delete(&db.TaskLog{}).Error; err != nil {
		log.Printf("prune expired persistent task logs failed: %v", err)
	}
}

func (b *Buffer) currentTime() time.Time {
	if b.now != nil {
		return b.now().UTC()
	}
	return time.Now().UTC()
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
