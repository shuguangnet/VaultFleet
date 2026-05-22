package api

import (
	"time"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

type AgentStateUpdater func(agentID string, status string, lastSeenAt *time.Time) error

func NewAgentStateUpdater(database *db.Database) AgentStateUpdater {
	return func(agentID string, status string, lastSeenAt *time.Time) error {
		if database == nil || database.DB == nil || agentID == "" || status == "" {
			return nil
		}
		updates := map[string]any{"status": status}
		if lastSeenAt != nil {
			updates["last_seen_at"] = *lastSeenAt
		}
		return database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Updates(updates).Error
	}
}

type HeartbeatStateUpdater func(agentID string, status string, lastSeenAt *time.Time, heartbeat *protocol.HeartbeatPayload) error

func NewHeartbeatStateUpdater(database *db.Database) HeartbeatStateUpdater {
	return func(agentID string, status string, lastSeenAt *time.Time, heartbeat *protocol.HeartbeatPayload) error {
		if database == nil || database.DB == nil || agentID == "" || status == "" {
			return nil
		}
		updates := map[string]any{"status": status}
		if lastSeenAt != nil {
			updates["last_seen_at"] = *lastSeenAt
		}
		if heartbeat != nil && (heartbeat.AgentVersion != "" || len(heartbeat.Capabilities) > 0) {
			var agent db.Agent
			if err := database.DB.Select("system_info").First(&agent, "id = ?", agentID).Error; err == nil {
				updated := mergeHeartbeatIntoSystemInfo(agent.SystemInfo, heartbeat)
				updates["system_info"] = updated
			}
		}
		return database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Updates(updates).Error
	}
}

func SubscribeAgentStateEvents(database *db.Database, bus *events.Bus) {
	if bus == nil {
		return
	}
	updater := NewAgentStateUpdater(database)
	bus.Subscribe(events.AgentOffline, func(event events.Event) {
		agentID := eventAgentID(event.Payload)
		if agentID == "" {
			return
		}
		_ = updater(agentID, "offline", nil)
	})
}

func eventAgentID(payload any) string {
	switch value := payload.(type) {
	case string:
		return value
	case map[string]any:
		if agentID, ok := value["agent_id"].(string); ok {
			return agentID
		}
		if agentID, ok := value["id"].(string); ok {
			return agentID
		}
	}
	return ""
}
