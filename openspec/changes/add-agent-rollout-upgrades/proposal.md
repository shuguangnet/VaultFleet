## Why

VaultFleet currently supports updating one Agent at a time, but multi-node deployments need a controlled rollout flow so operators can upgrade by tag, validate a canary node first, and stop before a bad release reaches every server.

This is especially important because the existing version mismatch path can trigger Agent self-updates automatically, which conflicts with deliberate gray/canary rollout operations.

## What Changes

- Add first-class Agent upgrade rollouts that target explicit nodes and/or nodes matched by tags.
- Add rollout planning and preflight results for target Agent status, current version, architecture, and self-update readiness.
- Execute a canary phase before broader rollout, requiring the canary Agent to reconnect and report the target version before continuing.
- Execute remaining targets in bounded batches and stop the rollout automatically when any node fails.
- Persist rollout and per-node item status so progress survives page refreshes and Master restarts.
- Add UI for creating rollouts, previewing selected targets, following progress, and seeing failure/skipped reasons.
- Adjust automatic Agent version mismatch handling so it does not bypass active controlled rollouts.
- Record audit events for rollout creation, cancellation, and node-level upgrade outcomes.

## Capabilities

### New Capabilities

- `agent-rollout-upgrades`: Controlled Agent upgrade rollout planning, canary execution, batched progression, failure stop behavior, and operator visibility.

### Modified Capabilities

- None.

## Impact

- Master database: new persisted rollout and rollout item models, plus migrations.
- Master API: new endpoints for creating, listing, reading, cancelling, and optionally retrying Agent upgrade rollouts.
- Master background processing: rollout coordinator that sends `update_agent` messages and advances canary/batch state from Agent reconnect/version reports.
- Master WebSocket/heartbeat handling: integrate version reports with rollout state and prevent automatic version mismatch updates from bypassing controlled rollouts.
- Agent protocol: reuse existing `update_agent` request/response where possible; only extend protocol if progress/error reporting needs stronger semantics.
- Web UI: Nodes page rollout drawer, target preview, rollout progress table, and status indicators.
- RBAC/audit: restrict mutation to roles allowed to manage nodes and log sensitive rollout operations.
- Documentation: update operator guidance for Agent upgrades, canary behavior, failure stop rules, and interaction with automatic updates.
