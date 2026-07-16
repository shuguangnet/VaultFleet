## ADDED Requirements

### Requirement: Multi-Source Docker Selection
VaultFleet SHALL allow an operator to select one or more Docker sources from a single snapshot for one restore plan.

#### Scenario: Select several containers
- **WHEN** a snapshot contains multiple Docker sources and the target Agent supports multi-container restore
- **THEN** the restore workflow allows the operator to select several sources
- **AND** the confirmation view lists every selected source and the total count

#### Scenario: Preserve single-container compatibility
- **WHEN** a restore request contains only the legacy single Docker source field
- **THEN** VaultFleet processes it as a one-item Docker restore plan

#### Scenario: Reject invalid selection
- **WHEN** a multi-container restore request contains no source, an unknown source ID, or more than the supported item limit
- **THEN** VaultFleet rejects the request before creating an Agent command

### Requirement: Capability-Gated Batch Restore
VaultFleet SHALL dispatch multiple Docker sources only to an Agent that advertises multi-container restore capability.

#### Scenario: Target supports batch restore
- **WHEN** the target Agent advertises `docker_multi_container_restore`
- **THEN** VaultFleet may dispatch all selected Docker sources in one restore command

#### Scenario: Target lacks batch restore capability
- **WHEN** the target Agent supports Docker container restore but does not advertise multi-container restore
- **THEN** the Web workflow remains limited to one Docker source
- **AND** the Master rejects a direct multi-source request with an actionable upgrade error

### Requirement: Batch Restore Preflight
VaultFleet SHALL preflight the complete Docker source selection before enabling execution.

#### Scenario: Report checks for each source
- **WHEN** preflight evaluates a multi-container restore plan
- **THEN** source-specific findings identify the related source ID and display name
- **AND** common findings that affect the entire batch remain visible as batch-level checks

#### Scenario: Detect conflicts inside the selection
- **WHEN** selected sources conflict through container names, Compose identities, target paths, or host ports
- **THEN** preflight returns structured conflict findings before restore execution

#### Scenario: Block on any preflight error
- **WHEN** any batch-level or source-level preflight check has error severity
- **THEN** the guided workflow disables final restore execution

### Requirement: Deterministic Batch Execution
The Agent SHALL restore data for the selected sources once and rebuild Docker sources sequentially in a deterministic order.

#### Scenario: Restore shared paths once
- **WHEN** multiple selected sources reference the same resolved path
- **THEN** the Agent includes that path only once in the data restore operation

#### Scenario: Rebuild in request order
- **WHEN** the data restore completes successfully
- **THEN** the Agent rebuilds the selected Docker sources one at a time in request order

#### Scenario: Cancel between sources
- **WHEN** an operator cancels a batch after one source completes
- **THEN** the Agent does not start the next source
- **AND** the final result identifies completed and canceled or skipped items

### Requirement: Compose Environment Preservation
The Agent SHALL preserve the environment-file context required to validate and recreate a Docker Compose service.

#### Scenario: Back up Compose environment files
- **WHEN** a discovered Compose project declares environment files or has a readable conventional `.env` file in its working directory
- **THEN** the Agent records the environment file paths in Docker metadata
- **AND** includes those files in the resolved backup paths without exposing their contents

#### Scenario: Restore Compose with its project environment
- **WHEN** a selected Docker source has usable Compose configuration and environment files
- **THEN** the Agent runs Compose with the recorded project directory and explicit environment files
- **AND** variables such as `${CONTAINER_NAME}` are resolved before Compose validation

#### Scenario: Block missing Compose variables before execution
- **WHEN** a Compose configuration references environment variables and no recorded or conventional environment file is usable
- **THEN** restore preflight returns a source-attributed error
- **AND** the Agent does not attempt Compose restoration with empty substitutions

#### Scenario: Preserve Compose files without variables
- **WHEN** an older snapshot has Compose configuration without variable references and no environment-file metadata
- **THEN** the Agent can restore the service using the existing backward-compatible Compose path

### Requirement: Isolated Item Outcomes
VaultFleet SHALL record an independent outcome for every selected Docker source and continue after a source-specific rebuild failure.

#### Scenario: Continue after one container fails
- **WHEN** rebuilding one selected Docker source fails after data restoration succeeded
- **THEN** the Agent records that source as failed
- **AND** the Agent attempts the remaining selected sources

#### Scenario: Data restore fails before rebuild
- **WHEN** restoring the selected data paths fails
- **THEN** the batch is marked failed
- **AND** every Docker source that was not started is recorded as skipped

#### Scenario: Derive aggregate status
- **WHEN** all selected sources finish
- **THEN** the task status is `success` if all items succeeded, `partial_success` if successes and failures coexist, and `failed` if no item succeeded

### Requirement: Batch Restore Progress and Logs
VaultFleet SHALL expose batch-level and current-source progress under one stable message ID.

#### Scenario: Show current source progress
- **WHEN** the Agent starts or completes a Docker source
- **THEN** it reports the total item count, completed count, failed count, current source ID, and current source name

#### Scenario: Persist logs and item results
- **WHEN** a batch restore emits logs, progress, and a final result
- **THEN** the Master associates them with the same task and message ID
- **AND** task details remain available after a Master restart

### Requirement: Retry Failed Sources
VaultFleet SHALL allow an operator to create a new restore attempt containing only retryable failed sources from a completed batch.

#### Scenario: Retry partial failure
- **WHEN** a batch finishes with one or more retryable failed sources
- **THEN** task details offer a retry action containing only those failed source IDs
- **AND** the new attempt preserves the original source agent, target agent, snapshot, and restore mode

#### Scenario: Re-run preflight before retry
- **WHEN** an operator retries failed sources
- **THEN** VaultFleet performs a new preflight for the reduced selection
- **AND** the original task and item results remain unchanged

### Requirement: Operator Confirmation
VaultFleet SHALL present the complete batch scope and failure semantics before destructive restore execution.

#### Scenario: Confirm batch restore
- **WHEN** preflight passes for more than one selected source
- **THEN** the confirmation view shows the target node, snapshot, selected sources, affected paths, execution order, and overwrite warnings
- **AND** the operator must explicitly confirm before the restore command is created
