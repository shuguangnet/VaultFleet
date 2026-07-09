## Context

VaultFleet already has a single-Agent update path:

- Master exposes `POST /api/agents/:id/update-agent`.
- Master sends `update_agent` and waits for `update_agent_resp`.
- Agent accepts the request, downloads the release asset, replaces its binary, and restarts.
- Heartbeats report Agent version and capabilities.
- Node tags already exist and can select groups of Agents.

The missing piece is orchestration. A single ACK only means "the Agent accepted the update request"; it does not prove that the new binary downloaded, restarted, reconnected, or reported the target version. For a multi-node fleet, upgrade success must be tracked across reconnects and persisted independently of one HTTP request.

There is also a current behavior that sends `version_info` to mismatched Agents on heartbeat. Since Agents treat version mismatch as a self-update signal, this can bypass canary rollout controls. Controlled rollout needs a policy gate around that automatic path.

## Goals / Non-Goals

**Goals:**

- Let operators create an Agent upgrade rollout from tags and/or explicit Agent IDs.
- Preview rollout targets before execution, including online status, current version, architecture, and expected target version.
- Run a canary phase before broader upgrade.
- Continue in batches only after every Agent in the current phase reconnects and reports the target version.
- Stop the rollout automatically when any target fails or times out.
- Persist rollout and per-Agent item state.
- Expose rollout progress in the UI and API.
- Audit rollout create/cancel and node-level results.
- Prevent automatic version mismatch updates from bypassing active rollout control.

**Non-Goals:**

- Full binary rollback in the first version.
- Maintenance windows or scheduled future rollout start.
- Parallel multi-version upgrade campaigns for the same Agent.
- Cross-repository release discovery or GitHub API integration beyond accepting a target version/repo.
- Streaming live install logs from the Agent updater.

## Decisions

### 1. Model rollouts as persisted Master-side state

Add two database models:

- `AgentUpgradeRollout`
  - ID, target version, GitHub repo, target tags, explicit target Agent IDs, canary count, batch size, status, failure reason, actor metadata, timestamps.
- `AgentUpgradeRolloutItem`
  - Rollout ID, Agent ID, phase (`canary` or `batch`), batch index, status, current version at planning time, target version, message ID, error, started/completed timestamps, last observed version.

Rationale: rollout progress spans Agent restarts and Master page refreshes. Persisted state is simpler and safer than keeping orchestration in memory only.

Alternative considered: reuse `agent_commands` only. Rejected because command status is per-message and cannot represent canary gating, target selection, skipped nodes, or batch progression.

### 2. Use the existing `update_agent` protocol for the first version

The coordinator sends the existing `update_agent` message to each item and records the ACK message ID. The item remains running until the Agent later heartbeats with `agent_version == target_version`.

Rationale: the Agent updater already exists. This keeps the first change focused on orchestration, UI, and state tracking.

Alternative considered: add a new `agent_update_progress` protocol stream. Rejected for P0 because the current Agent updater does not expose structured install steps and rollout correctness primarily needs reconnect/version confirmation.

### 3. Success means ACK plus target-version heartbeat

An item is successful only when both are true:

- The Agent accepted the `update_agent` request.
- The Agent reconnects or heartbeats and reports the requested target version.

The ACK alone moves the item to an "updating" state, not success.

Rationale: a restart or download failure can happen after ACK. Version heartbeat is the durable proof that the target binary is running.

### 4. Active rollout controls automatic version mismatch updates

Add a Master-side update mode or gate for heartbeat mismatch handling:

- When a controlled rollout is active for an Agent, heartbeat mismatch handling must not send an unsolicited `version_info` update outside the rollout coordinator.
- If a global auto-update mode is introduced, default behavior for controlled rollout safety should be `notify_only` or "skip auto-update while rollout exists".

Rationale: uncontrolled `version_info` updates break canary semantics.

Alternative considered: leave existing automatic updates unchanged. Rejected because Agents outside canary could upgrade before the rollout coordinator approves them.

### 5. Execute rollouts through a coordinator service

Add a Master coordinator service that:

1. Resolves targets from explicit IDs and tag filters.
2. Creates rollout items.
3. Starts canary items first.
4. Advances to batch items only after all running items in the current phase succeed.
5. Stops and marks remaining pending items as skipped when any item fails.
6. Can resume active rollouts after Master restart by inspecting persisted state.

The coordinator can run as a lightweight background loop plus explicit triggers after rollout creation and Agent heartbeat updates.

### 6. Keep first-version concurrency conservative

Defaults:

- canary count: 1
- batch size: 5
- item timeout: 10 minutes unless configured internally

The API should validate canary count and batch size are positive and bounded.

Rationale: Agent upgrade is a control-plane risk. Small defaults reduce blast radius and match expected VPS fleet sizes.

## Risks / Trade-offs

- [Risk] Agent accepts update but fails before reconnecting. -> Mitigation: item timeout marks the node failed and stops remaining rollout.
- [Risk] Master restarts while a rollout is active. -> Mitigation: persisted rollout/items plus coordinator resume on startup.
- [Risk] Existing automatic version mismatch update bypasses canary. -> Mitigation: gate `version_info` update while rollout control applies.
- [Risk] Offline nodes make rollout planning confusing. -> Mitigation: preflight marks them blocked/skipped by default and does not wait indefinitely.
- [Risk] Version strings vary between release tags and development builds. -> Mitigation: use existing `defaultAgentUpdateVersion` behavior and display exact requested target version in all rollout state.
- [Risk] Multiple active rollouts target the same Agent. -> Mitigation: reject creation when any selected Agent has a non-terminal rollout item.
- [Risk] ACK timeout does not always mean update failed; it may be slow network. -> Mitigation: classify as timeout, stop safely, and allow a later retry/new rollout.

## Migration Plan

1. Add nullable/new tables through GORM auto-migration.
2. Existing deployments continue using single-Agent update until a rollout is created.
3. Deploying the feature should not require Agent changes if the existing `update_agent` protocol is sufficient.
4. If rollback is needed, terminal rollout rows can remain unused; they do not affect backup or restore behavior.

## Open Questions

- Should the first version expose a global `agent_auto_update_mode`, or only suppress automatic updates while any active rollout targets the Agent?
- Should operators be allowed to include offline Agents as pending future targets, or should P0 always skip them during planning?
- Should canary selection be automatic first target, or should UI allow choosing named canary nodes in P0?
- Should rollout notifications be added immediately, or left to a follow-up after core state and UI are stable?
