## Why

VaultFleet currently sends notifications for backup failures and Agent offline events, but successful backups and successful recoverability verification cannot be subscribed to. Operators who need positive confirmation, external automation, or audit-friendly completion signals cannot distinguish "backup ran successfully" from "no message was sent."

## What Changes

- Add first-class notification events for successful backups.
- Add first-class notification events for successful and failed backup recoverability verification.
- Keep existing `backup_failed` and `agent_offline` behavior compatible for existing notification configs.
- Enrich task-result notifications with useful backup context such as status, snapshot ID, artifact information, duration, repository size, and error text when present.
- Update notification API validation and Web UI event options so operators can choose success and failure events per channel.
- Add tests proving success events are opt-in and do not spam channels subscribed only to failure events.

## Capabilities

### New Capabilities

- `notification-events`: Notification event taxonomy, subscription validation, and task-result notification behavior for backups, verification, and Agent offline events.

### Modified Capabilities

- None.

## Impact

- Master notification dispatcher: event constants, task-result classification, and notification message rendering.
- Master notification API: allowed event validation and backward-compatible persistence of event arrays.
- Web UI notification settings: event selector options and default subscription choices.
- Tests: dispatcher, API validation, and notification UI/service coverage.
- No Agent protocol changes are expected because task success/failure results already reach Master through `task_result`.
