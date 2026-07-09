## 1. Data Model and Contracts

- [x] 1.1 Add `AgentUpgradeRollout` and `AgentUpgradeRolloutItem` database models with terminal/non-terminal statuses, phase, batch index, target version, selected tags/agents, message ID, error, and timestamps.
- [x] 1.2 Add GORM migration coverage for the new rollout tables without changing existing Agent, command, backup, or restore data.
- [x] 1.3 Add Go response/request DTOs for rollout create, list, detail, cancel, item summary counts, and target preview.
- [x] 1.4 Add frontend TypeScript types for rollout status, rollout item status, create request, list response, and detail response.

## 2. Rollout Planning and Validation

- [x] 2.1 Implement target resolution from explicit Agent IDs and normalized tag filters, including de-duplication and stable ordering.
- [x] 2.2 Validate target version, GitHub repository, canary count, batch size, and non-empty target selection.
- [x] 2.3 Reject rollout creation when any selected Agent already has a non-terminal rollout item.
- [x] 2.4 Record preflight state for each target Agent: online/offline, current version, architecture, already-on-target, skipped/blocked reason, and planned phase.
- [x] 2.5 Add backend tests for tag target resolution, explicit Agent selection, empty targets, duplicate active target rejection, offline skip behavior, and already-current success behavior.

## 3. Rollout Coordinator

- [x] 3.1 Implement a Master-side rollout coordinator service that can start, resume, and advance active rollouts from persisted state.
- [x] 3.2 Reuse existing `update_agent` protocol messages to send update requests and record accepted/rejected ACK results on rollout items.
- [x] 3.3 Treat item success as target-version heartbeat confirmation, not merely `update_agent_resp` acceptance.
- [x] 3.4 Enforce canary-first execution and prevent non-canary batches from starting before all canary items succeed.
- [x] 3.5 Enforce batch size limits and wait for all running items in the current batch before starting the next batch.
- [x] 3.6 Stop rollouts on item failure, rejection, or timeout, and mark remaining pending items skipped with a clear reason.
- [x] 3.7 Resume non-terminal rollouts after Master startup without duplicating successful item update requests.
- [x] 3.8 Add coordinator tests for canary success progression, canary failure stop, batch progression, timeout stop, restart resume, and already-current completion.

## 4. Heartbeat and Automatic Update Gating

- [x] 4.1 Wire Agent heartbeat version reports into the rollout coordinator so running items complete when the target version is observed.
- [x] 4.2 Gate existing heartbeat `version_info` automatic update behavior so Agents with non-terminal rollout items do not receive unsolicited updates outside the rollout coordinator.
- [x] 4.3 Preserve existing automatic update behavior for Agents with no active rollout gate.
- [x] 4.4 Add WebSocket handler tests proving active rollout gates suppress `version_info` and non-rollout Agents still follow configured mismatch behavior.

## 5. API, RBAC, and Audit

- [x] 5.1 Add protected API routes to create, list, read detail, and cancel Agent upgrade rollouts.
- [x] 5.2 Enforce existing node-write/operator permissions for rollout mutations and read permissions for rollout visibility.
- [x] 5.3 Add audit events for rollout create, cancel, item success/failure/skipped, and rollout terminal status.
- [x] 5.4 Add API tests for authorization, create/list/detail/cancel responses, validation errors, audit records, and status/count summaries.
- [x] 5.5 Keep the existing single-Agent update endpoint working for direct manual updates.

## 6. Frontend UI

- [x] 6.1 Add services and React Query hooks for rollout create, list, detail, and cancel APIs.
- [x] 6.2 Add a Nodes page "批量升级" entry point visible only to authorized operators/admins.
- [x] 6.3 Build a rollout creation drawer with target version, GitHub repo, tag selector, explicit Agent selector, canary count, batch size, and resolved target preview.
- [x] 6.4 Show rollout list/progress with status, canary progress, batch progress, item counts, failed item, skipped reason, and current/target versions.
- [x] 6.5 Add clear failure-stop messaging so operators can see why later items were skipped.
- [x] 6.6 Add frontend tests for drawer validation, target preview rendering, successful create request payload, rollout progress rendering, and permission-gated actions.

## 7. Documentation and Verification

- [x] 7.1 Update README and/or docs with Agent rollout upgrade workflow, canary semantics, batch behavior, failure stop rules, and automatic update interaction.
- [x] 7.2 Document operational guidance for choosing canary nodes, batch size, and handling failed rollouts.
- [x] 7.3 Run targeted Go tests for rollout models, API, coordinator, and WebSocket heartbeat gating.
- [x] 7.4 Run targeted frontend tests for the Nodes page rollout UI and TypeScript build.
- [x] 7.5 Run `openspec validate --changes add-agent-rollout-upgrades` and resolve any spec or task issues.
