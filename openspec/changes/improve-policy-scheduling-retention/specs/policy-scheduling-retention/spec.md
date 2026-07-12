## ADDED Requirements

### Requirement: Visual schedule builder
The policy editor SHALL provide visual schedule modes for daily, weekly, monthly, and fixed-interval execution, and SHALL generate a standard five-field Cron expression as the persisted `schedule` value.

#### Scenario: Configure a daily backup
- **WHEN** an operator selects daily execution at 02:30
- **THEN** the editor generates `30 2 * * *` and displays that the policy runs every day at 02:30 in the node's local time

#### Scenario: Configure multiple weekly executions
- **WHEN** an operator selects Monday, Wednesday, and Friday at 03:00
- **THEN** the editor generates an equivalent weekly Cron expression and describes the three selected weekdays explicitly

#### Scenario: Reject ambiguous weekly frequency
- **WHEN** an operator wants a policy to run three times per week
- **THEN** the editor requires the operator to select three explicit weekdays instead of automatically choosing dates

#### Scenario: Configure a monthly backup
- **WHEN** an operator selects day 15 of each month at 01:00
- **THEN** the editor generates `0 1 15 * *` and presents the monthly schedule in business language

#### Scenario: Configure an interval
- **WHEN** an operator selects execution every 6 hours
- **THEN** the editor generates an equivalent Cron schedule and displays the interval summary

### Requirement: Custom Cron compatibility
The policy editor SHALL retain a custom Cron mode, SHALL preserve valid expressions exactly, and SHALL not silently convert expressions that the visual builder cannot represent without loss.

#### Scenario: Edit a recognizable expression
- **WHEN** an existing policy has `0 2 * * *`
- **THEN** the editor may open the daily visual mode with 02:00 selected while preserving the equivalent persisted schedule

#### Scenario: Edit a complex expression
- **WHEN** an existing policy contains a valid expression that is not exactly representable by a visual mode
- **THEN** the editor opens custom mode and leaves the original expression unchanged

#### Scenario: Switch away from custom mode
- **WHEN** an operator switches a complex custom expression to a visual mode
- **THEN** the editor requires a complete visual schedule selection before replacing the custom expression

### Requirement: Schedule validation and explanation
The system SHALL validate schedules before policy creation or update and SHALL present a human-readable explanation for valid schedules.

#### Scenario: Reject invalid Cron in the editor
- **WHEN** an operator enters an invalid custom Cron expression
- **THEN** the editor displays an inline error and disables policy submission

#### Scenario: Reject invalid Cron at the API
- **WHEN** a client submits an invalid schedule directly to the policy API
- **THEN** the Master rejects the request with a validation error and does not persist the policy

#### Scenario: Preserve supported legacy formats
- **WHEN** a client submits a valid five-field, six-field, or supported descriptor expression already accepted by the Agent scheduler
- **THEN** the Master accepts it even if the visual builder cannot represent it

### Requirement: Node-local time semantics
The UI SHALL state that backup schedules execute in the target Agent's local time and SHALL not present browser-local preview times as authoritative Agent execution times.

#### Scenario: Display schedule timezone
- **WHEN** an operator views or edits a schedule
- **THEN** the schedule summary includes a visible “节点本地时间” indication

### Requirement: Clear retention configuration
The policy editor SHALL expose retention as “keep latest N”, “keep N daily”, “keep N weekly”, and “keep N monthly” snapshots, with presets and independently editable custom values.

#### Scenario: Keep the latest seven backups
- **WHEN** an operator selects custom retention and sets “保留最近” to 7
- **THEN** the submitted retention contains `keep_last: 7`

#### Scenario: Disable a retention tier
- **WHEN** an operator sets a retention value to 0
- **THEN** the UI explains that the corresponding tier is disabled and submits 0 for that field

#### Scenario: Explain combined retention
- **WHEN** more than one retention tier is enabled
- **THEN** the UI explains that snapshots matching any tier are retained and that one snapshot may satisfy multiple tiers

#### Scenario: Reject an empty retention policy
- **WHEN** all four retention values are 0
- **THEN** the editor and Master reject the policy and require at least one enabled retention tier

### Requirement: Policy schedule and retention summaries
The system SHALL display separate readable summaries for execution schedule and snapshot retention without requiring operators to interpret raw Cron or restic field names.

#### Scenario: Review policy before saving
- **WHEN** a valid schedule and retention policy are configured
- **THEN** the editor displays one summary for when backups run and another summary for which snapshots remain

#### Scenario: View an existing policy
- **WHEN** an operator views the policy list
- **THEN** each policy displays a concise readable schedule while the raw Cron remains available as secondary technical detail
