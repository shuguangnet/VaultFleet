## ADDED Requirements

### Requirement: Create Agent upgrade rollouts
VaultFleet SHALL allow authorized operators to create an Agent upgrade rollout targeting explicit Agents, tag-matched Agents, or both.

#### Scenario: Create rollout from tags
- **WHEN** an authorized operator creates a rollout with target tags, target version, GitHub repository, canary count, and batch size
- **THEN** VaultFleet resolves matching Agents, persists the rollout, persists one rollout item per resolved Agent, and returns the rollout ID and planned target summary

#### Scenario: Create rollout from explicit Agents
- **WHEN** an authorized operator creates a rollout with explicit Agent IDs
- **THEN** VaultFleet persists rollout items only for the selected Agents that exist

#### Scenario: Reject empty target selection
- **WHEN** an authorized operator creates a rollout without explicit Agent IDs and without target tags
- **THEN** VaultFleet rejects the request with a validation error

#### Scenario: Reject duplicate active target
- **WHEN** any selected Agent already has a non-terminal rollout item in another rollout
- **THEN** VaultFleet rejects the new rollout and identifies the conflicting Agent

### Requirement: Preflight rollout targets
VaultFleet SHALL evaluate rollout targets before upgrade execution and record target readiness per Agent.

#### Scenario: Online compatible Agent is planned
- **WHEN** a target Agent exists, is online, has reported an architecture, and has reported an Agent version
- **THEN** VaultFleet marks the rollout item pending with its current version, target version, architecture, and planned phase

#### Scenario: Offline Agent is skipped
- **WHEN** a target Agent is offline during rollout creation
- **THEN** VaultFleet marks that rollout item skipped or blocked with an offline reason and does not wait indefinitely for it to come online

#### Scenario: Agent already on target version
- **WHEN** a target Agent already reports the requested target version
- **THEN** VaultFleet marks that rollout item successful without sending an update command

### Requirement: Execute canary before broader rollout
VaultFleet SHALL upgrade canary items before starting non-canary rollout batches.

#### Scenario: Canary starts first
- **WHEN** a rollout is created with canary count greater than zero
- **THEN** VaultFleet sends `update_agent` only to the canary item set before starting any later batch

#### Scenario: Canary success advances rollout
- **WHEN** every canary item accepts the update request and later heartbeats with the target Agent version
- **THEN** VaultFleet marks canary items successful and starts the first non-canary batch

#### Scenario: Canary failure stops rollout
- **WHEN** any canary item rejects the update request, times out, or fails to report the target version before its deadline
- **THEN** VaultFleet marks the rollout failed and marks all unstarted items skipped

### Requirement: Execute upgrades in bounded batches
VaultFleet SHALL upgrade non-canary rollout items in bounded batches after canary success.

#### Scenario: Batch size limits parallel updates
- **WHEN** a rollout has pending non-canary items and a batch size of N
- **THEN** VaultFleet sends update requests to at most N pending items in the current batch

#### Scenario: Next batch waits for current batch
- **WHEN** a batch has running items
- **THEN** VaultFleet does not start the next batch until every item in the current batch reaches success

#### Scenario: Batch success completes rollout
- **WHEN** all non-skipped rollout items have reached success
- **THEN** VaultFleet marks the rollout successful with completed timestamps

### Requirement: Stop rollout on failure
VaultFleet SHALL stop a rollout when any active item fails, rejects, or times out.

#### Scenario: Update request rejected
- **WHEN** an Agent responds to `update_agent` with `accepted=false`
- **THEN** VaultFleet marks that item failed, records the Agent error, marks the rollout failed, and skips all remaining pending items

#### Scenario: Version confirmation timeout
- **WHEN** an Agent accepted an update request but does not heartbeat with the target version before the item deadline
- **THEN** VaultFleet marks that item failed with a timeout reason and stops the rollout

#### Scenario: Agent disconnect during update
- **WHEN** an Agent disconnects after accepting an update
- **THEN** VaultFleet keeps the item running until the Agent either heartbeats with the target version or reaches its deadline

### Requirement: Persist and resume rollout state
VaultFleet SHALL persist rollout and per-Agent item state so rollout progress is available after page refreshes and Master restarts.

#### Scenario: List persisted rollouts
- **WHEN** an authorized user lists Agent upgrade rollouts
- **THEN** VaultFleet returns rollout status, target version, counts by item status, target tags, selected Agents, and timestamps

#### Scenario: Read rollout details
- **WHEN** an authorized user reads a rollout detail
- **THEN** VaultFleet returns every rollout item with Agent identity, phase, batch index, status, current version, target version, message ID, error text, and timestamps

#### Scenario: Resume active rollout after restart
- **WHEN** Master starts and finds a rollout in a non-terminal state
- **THEN** VaultFleet resumes coordination from persisted rollout item statuses without duplicating successful item updates

### Requirement: Gate automatic Agent version updates
VaultFleet SHALL prevent automatic version mismatch updates from bypassing controlled rollout execution.

#### Scenario: Active rollout controls selected Agent
- **WHEN** an Agent heartbeats with a version different from Master while it has a non-terminal rollout item
- **THEN** VaultFleet does not send an unsolicited `version_info` self-update outside the rollout coordinator

#### Scenario: No active rollout
- **WHEN** an Agent heartbeats with a version different from Master and no rollout gate applies
- **THEN** VaultFleet follows the configured Agent auto-update behavior

### Requirement: Enforce permissions and audit rollout actions
VaultFleet SHALL restrict Agent rollout mutations to authorized roles and record audit events for sensitive rollout operations.

#### Scenario: Viewer cannot create rollout
- **WHEN** a viewer attempts to create or cancel an Agent upgrade rollout
- **THEN** VaultFleet rejects the request with an authorization error

#### Scenario: Operator creates rollout
- **WHEN** an admin or operator creates an Agent upgrade rollout
- **THEN** VaultFleet accepts the request if validation passes and records an audit event with target version and target selection summary

#### Scenario: Rollout item completes
- **WHEN** a rollout item reaches success, failure, or skipped
- **THEN** VaultFleet records enough persisted status for audit and troubleshooting without exposing secrets

### Requirement: Show rollout controls and progress in the UI
VaultFleet SHALL provide UI controls for creating Agent upgrade rollouts and viewing rollout progress.

#### Scenario: Create rollout from Nodes page
- **WHEN** an authorized operator opens the Nodes page
- **THEN** the UI provides a batch upgrade entry point with target version, GitHub repository, tag selector, Agent selector, canary count, batch size, and target preview

#### Scenario: View rollout progress
- **WHEN** a rollout exists
- **THEN** the UI shows rollout status, canary and batch progress, item failures, skipped nodes, and current reported Agent versions

#### Scenario: Stop on failed rollout is visible
- **WHEN** a rollout stops because one item failed
- **THEN** the UI clearly shows the failed item and why later pending items were skipped
