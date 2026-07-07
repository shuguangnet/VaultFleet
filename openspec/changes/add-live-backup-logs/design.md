## Context

VaultFleet already reports backup progress through `backup_progress` WebSocket messages from Agent to Master and attaches the latest progress to task list responses. It also supports diagnostic log collection, but that collects process-level Agent logs after the fact and is not tied to a specific backup task.

Live backup logs need a task-scoped path: operators should open a running backup and see the current command/stage output without downloading a diagnostic bundle or waiting for failure. The system must handle secrets carefully because backup execution touches restic passwords, rclone config, storage URLs, hook commands, and application output.

## Goals / Non-Goals

**Goals:**
- Stream task-scoped log lines from Agent to Master while backup, verification, archive, Docker source resolution, and hooks run.
- Redact sensitive values before log lines leave the Agent.
- Keep a bounded in-memory log buffer on Master for active and recently completed tasks.
- Expose authenticated APIs to fetch recent lines and optionally poll incrementally by sequence number.
- Add a Web UI log drawer/panel for task history with auto-follow and pause support.
- Preserve existing progress reporting and task result behavior.

**Non-Goals:**
- Do not persist full live logs indefinitely in SQLite by default.
- Do not expose raw restic/rclone config, passwords, token values, or environment dumps.
- Do not build browser WebSocket/SSE streaming in the first version.
- Do not make live logs a replacement for diagnostic bundles or Agent journal collection.

## Decisions

1. Add `task_log` protocol messages from Agent to Master.

   Payload shape:

   ```text
   agent_id
   message_id
   task_type
   sequence
   timestamp
   level
   phase
   stream
   line
   truncated
   ```

   Rationale: Existing task identity already uses command message IDs. A dedicated message type avoids overloading progress payloads with unbounded text and keeps log processing isolated.

   Alternative considered: append logs to `backup_progress`. This would be simpler but progress has very different retention, UI rendering, and payload size expectations.

2. Redact on Agent before sending.

   Rationale: Secrets should not cross the trust boundary from Agent to Master in log lines. Master can perform a second defensive redaction pass, but Agent-side redaction is mandatory.

   Sources to redact include known policy secrets, restic password values, rclone sensitive fields, tokens, Authorization headers, and generic password/secret/key patterns.

3. Use a bounded Master log buffer keyed by `(agent_id, message_id)`.

   Rationale: Operators mostly need the most recent lines of the running task. A bounded ring buffer prevents long backups from exhausting Master memory and avoids database growth from noisy command output.

   Proposed default: keep the last 2,000 lines or 512 KiB per task, whichever limit is hit first. Keep buffers for a short TTL after task completion, such as 24 hours or until Master restart.

   Alternative considered: persist logs in SQLite. This is useful for audit/debug history but increases storage, retention, and redaction obligations. It can be a later option.

4. Expose polling APIs instead of browser streaming first.

   Endpoints:

   ```text
   GET /api/tasks/:id/logs?after=<seq>&limit=<n>
   GET /api/commands/:id/logs?after=<seq>&limit=<n>
   ```

   Rationale: The UI already polls operational data and Master does not currently expose browser WebSocket/SSE for task events. Polling with sequence numbers is simple, robust through reconnects, and enough for live log viewing.

5. Capture structured stage lines even when tool stdout is unavailable.

   Rationale: Some current execution helpers only return final output or progress events. The first implementation should still emit useful stage logs such as "initializing repository", "running backup", "applying retention", "listing snapshots", and hook start/finish. Tool stdout/stderr capture can then be added to restic/rclone command wrappers.

## Risks / Trade-offs

- Sensitive output leakage -> Agent-side redaction plus Master-side defensive redaction; never log full config or environment; tests cover common secret patterns.
- High-volume logs -> line length limit, per-task ring limits, dropped-line counters, and UI indicators for truncation.
- Missing logs for old Agents -> Master returns a clear unsupported/empty state when no log capability exists; progress remains available.
- Polling overhead -> incremental `after` sequence fetch and conservative default polling interval while drawer is open.
- Hook output may contain application data -> document that hooks should avoid printing secrets; redact patterns but do not guarantee semantic data classification.

## Migration Plan

- Add a new Agent capability, for example `live_task_logs`.
- New Masters accept `task_log` messages but remain compatible with Agents that do not send them.
- New Agents send logs to old Masters harmlessly only if old Master ignores unknown message types; verify current handler behavior before implementation.
- No database migration is required for the first version unless we later persist log snapshots.

## Open Questions

- Should completed-task logs be optionally persisted as a small redacted tail in `task_histories`, or should the first version remain memory-only?
- Should restore tasks use the same log viewer immediately, or should scope start with backup and verification only?
- Should API Tokens need only `read:operational`, or should log viewing require a stricter permission because hook output may contain operational details?
