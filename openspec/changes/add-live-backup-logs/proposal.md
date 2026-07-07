## Why

Operators can see backup progress today, but when a backup stalls or fails they only get limited status and final error text. Real-time task logs let operators understand what restic, rclone, hooks, Docker discovery, archive creation, or verification are doing while the backup is still running.

## What Changes

- Add task-scoped live log events emitted by Agents during backup-related work.
- Capture sanitized log lines from backup stages, pre/post hooks, restic/rclone output, archive mode, Docker source resolution, and verification where available.
- Add Master-side buffering and APIs for fetching recent log lines by task or command message ID.
- Add Web UI controls on running and recent tasks to view live logs with auto-follow, pause, copy, and clear-filter behavior.
- Preserve existing progress reporting; live logs complement progress and final task history rather than replacing them.

## Capabilities

### New Capabilities
- `live-backup-logs`: Task-scoped log streaming, buffering, retrieval, and UI viewing for backup execution.

### Modified Capabilities
- None.

## Impact

- Protocol: add a live task log message type and Agent capability flag.
- Agent: add a redacted task log sink and wire it into backup, verification, hooks, Docker source handling, archive creation, restic, and rclone paths where practical.
- Master: add in-memory bounded log buffers keyed by Agent/message/task, attach logs to active task lookup, and expose authenticated API endpoints.
- Frontend: add log viewer UI in task history and backup progress workflows.
- Security: redact secrets before logs leave the Agent and avoid persisting full sensitive command output by default.
- Tests/docs: cover buffering, redaction, permission checks, API retrieval, and UI rendering.
