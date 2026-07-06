package api

import (
	"encoding/json"
	"strconv"
	"strings"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func agentHasCapability(database *db.Database, agentID string, capability string) (bool, error) {
	var agent db.Agent
	if err := database.DB.Select("system_info").First(&agent, "id = ?", agentID).Error; err != nil {
		return false, err
	}
	return systemInfoHasCapability(agent.SystemInfo, capability), nil
}

func agentSupportsPlaintextRclonePass(database *db.Database, agentID string) (bool, error) {
	return agentHasCapability(database, agentID, protocol.CapabilityPolicyPlaintextRclonePass)
}

func systemInfoHasCapability(raw string, capability string) bool {
	for _, supported := range agentCapabilitiesFromSystemInfo(raw) {
		if supported == capability {
			return true
		}
	}
	return false
}

func agentCapabilitiesFromSystemInfo(raw string) []string {
	info := parseAgentSystemInfoMap(raw)
	capabilities, ok := info["capabilities"].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(capabilities)+1)
	for _, value := range capabilities {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	version, _ := info["version"].(string)
	return addCompatibleAgentCapabilities(result, version)
}

func addCompatibleAgentCapabilities(capabilities []string, version string) []string {
	result := append([]string(nil), capabilities...)
	if containsString(result, protocol.CapabilityDockerContainerRestore) {
		return result
	}
	if containsString(result, protocol.CapabilityDockerWorkloadBackups) && agentVersionAtLeast(version, "v0.5.21") {
		result = append(result, protocol.CapabilityDockerContainerRestore)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func agentVersionAtLeast(version string, minimum string) bool {
	current, ok := parseAgentVersion(version)
	if !ok {
		return false
	}
	required, ok := parseAgentVersion(minimum)
	if !ok {
		return false
	}
	for i := range required {
		if current[i] > required[i] {
			return true
		}
		if current[i] < required[i] {
			return false
		}
	}
	return true
}

func parseAgentVersion(version string) ([3]int, bool) {
	var result [3]int
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	parts := strings.Split(version, ".")
	if len(parts) < len(result) {
		return result, false
	}
	for i := range result {
		value, err := strconv.Atoi(parts[i])
		if err != nil {
			return result, false
		}
		result[i] = value
	}
	return result, true
}

func mergeHeartbeatIntoSystemInfo(raw string, heartbeat *protocol.HeartbeatPayload) string {
	info := parseAgentSystemInfoMap(raw)
	if heartbeat != nil {
		if heartbeat.AgentVersion != "" {
			info["version"] = heartbeat.AgentVersion
		}
		if len(heartbeat.Capabilities) > 0 {
			info["capabilities"] = heartbeat.Capabilities
		}
	}
	data, err := json.Marshal(info)
	if err != nil {
		return raw
	}
	return string(data)
}

func parseAgentSystemInfoMap(raw string) map[string]any {
	info := make(map[string]any)
	if raw == "" {
		return info
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return make(map[string]any)
	}
	return info
}
