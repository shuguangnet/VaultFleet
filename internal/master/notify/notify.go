package notify

import (
	"context"
	"time"
)

type Level string

const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

type NotifyMessage struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Level     Level     `json:"level"`
	AgentName string    `json:"agent_name"`
	Timestamp time.Time `json:"timestamp"`
}

type Notifier interface {
	Send(ctx context.Context, msg NotifyMessage) error
	Type() string
}
