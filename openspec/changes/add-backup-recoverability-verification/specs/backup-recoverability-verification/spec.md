## ADDED Requirements

### Requirement: Verification Configuration
VaultFleet SHALL allow snapshot backup policies to define recoverability verification settings.

#### Scenario: Configure verification for snapshot policy
- **WHEN** an operator creates or updates a snapshot backup policy with verification enabled
- **THEN** VaultFleet stores the verification schedule, sample count, sample restore setting, and timeout for that policy

#### Scenario: Reject unsupported archive verification
- **WHEN** an operator attempts to enable restic recoverability verification for an archive-mode policy
- **THEN** VaultFleet MUST reject the setting or report that archive verification is unsupported

#### Scenario: Preserve existing policies
- **WHEN** VaultFleet starts with policies created before verification settings existed
- **THEN** those policies remain valid and keep their existing backup behavior

### Requirement: Manual Verification Trigger
VaultFleet SHALL allow operators to start a recoverability verification for a snapshot backup policy on demand.

#### Scenario: Start verify now
- **WHEN** an operator requests immediate verification for an online Agent with a snapshot backup policy
- **THEN** VaultFleet creates a verification command and task history record associated with the Agent, policy, and storage

#### Scenario: Block unsupported Agent
- **WHEN** an operator requests verification for an Agent that does not advertise verification support
- **THEN** VaultFleet MUST return an actionable error indicating that the Agent must be upgraded

#### Scenario: Queue offline Agent according to command behavior
- **WHEN** an operator requests verification for an offline Agent
- **THEN** VaultFleet SHALL either queue the verification command according to existing command semantics or return a clear offline error

### Requirement: Scheduled Verification
VaultFleet SHALL dispatch recoverability verification according to policy verification schedules.

#### Scenario: Schedule due verification
- **WHEN** a policy verification schedule becomes due
- **THEN** VaultFleet creates at most one pending or running verification command for that policy

#### Scenario: Skip disabled verification
- **WHEN** verification is disabled for a policy
- **THEN** VaultFleet MUST NOT create scheduled verification commands for that policy

#### Scenario: Keep backup schedule independent
- **WHEN** a verification task succeeds or fails
- **THEN** VaultFleet MUST NOT alter the policy's normal backup schedule or retention settings

### Requirement: Agent Verification Execution
VaultFleet Agents SHALL verify the latest restic snapshot for the policy repository using bounded checks.

#### Scenario: Verify latest snapshot
- **WHEN** an Agent receives a verification request for a snapshot policy
- **THEN** the Agent lists repository snapshots, selects the newest snapshot, and includes that snapshot ID in the verification result

#### Scenario: Run required checks
- **WHEN** a verification request runs for a repository with at least one snapshot
- **THEN** the Agent runs repository check, snapshot listing, and sampled listing checks
- **AND** the Agent reports a structured result for each check

#### Scenario: Report empty repository
- **WHEN** the Agent cannot find any snapshots in the repository
- **THEN** the Agent reports verification failure with a structured check explaining that no snapshot is available

#### Scenario: Enforce verification timeout
- **WHEN** verification exceeds the configured timeout
- **THEN** the Agent cancels the running verification work and reports a failed timeout result

### Requirement: Optional Sample Restore
VaultFleet SHALL support an optional sample restore check that writes only to an Agent-controlled temporary directory.

#### Scenario: Run sample restore when enabled
- **WHEN** sample restore is enabled and the selected snapshot contains a restorable file sample
- **THEN** the Agent restores the sample into a temporary verification directory and reports whether the restored content exists

#### Scenario: Clean up sample restore
- **WHEN** a sample restore check finishes
- **THEN** the Agent removes the temporary verification directory
- **AND** any cleanup failure is reported as a warning check result

#### Scenario: Skip sample restore when disabled
- **WHEN** sample restore is disabled
- **THEN** the Agent MUST NOT run a restic restore command as part of verification

### Requirement: Verification Result Persistence
VaultFleet SHALL persist recoverability verification results with structured check details.

#### Scenario: Store successful verification
- **WHEN** all blocking verification checks pass
- **THEN** VaultFleet records the verification task as successful
- **AND** stores the verified snapshot ID and structured check results

#### Scenario: Store failed verification
- **WHEN** any blocking verification check fails
- **THEN** VaultFleet records the verification task as failed
- **AND** stores the failed check code, message, detail, and error log

#### Scenario: Query latest policy verification
- **WHEN** a client lists backup policies
- **THEN** VaultFleet exposes each policy's latest verification status, verified snapshot ID, and verification time when available

### Requirement: Verification Visibility
VaultFleet SHALL make verification state visible to operators in the Web UI.

#### Scenario: Show policy verification status
- **WHEN** an operator views the policy list or policy detail
- **THEN** the UI displays whether the latest verification passed, failed, is running, or has never run

#### Scenario: Show verification task details
- **WHEN** an operator opens a verification task
- **THEN** the UI displays per-check status, severity, message, and duration

#### Scenario: Notify on verification failure
- **WHEN** a verification task fails
- **THEN** VaultFleet emits a task failure event compatible with configured notification channels
