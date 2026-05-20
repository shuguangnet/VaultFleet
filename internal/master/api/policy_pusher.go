package api

import (
	"context"
	"log"
	"sync"
	"time"

	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

type PolicyPusherHub interface {
	IsOnline(agentID string) bool
	Send(agentID string, msg interface{}) error
}

type PolicyLookupFunc func(agentID string) (*protocol.Message, bool)
type PolicyCommandLookupFunc func(agentID string) (*CurrentPolicyCommand, bool)

type PolicyChangedPusher struct {
	DB            *db.Database
	Hub           PolicyPusherHub
	Lookup        PolicyLookupFunc
	CommandLookup PolicyCommandLookupFunc
	Commands      *commands.Service
	mu            sync.Mutex
}

func NewPolicyChangedPusher(database *db.Database, hub PolicyPusherHub, lookup PolicyLookupFunc) *PolicyChangedPusher {
	return &PolicyChangedPusher{
		DB:            database,
		Hub:           hub,
		Lookup:        lookup,
		CommandLookup: CurrentPolicyCommandLookup(database),
	}
}

func (p *PolicyChangedPusher) Handle(event events.Event) {
	if p == nil || p.Hub == nil {
		return
	}
	if action := eventAction(event.Payload); action == "ack" {
		return
	}
	agentID := eventAgentID(event.Payload)
	if agentID == "" || !p.Hub.IsOnline(agentID) {
		return
	}
	if p.Commands == nil {
		msg, ok := p.lookupDirectMessage(agentID)
		if !ok || msg == nil {
			return
		}
		if err := p.Hub.Send(agentID, *msg); err != nil {
			log.Printf("push policy to agent %s failed: %v", agentID, err)
		}
		return
	}

	if !p.EnsureDurableCommand(context.Background(), agentID) {
		return
	}
	if err := p.Commands.DispatchNewPendingForAgent(context.Background(), agentID, 10); err != nil {
		log.Printf("dispatch policy command for agent %s failed: %v", agentID, err)
	}
}

func (p *PolicyChangedPusher) EnsureDurableCommand(ctx context.Context, agentID string) bool {
	if p == nil || p.Commands == nil || agentID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	current, ok := p.lookupCommand(agentID)
	now := p.now()
	if !ok || current == nil || current.Message == nil {
		p.retireActivePolicyPushCommands(agentID, "no current policy; stale policy push command retired", now)
		return false
	}
	p.retireSupersededPolicyPushCommands(agentID, current.PolicyID, current.StorageID, current.PolicyUpdatedAt, now)
	if p.hasActivePolicyPushCommand(agentID, current.PolicyID, current.StorageID, current.PolicyUpdatedAt, now) {
		return true
	}
	if _, err := p.Commands.CreateCommand(ctx, commands.CreateCommandInput{
		AgentID:         agentID,
		Type:            protocol.TypePolicyPush,
		Message:         *current.Message,
		PolicyID:        current.PolicyID,
		PolicyUpdatedAt: &current.PolicyUpdatedAt,
		StorageID:       current.StorageID,
	}); err != nil {
		log.Printf("create policy command for agent %s failed: %v", agentID, err)
		return false
	}
	return true
}

func (p *PolicyChangedPusher) retireActivePolicyPushCommands(agentID string, reason string, now time.Time) {
	if p == nil || p.DB == nil || p.DB.DB == nil || agentID == "" {
		return
	}
	result := p.DB.DB.Model(&db.AgentCommand{}).
		Where(
			"agent_id = ? AND type = ? AND status IN ?",
			agentID,
			protocol.TypePolicyPush,
			activePolicyPushCommandStatuses(),
		).
		Where("(policy_id <> ? OR storage_id <> ? OR policy_updated_at IS NOT NULL)", "", "").
		Updates(map[string]any{
			"status":        commands.CommandStatusFailed,
			"error_message": reason,
			"completed_at":  now,
		})
	if result.Error != nil {
		log.Printf("retire active policy commands for agent %s failed: %v", agentID, result.Error)
	}
}

func (p *PolicyChangedPusher) retireSupersededPolicyPushCommands(agentID string, policyID string, storageID string, policyUpdatedAt time.Time, now time.Time) {
	if p == nil || p.DB == nil || p.DB.DB == nil || agentID == "" || policyID == "" || storageID == "" || policyUpdatedAt.IsZero() {
		return
	}
	result := p.DB.DB.Model(&db.AgentCommand{}).
		Where(
			"agent_id = ? AND type = ? AND status IN ?",
			agentID,
			protocol.TypePolicyPush,
			activePolicyPushCommandStatuses(),
		).
		Where(
			"(policy_id IS NULL OR policy_id <> ? OR storage_id IS NULL OR storage_id <> ? OR policy_updated_at IS NULL OR policy_updated_at <> ?)",
			policyID,
			storageID,
			policyUpdatedAt,
		).
		Updates(map[string]any{
			"status":        commands.CommandStatusFailed,
			"error_message": "stale policy push command retired; superseded by current policy",
			"completed_at":  now,
		})
	if result.Error != nil {
		log.Printf("retire superseded policy commands for agent %s failed: %v", agentID, result.Error)
	}
}

func activePolicyPushCommandStatuses() []string {
	return []string{commands.CommandStatusPending, commands.CommandStatusDispatched, commands.CommandStatusRunning}
}

func (p *PolicyChangedPusher) lookupCommand(agentID string) (*CurrentPolicyCommand, bool) {
	if p == nil {
		return nil, false
	}
	if p.CommandLookup != nil {
		return p.CommandLookup(agentID)
	}
	if p.Lookup == nil {
		return nil, false
	}
	msg, ok := p.Lookup(agentID)
	if !ok || msg == nil {
		return nil, false
	}
	return &CurrentPolicyCommand{Message: msg, AgentID: agentID}, true
}

func (p *PolicyChangedPusher) lookupMessage(agentID string) (*protocol.Message, bool) {
	if current, ok := p.lookupCommand(agentID); ok && current != nil && current.Message != nil {
		return current.Message, true
	}
	return nil, false
}

func (p *PolicyChangedPusher) lookupDirectMessage(agentID string) (*protocol.Message, bool) {
	if p != nil && p.Lookup != nil {
		return p.Lookup(agentID)
	}
	return p.lookupMessage(agentID)
}

func (p *PolicyChangedPusher) hasActivePolicyPushCommand(agentID string, policyID string, storageID string, policyUpdatedAt time.Time, now time.Time) bool {
	if p == nil || p.DB == nil || p.DB.DB == nil || agentID == "" || policyID == "" || storageID == "" {
		return false
	}
	var count int64
	query := p.DB.DB.Model(&db.AgentCommand{}).
		Where(
			"agent_id = ? AND type = ? AND policy_id = ? AND storage_id = ? AND status IN ?",
			agentID,
			protocol.TypePolicyPush,
			policyID,
			storageID,
			[]string{commands.CommandStatusPending, commands.CommandStatusDispatched, commands.CommandStatusRunning},
		).
		Where("(deadline_at IS NULL OR deadline_at > ?)", now)
	if !policyUpdatedAt.IsZero() {
		query = query.Where("policy_updated_at = ?", policyUpdatedAt)
	}
	if err := query.Count(&count).Error; err != nil {
		log.Printf("check active policy command for agent %s failed: %v", agentID, err)
		return false
	}
	return count > 0
}

func (p *PolicyChangedPusher) now() time.Time {
	if p != nil && p.Commands != nil && p.Commands.Now != nil {
		return p.Commands.Now()
	}
	return time.Now()
}

func eventAction(payload any) string {
	switch value := payload.(type) {
	case map[string]any:
		if action, ok := value["action"].(string); ok {
			return action
		}
	}
	return ""
}
