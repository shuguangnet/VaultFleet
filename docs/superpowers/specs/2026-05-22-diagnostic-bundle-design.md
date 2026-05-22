# Diagnostic Bundle (诊断包) — Design Spec

## Overview

Add a "diagnostic bundle" feature to VaultFleet that lets users generate and download a ZIP file containing system state, Master logs, and Agent logs — all automatically collected and redacted. This replaces the manual process of copying logs from Docker/journalctl and pasting them into GitHub issues.

## Motivation

The current bug report workflow (`/system` → "提交 Issue" → GitHub) requires users to manually run `docker compose logs` or `journalctl`, copy the output, and paste it into the issue template. This is error-prone, time-consuming, and users often skip the logs section entirely. A one-click diagnostic bundle solves this.

## User Flow

1. User navigates to `/system` page
2. A new "诊断包" card shows:
   - A list of **online Agents** with checkboxes (offline agents shown but disabled/grayed out)
   - A "生成诊断包" button
3. User optionally selects which Agents to include, clicks "生成诊断包"
4. UI shows progress (collecting Master data → collecting Agent logs → packaging)
5. Browser downloads `vaultfleet-diagnostic-<timestamp>.zip`
6. User attaches the ZIP to their GitHub issue or sends it via other channels

## Diagnostic Bundle Contents

### ZIP Structure

```
vaultfleet-diagnostic-20260522T143000.zip
├── meta.json                    # generation timestamp, VaultFleet version, OS/arch
├── master/
│   ├── logs.txt                 # Master process logs (last 24h from ring buffer)
│   ├── nodes.json               # All registered nodes with online status
│   ├── storage.json             # Storage backend configs (redacted)
│   ├── policies.json            # Backup policy list
│   └── recent_errors.json       # Last 50 failed task error_logs
└── agents/
    ├── <agent-name-1>/
    │   └── logs.txt             # Agent 1 last 24h logs (max 5MB, redacted)
    └── <agent-name-2>/
        └── logs.txt             # Agent 2 last 24h logs (max 5MB, redacted)
```

### Data Sources

| Item | Source | Already Available? |
|------|--------|--------------------|
| Version, OS/arch | `GET /api/system/version` + runtime | Yes |
| Node list + status | DB `Agent` model | Yes |
| Storage configs | DB `StorageBackend` model | Yes |
| Backup policies | DB `BackupPolicy` model | Yes |
| Recent task errors | DB `TaskHistory` model (status=failed) | Yes |
| Master logs | **New** ring buffer capturing stdout | No — needs implementation |
| Agent logs | **New** WebSocket command `collect_logs` | No — needs implementation |

## Backend Changes

### 1. Master Log Ring Buffer

**File:** new `internal/master/logbuf/logbuf.go`

- Implement an in-memory ring buffer (`[]byte`, ~2MB capacity) that captures all `log` package output
- At Master startup, replace `log.SetOutput` with a `MultiWriter` that writes to both `os.Stdout` and the ring buffer
- Provide `ReadAll() []byte` to dump the buffer contents (oldest to newest)
- Thread-safe via `sync.Mutex`

### 2. New API Endpoint

**File:** `internal/master/api/diagnostic.go` + registration in `router.go`

`GET /api/system/diagnostic?agents=<id1>,<id2>`

- Protected route (requires auth)
- Query parameter `agents` is optional comma-separated list of agent IDs
- Response: `Content-Type: application/zip`, streaming ZIP file
- Steps:
  1. Collect local data (version, nodes, storage, policies, recent errors)
  2. Read Master log buffer
  3. For each requested agent ID: send `collect_logs` command via WebSocket, wait for response (30s timeout)
  4. Apply redaction to all text content
  5. Build ZIP archive and stream to response

### 3. WebSocket Command: `collect_logs`

**Master side** (`internal/master/ws/` or `internal/master/commands/`):
- New command type `collect_logs`
- Send to specified agent, wait for response with 30s timeout
- Response payload: `{ "logs": "<log text>" }`

**Agent side** (`internal/agent/`):
- New handler for `collect_logs` command
- Auto-detect init system:
  - systemd: `journalctl -u vaultfleet-agent --since "24 hours ago" --no-pager`
  - Log file fallback: read `/var/log/vaultfleet-agent.log`, filter to last 24h
- Truncate to 5MB max
- Apply redaction before sending
- Send result back via WebSocket

### 4. Redaction

**Shared utility** (used by both Master and Agent):

Regex patterns to replace sensitive values with `[REDACTED]`:
- `(token|password|passwd|secret|cookie|credential|api_key|access_key|secret_key|private_key|auth)(\s*[=:]\s*)(\S+)` → keep group 1+2, replace group 3
- Storage config JSON: redact `accessKey`, `secretKey`, `endpoint`, `password` fields
- Bearer tokens: `Bearer \S+` → `Bearer [REDACTED]`

## Frontend Changes

### System Page — New "诊断包" Card

**File:** `web/src/pages/system/system-page.tsx`

Location: alongside the existing "问题反馈" card in the `/system` page.

Card contents:
- Title: "诊断包" with subtitle "自动收集系统信息和日志，用于问题排查"
- Online Agent list with checkboxes (offline agents shown with disabled checkbox + "离线" badge)
- "生成诊断包" button
- During generation: progress indicator with status text ("正在收集 Master 数据..."、"正在收集 Agent-X 日志..."、"正在打包...")
- On completion: auto-trigger file download
- On error: toast notification with error message

### New API Service

**File:** `web/src/services/diagnostic.ts`

```typescript
export async function downloadDiagnosticBundle(agentIds: string[]): Promise<Blob> {
  const params = agentIds.length > 0 ? `?agents=${agentIds.join(',')}` : '';
  const response = await fetch(`/api/system/diagnostic${params}`);
  return response.blob();
}
```

## Error Handling & Timeouts

- Agent log collection timeout: **30 seconds** per agent
- If an agent times out: include a `timeout.txt` marker file in its directory in the ZIP
- If an agent fails: include an `error.txt` with the error message
- Offline agents cannot be selected (UI prevents it)
- Master data collection failures: include what's available, note failures in `meta.json`
- Overall request timeout: 60 seconds (accounts for multiple agents)

## Security Considerations

- Diagnostic endpoint requires authentication (same as all protected routes)
- Automatic redaction of sensitive values in all log content
- Storage configs have credentials stripped before inclusion
- Agent logs are redacted on the Agent side before transmission (defense in depth)
- ZIP filename includes timestamp but no sensitive identifiers

## Future Extensions

- Preview diagnostic bundle contents before download
- Direct upload to GitHub issue (requires GitHub OAuth)
- Scheduled/automatic diagnostic collection on repeated failures
- Include Agent-side restic/rclone config files (redacted)
