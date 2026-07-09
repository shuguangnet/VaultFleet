## Context

VaultFleet already has a notification dispatcher with Telegram, Webhook, and Email notifiers. Notification configs store an event list, and the dispatcher subscribes to the Master event bus. The event bus already receives `task_result` events from Agent task completion, including successful and failed backup or verification results.

The current dispatcher only converts failed `backup` or `verify` task results into `backup_failed`; successful task results are intentionally ignored. The notification API and Web UI only expose `backup_failed` and `agent_offline` as allowed subscription events.

## Goals / Non-Goals

**Goals:**

- Let operators opt in to successful backup notifications.
- Let operators subscribe to backup verification success and failure independently from normal backup events.
- Preserve existing notification configs and existing `backup_failed` behavior.
- Render task-result notifications with enough context to be useful in Telegram, Webhook, and Email templates.
- Keep the change scoped to Master/API/UI because Agents already report task results.

**Non-Goals:**

- Daily digest or notification aggregation.
- Per-policy, per-tag, or per-node notification filters.
- Notification delivery history.
- Restore success/failure notifications.
- Agent protocol changes.

## Decisions

### 1. Add event names without changing notification config storage

Notification configs already store `events` as a JSON array of strings. Add new event constants:

- `backup_succeeded`
- `backup_verification_succeeded`
- `backup_verification_failed`

Keep `backup_failed` and `agent_offline`.

Rationale: this preserves the existing API shape and database schema. Existing configs continue to load and match exactly as before.

Alternative considered: introduce a structured rule model with task type/status filters. Rejected for P0 because the immediate need is opt-in success notifications, and richer filters can layer on later.

### 2. Classify task-result notifications by task type and terminal status

Use `protocol.TaskResultPayload.TaskType` and `Status`:

- `backup` + success status -> `backup_succeeded`
- `backup` + failure/timeout/cancelled status -> `backup_failed`
- `verify` + success status -> `backup_verification_succeeded`
- `verify` + failure/timeout/cancelled status -> `backup_verification_failed`

Rationale: the task result is the durable source of truth for backup completion. Treating timeout/cancelled as failure-level notification outcomes avoids silent operational gaps.

### 3. Maintain compatibility for verification failures

For compatibility, an existing config subscribed only to `backup_failed` should continue receiving failed backup verification notifications during the first version of this change. New configs can subscribe to `backup_verification_failed` directly.

Rationale: verification failures were previously routed through `backup_failed`. Removing that would be a silent breaking change.

### 4. Enrich notification body text using existing payload fields

Notification messages should include available task context without adding new template fields:

- Status
- Snapshot ID
- Artifact name/path/size
- Duration
- Repository size
- Error log for failures

Rationale: all notifier types consume `NotifyMessage` with `Title`, `Body`, `Level`, `AgentName`, and `Timestamp`. Rich body text improves usefulness without changing notifier interfaces or templates.

## Risks / Trade-offs

- [Risk] Operators may enable success notifications on frequent scheduled backups and receive too many messages. -> Mitigation: success events are opt-in and defaults can remain conservative.
- [Risk] Verification failure compatibility can send duplicate notifications if a channel subscribes to both `backup_failed` and `backup_verification_failed`. -> Mitigation: dispatch should de-duplicate matching configs per task result.
- [Risk] Task status vocabulary can vary. -> Mitigation: classify common success and failure statuses case-insensitively and ignore non-terminal/unknown statuses.
- [Risk] UI labels may imply all task types are covered. -> Mitigation: labels should explicitly say backup and verification, not generic task success.

## Migration Plan

1. Deploy the Master change. No database migration is required.
2. Existing configs continue to work.
3. Operators can edit notification configs and add the new success/verification events.
4. Rollback is safe because configs containing new event strings will be ignored by older dispatchers; older API validation may reject editing those configs until upgraded again.

## Open Questions

- Should future work add daily success digest notifications for high-frequency backup schedules?
- Should restore success/failure events be added as a separate notification-events extension?
- Should notification configs eventually support policy/tag filters to reduce success notification noise?
