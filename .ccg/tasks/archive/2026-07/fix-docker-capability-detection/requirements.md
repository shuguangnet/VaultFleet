# Requirements

User reports the policy UI shows Docker container backup/detection as unavailable:

- "Docker 容器"
- "已选择 0 个容器"
- "当前 Agent 未上报 Docker 备份能力"

The fix must make current Agents report the Docker/archive backup capability expected by the UI so Docker workload controls are enabled for capable Agents. Existing enrollment and heartbeat paths should remain compatible with the Master capability parsing.

Current evidence:

- `pkg/protocol/message.go` defines `CapabilityArchiveBackup = "archive_backup"`.
- `cmd/agent/main.go` heartbeat capabilities omit `CapabilityArchiveBackup`.
- `internal/agent/enroll/enroll.go` initial enrollment system info capabilities omit `CapabilityArchiveBackup`.
- Current source branch does not contain the exact Docker container-selection UI text, but the reported message is consistent with the Master seeing an Agent system info capability set that lacks `archive_backup`.
