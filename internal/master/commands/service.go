package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const (
	CommandStatusPending    = "pending"
	CommandStatusDispatched = "dispatched"
	CommandStatusRunning    = "running"
	CommandStatusSucceeded  = "succeeded"
	CommandStatusFailed     = "failed"
	CommandStatusTimeout    = "timeout"

	TaskStatusPending = "pending"
	TaskStatusRunning = "running"
	TaskStatusSuccess = "success"
	TaskStatusFailed  = "failed"
	TaskStatusTimeout = "timeout"
)

type Hub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type Service struct {
	DB         *db.Database
	Hub        Hub
	Now        func() time.Time
	dispatchMu sync.Mutex
}

type CreateCommandInput struct {
	AgentID    string
	Type       string
	Message    protocol.Message
	TaskType   string
	TaskState  string
	SnapshotID string
	PolicyID   string
	StorageID  string
}

func NewService(database *db.Database, hub Hub) *Service {
	return &Service{DB: database, Hub: hub, Now: time.Now}
}

func DeadlineForType(commandType string, now time.Time) time.Time {
	switch commandType {
	case protocol.TypePolicyPush:
		return now.Add(5 * time.Minute)
	case protocol.TypeSnapshotListReq:
		return now.Add(2 * time.Minute)
	case protocol.TypeBackupNow, protocol.TypeRestoreReq:
		return now.Add(6 * time.Hour)
	default:
		return now.Add(30 * time.Minute)
	}
}

func (s *Service) CreateCommand(ctx context.Context, input CreateCommandInput) (db.AgentCommand, error) {
	if s == nil || s.DB == nil || s.DB.DB == nil {
		return db.AgentCommand{}, errors.New("command service database not configured")
	}
	if input.AgentID == "" || input.Type == "" || input.Message.ID == "" {
		return db.AgentCommand{}, errors.New("agent id, command type, and message id are required")
	}

	raw, err := json.Marshal(input.Message)
	if err != nil {
		return db.AgentCommand{}, fmt.Errorf("marshal command payload: %w", err)
	}
	encrypted, err := db.Encrypt(string(raw), s.DB.MasterKey)
	if err != nil {
		return db.AgentCommand{}, fmt.Errorf("encrypt command payload: %w", err)
	}

	deadline := DeadlineForType(input.Type, s.now())
	command := db.AgentCommand{
		AgentID:    input.AgentID,
		Type:       input.Type,
		Status:     CommandStatusPending,
		MessageID:  input.Message.ID,
		Payload:    encrypted,
		PolicyID:   input.PolicyID,
		StorageID:  input.StorageID,
		DeadlineAt: &deadline,
	}

	err = s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&command).Error; err != nil {
			return err
		}
		if input.TaskType == "" {
			return nil
		}

		state := input.TaskState
		if state == "" {
			state = TaskStatusPending
		}
		history := db.TaskHistory{
			AgentID:    input.AgentID,
			Type:       input.TaskType,
			Status:     state,
			SnapshotID: input.SnapshotID,
			MessageID:  input.Message.ID,
			CommandID:  command.ID,
			PolicyID:   input.PolicyID,
			StorageID:  input.StorageID,
		}
		return tx.Create(&history).Error
	})
	if err != nil {
		return db.AgentCommand{}, err
	}

	return command, nil
}

func (s *Service) DispatchPendingForAgent(ctx context.Context, agentID string, limit int) error {
	if s == nil || s.DB == nil || s.DB.DB == nil || s.Hub == nil || agentID == "" {
		return nil
	}
	if !s.Hub.IsOnline(agentID) {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}

	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	now := s.now()
	var commands []db.AgentCommand
	err := s.DB.DB.WithContext(ctx).
		Where("agent_id = ? AND status = ? AND (deadline_at IS NULL OR deadline_at > ?)",
			agentID,
			CommandStatusPending,
			now,
		).
		Order("created_at ASC").
		Limit(limit).
		Find(&commands).Error
	if err != nil {
		return err
	}

	for _, command := range commands {
		if err := s.dispatch(ctx, command); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) messageFromCommand(command db.AgentCommand) (protocol.Message, error) {
	if s == nil || s.DB == nil {
		return protocol.Message{}, errors.New("command service database not configured")
	}
	plaintext, err := db.Decrypt(command.Payload, s.DB.MasterKey)
	if err != nil {
		return protocol.Message{}, fmt.Errorf("decrypt command payload: %w", err)
	}
	var message protocol.Message
	if err := json.Unmarshal([]byte(plaintext), &message); err != nil {
		return protocol.Message{}, fmt.Errorf("unmarshal command payload: %w", err)
	}
	return message, nil
}

func (s *Service) dispatch(ctx context.Context, command db.AgentCommand) error {
	message, err := s.messageFromCommand(command)
	if err != nil {
		return err
	}
	if err := s.Hub.Send(command.AgentID, message); err != nil {
		return s.recordDispatchFailure(ctx, command, err)
	}

	now := s.now()
	status := CommandStatusDispatched
	if isLongRunning(command.Type) {
		status = CommandStatusRunning
	}

	return s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.AgentCommand{}).
			Where("id = ?", command.ID).
			Updates(map[string]any{
				"attempts":      gorm.Expr("attempts + ?", 1),
				"dispatched_at": &now,
				"error_message": "",
				"status":        status,
			}).Error; err != nil {
			return err
		}
		if !isLongRunning(command.Type) {
			return nil
		}
		return tx.Model(&db.TaskHistory{}).
			Where("command_id = ? AND status = ?", command.ID, TaskStatusPending).
			Update("status", TaskStatusRunning).Error
	})
}

func (s *Service) recordDispatchFailure(ctx context.Context, command db.AgentCommand, dispatchErr error) error {
	if s == nil || s.DB == nil || s.DB.DB == nil {
		return nil
	}
	return s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.AgentCommand{}).
			Where("id = ?", command.ID).
			Updates(map[string]any{
				"attempts":      gorm.Expr("attempts + ?", 1),
				"dispatched_at": nil,
				"error_message": dispatchErr.Error(),
				"status":        CommandStatusPending,
			}).Error; err != nil {
			return err
		}
		if !isLongRunning(command.Type) {
			return nil
		}
		return tx.Model(&db.TaskHistory{}).
			Where("command_id = ? AND status = ?", command.ID, TaskStatusRunning).
			Update("status", TaskStatusPending).Error
	})
}

func (s *Service) now() time.Time {
	if s == nil || s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func isLongRunning(commandType string) bool {
	return commandType == protocol.TypeBackupNow || commandType == protocol.TypeRestoreReq
}
