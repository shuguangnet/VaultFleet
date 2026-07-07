package agent

import (
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vaultfleet/pkg/protocol"
	"vaultfleet/pkg/redact"
)

const (
	taskLogLevelInfo  = "info"
	taskLogLevelError = "error"

	taskLogStreamSystem = "system"
	taskLogStreamStdout = "stdout"
	taskLogStreamStderr = "stderr"

	maxTaskLogLineBytes = 4096
)

type taskLogEmitter struct {
	mu        sync.Mutex
	agentID   string
	messageID string
	taskType  string
	send      SendFunc
	sequence  int64
}

func newTaskLogEmitter(agentID string, messageID string, taskType string, send SendFunc) *taskLogEmitter {
	return &taskLogEmitter{
		agentID:   agentID,
		messageID: messageID,
		taskType:  taskType,
		send:      send,
	}
}

func (e *taskLogEmitter) Info(phase string, line string) {
	e.Emit(taskLogLevelInfo, phase, taskLogStreamSystem, line)
}

func (e *taskLogEmitter) Error(phase string, line string) {
	e.Emit(taskLogLevelError, phase, taskLogStreamSystem, line)
}

func (e *taskLogEmitter) Stdout(phase string, line string) {
	e.Emit(taskLogLevelInfo, phase, taskLogStreamStdout, line)
}

func (e *taskLogEmitter) Stderr(phase string, line string) {
	e.Emit(taskLogLevelError, phase, taskLogStreamStderr, line)
}

func (e *taskLogEmitter) Emit(level string, phase string, stream string, text string) {
	if e == nil || e.send == nil || e.agentID == "" || e.messageID == "" {
		return
	}
	for _, line := range splitTaskLogLines(text) {
		e.emitLine(level, phase, stream, line)
	}
}

func (e *taskLogEmitter) emitLine(level string, phase string, stream string, line string) {
	line, truncated := sanitizeTaskLogLine(line)
	if line == "" {
		return
	}
	level = strings.TrimSpace(level)
	if level == "" {
		level = taskLogLevelInfo
	}
	phase = strings.TrimSpace(phase)
	stream = strings.TrimSpace(stream)
	if stream == "" {
		stream = taskLogStreamSystem
	}

	e.mu.Lock()
	e.sequence++
	sequence := e.sequence
	e.mu.Unlock()

	payload := protocol.TaskLogPayload{
		AgentID:   e.agentID,
		MessageID: e.messageID,
		TaskType:  e.taskType,
		Sequence:  sequence,
		Timestamp: time.Now().UTC(),
		Level:     level,
		Phase:     phase,
		Stream:    stream,
		Line:      line,
		Truncated: truncated,
	}
	msg, err := protocol.NewMessage(protocol.TypeTaskLog, payload)
	if err != nil {
		log.Printf("create task log message failed: %v", err)
		return
	}
	msg.ID = e.messageID
	if err := e.send(*msg); err != nil {
		log.Printf("send task log failed: %v", err)
	}
}

func splitTaskLogLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func sanitizeTaskLogLine(line string) (string, bool) {
	line = strings.TrimSpace(redact.Text(line))
	if line == "" {
		return "", false
	}
	if len(line) <= maxTaskLogLineBytes {
		return line, false
	}
	truncated := line[:maxTaskLogLineBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}
