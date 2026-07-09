## ADDED Requirements

### Requirement: Subscribe to backup completion outcomes
VaultFleet SHALL allow notification configs to subscribe independently to successful and failed backup task outcomes.

#### Scenario: Backup success notification
- **WHEN** an Agent reports a backup task result with a successful terminal status
- **THEN** VaultFleet sends notifications only to configs subscribed to `backup_succeeded`

#### Scenario: Backup failure notification
- **WHEN** an Agent reports a backup task result with a failed, timed out, or cancelled terminal status
- **THEN** VaultFleet sends notifications to configs subscribed to `backup_failed`

#### Scenario: Success events are opt-in
- **WHEN** a notification config is subscribed only to `backup_failed`
- **THEN** VaultFleet MUST NOT send that config a successful backup notification

### Requirement: Subscribe to backup verification outcomes
VaultFleet SHALL allow notification configs to subscribe independently to successful and failed backup recoverability verification outcomes.

#### Scenario: Verification success notification
- **WHEN** an Agent reports a verification task result with a successful terminal status
- **THEN** VaultFleet sends notifications only to configs subscribed to `backup_verification_succeeded`

#### Scenario: Verification failure notification
- **WHEN** an Agent reports a verification task result with a failed, timed out, or cancelled terminal status
- **THEN** VaultFleet sends notifications to configs subscribed to `backup_verification_failed`

#### Scenario: Verification failure compatibility
- **WHEN** an existing notification config is subscribed to `backup_failed`
- **AND** an Agent reports a failed verification task result
- **THEN** VaultFleet continues sending a failure notification to that config for compatibility

### Requirement: Render task-result notification context
VaultFleet SHALL include useful task result context in backup and verification notification messages.

#### Scenario: Successful backup context
- **WHEN** VaultFleet sends a successful backup notification
- **THEN** the notification includes the Agent name, task status, completion time, duration when available, snapshot ID when available, and artifact information when available

#### Scenario: Failed task context
- **WHEN** VaultFleet sends a failed backup or verification notification
- **THEN** the notification includes the Agent name, task status, completion time, and error text when available

### Requirement: Configure notification events in the API and UI
VaultFleet SHALL expose supported notification events through API validation and Web UI configuration.

#### Scenario: API accepts new notification events
- **WHEN** an operator creates or updates a notification config with `backup_succeeded`, `backup_verification_succeeded`, or `backup_verification_failed`
- **THEN** VaultFleet persists the config and returns the selected event array

#### Scenario: UI shows new event options
- **WHEN** an operator opens the notification configuration drawer
- **THEN** the UI offers backup success, backup failure, verification success, verification failure, and Agent offline event options

#### Scenario: API rejects unknown notification events
- **WHEN** an operator submits a notification config with an unsupported event name
- **THEN** VaultFleet rejects the request with a validation error
