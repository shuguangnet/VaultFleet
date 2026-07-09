## 1. Event Model and Classification

- [x] 1.1 Add notification event constants for backup success, verification success, and verification failure while preserving existing event names.
- [x] 1.2 Classify `task_result` payloads into backup success, backup failure, verification success, verification failure, or ignored non-terminal/non-backup outcomes.
- [x] 1.3 Treat timeout and cancelled task statuses as failure-level notification outcomes.
- [x] 1.4 Preserve compatibility so verification failures still notify configs subscribed to `backup_failed`.

## 2. Notification Message Rendering

- [x] 2.1 Render successful backup notification titles, levels, timestamps, and body text from task result fields.
- [x] 2.2 Render successful and failed verification notification titles, levels, timestamps, and body text.
- [x] 2.3 Include available duration, snapshot ID, artifact name/path/size, repository size, and error text in task-result notification bodies.
- [x] 2.4 Avoid duplicate delivery to the same notification config when compatibility aliases match the same task result.

## 3. API and Frontend Configuration

- [x] 3.1 Extend notification API event validation to accept the new event names and continue rejecting unknown event names.
- [x] 3.2 Update Web UI notification event options to show backup success, backup failure, verification success, verification failure, and Agent offline.
- [x] 3.3 Keep default new notification subscriptions conservative so success notifications are opt-in unless explicitly selected.
- [x] 3.4 Update frontend notification event types or helpers if needed.

## 4. Tests and Verification

- [x] 4.1 Add dispatcher tests for backup success, backup failure, verification success, verification failure, compatibility routing, and ignored restore/non-terminal outcomes.
- [x] 4.2 Add API tests for creating or updating configs with new event names and rejecting unsupported events.
- [x] 4.3 Add or update frontend tests covering the new event options.
- [x] 4.4 Run targeted notification backend tests.
- [x] 4.5 Run targeted frontend tests/build for notification changes.
- [x] 4.6 Run `openspec validate --changes improve-backup-notification-events` and resolve any spec/task issues.
