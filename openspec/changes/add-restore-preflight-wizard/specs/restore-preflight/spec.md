## ADDED Requirements

### Requirement: Guided Restore Planning

VaultFleet SHALL provide a guided restore workflow that captures a complete restore plan before execution.

#### Scenario: Plan cross-agent file restore
- **WHEN** an operator opens restore from a snapshot that belongs to a source agent
- **THEN** the guided workflow requires the operator to select a target agent and target path before file restore can be submitted
- **AND** the plan preserves the source agent ID separately from the target agent ID

#### Scenario: Plan Docker container restore
- **WHEN** an operator opens restore for a snapshot with Docker backup metadata
- **THEN** the guided workflow allows Docker container restore mode
- **AND** the operator can select which Docker source from the snapshot to restore

#### Scenario: Hide Docker mode without Docker metadata
- **WHEN** a snapshot has no Docker backup metadata
- **THEN** the guided workflow does not allow Docker container restore mode for that snapshot

### Requirement: Restore Preflight API

VaultFleet SHALL expose a restore preflight API that validates a restore plan without queuing or dispatching a restore command.

#### Scenario: Preflight returns structured report
- **WHEN** a client submits a restore preflight request
- **THEN** VaultFleet returns an overall status and a list of check results
- **AND** each check result includes a stable code, severity, message, and optional detail

#### Scenario: Preflight does not create command
- **WHEN** a restore preflight request is processed successfully or unsuccessfully
- **THEN** VaultFleet does not create an Agent command
- **AND** VaultFleet does not create a restore task history record

#### Scenario: Blocking errors mark preflight failed
- **WHEN** any preflight check returns severity `error`
- **THEN** the overall preflight status is failed
- **AND** the guided workflow treats the restore plan as not executable

### Requirement: Master-Side Restore Validation

VaultFleet SHALL validate Master-known restore conditions before asking the target Agent to perform host runtime checks.

#### Scenario: Validate source snapshot
- **WHEN** a preflight request references a source agent and snapshot ID
- **THEN** VaultFleet resolves database snapshot IDs to restic snapshot IDs using the source agent
- **AND** VaultFleet returns a blocking error if the source snapshot cannot be resolved

#### Scenario: Validate target agent
- **WHEN** a preflight request references a target agent
- **THEN** VaultFleet returns a blocking error if the target agent does not exist
- **AND** VaultFleet returns a blocking error if the target agent is offline

#### Scenario: Validate selective restore capability
- **WHEN** a preflight request includes selected paths or requires Docker restore paths
- **THEN** VaultFleet verifies that the target agent supports selective restore
- **AND** VaultFleet returns a blocking error if the capability is missing

#### Scenario: Validate Docker metadata
- **WHEN** a preflight request uses Docker container restore mode
- **THEN** VaultFleet loads Docker backup metadata from the source agent snapshot history
- **AND** VaultFleet returns a blocking error if Docker metadata or the selected Docker source is missing

### Requirement: Target Agent File Restore Checks

VaultFleet SHALL ask the target Agent to validate file restore readiness when the target agent is online and supports restore preflight.

#### Scenario: Validate target path writeability
- **WHEN** a file restore preflight request includes a target path
- **THEN** the target Agent verifies that the target path can be created or written
- **AND** the preflight report includes a blocking error if the path is not writable

#### Scenario: Validate selected restore paths
- **WHEN** a file restore preflight request includes selected snapshot paths
- **THEN** VaultFleet validates that the target Agent supports include-path restore
- **AND** the target Agent receives the selected paths in the preflight request for reporting context

#### Scenario: Report missing preflight capability
- **WHEN** the target agent is online but does not advertise restore preflight support
- **THEN** VaultFleet returns a blocking error that instructs the operator to upgrade the Agent

### Requirement: Target Agent Docker Restore Checks

VaultFleet SHALL ask the target Agent to validate Docker restore readiness when Docker container restore mode is selected.

#### Scenario: Validate Docker availability
- **WHEN** a Docker restore preflight request reaches the target Agent
- **THEN** the target Agent checks Docker Engine availability
- **AND** the preflight report includes a blocking error if Docker cannot be reached

#### Scenario: Detect container conflicts
- **WHEN** Docker metadata references a container name or Compose service that already exists on the target host
- **THEN** the preflight report includes a warning or blocking error describing the conflict

#### Scenario: Validate Docker restore paths
- **WHEN** Docker metadata contains resolved host paths for selected mounts or Compose files
- **THEN** the target Agent checks whether those paths can be restored on the target host
- **AND** the preflight report includes actionable errors or warnings for paths that are missing, unwritable, or risky to overwrite

### Requirement: Guided Restore Execution Gate

VaultFleet SHALL require a successful preflight report before enabling final restore execution in the guided Web UI.

#### Scenario: Enable restore after successful preflight
- **WHEN** all preflight checks complete without blocking errors
- **THEN** the guided Web UI enables the final restore action
- **AND** the restore request uses the same source agent, target agent, snapshot, restore mode, selected paths, target path, and Docker source that were preflighted

#### Scenario: Disable restore after failed preflight
- **WHEN** preflight returns one or more blocking errors
- **THEN** the guided Web UI keeps the final restore action disabled
- **AND** the UI displays the blocking errors and remediation messages

#### Scenario: Preserve direct restore API compatibility
- **WHEN** a non-UI client calls the existing restore execution API directly
- **THEN** VaultFleet continues to process the request according to existing restore validation rules
- **AND** the request does not require a preflight token in this change
