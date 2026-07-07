## ADDED Requirements

### Requirement: Agent Emits Task-Scoped Live Logs
The system SHALL allow capable Agents to emit redacted live log lines associated with a backup-related task message ID.

#### Scenario: Backup emits stage logs
- **WHEN** a capable Agent runs a backup task
- **THEN** the Agent sends task log events for major phases including initialization, backup execution, retention, snapshot listing, and stats collection

#### Scenario: Hooks emit bounded output logs
- **WHEN** a backup policy runs pre-backup or post-backup hooks that produce output
- **THEN** the Agent sends bounded, redacted task log events for hook start, hook output, hook completion, and hook failure

#### Scenario: Unsupported Agent does not block backup
- **WHEN** an older Agent does not support live task logs
- **THEN** backup execution and progress reporting continue without live log events

### Requirement: Live Logs Are Redacted Before Leaving Agent
The system SHALL redact sensitive values from live log lines before sending them from Agent to Master.

#### Scenario: Secret-like output is redacted
- **WHEN** a log line contains password, token, secret, access key, Authorization header, restic password, or known rclone secret values
- **THEN** the Agent replaces the sensitive value with a redacted placeholder before sending the log event

#### Scenario: Master applies defensive redaction
- **WHEN** Master receives a task log event
- **THEN** Master applies the existing redaction rules again before buffering or returning the line

### Requirement: Master Buffers Live Logs By Task
The system SHALL keep a bounded, task-scoped live log buffer keyed by Agent ID and task message ID.

#### Scenario: Buffer stores recent lines
- **WHEN** Master receives live log events for a running task
- **THEN** Master stores recent lines with monotonically increasing sequence numbers and timestamps

#### Scenario: Buffer enforces limits
- **WHEN** a task emits more log data than the configured line or byte limit
- **THEN** Master drops oldest lines and reports that the returned log set is truncated

#### Scenario: Completed task logs remain temporarily available
- **WHEN** a task completes
- **THEN** Master keeps its recent live log buffer available for a bounded retention window or until Master restart

### Requirement: Users Can Retrieve Task Logs
The system SHALL expose authenticated APIs to retrieve live log lines for tasks and command-backed work.

#### Scenario: Fetch task logs
- **WHEN** an authorized user requests logs for a task ID
- **THEN** the system returns available log lines, the latest sequence, truncation metadata, and task identity

#### Scenario: Fetch incremental logs
- **WHEN** an authorized user requests logs after a specific sequence number
- **THEN** the system returns only newer log lines up to the requested or default limit

#### Scenario: Deny unauthenticated or unauthorized access
- **WHEN** a request lacks operational read permission
- **THEN** the system denies access to task logs

### Requirement: Web UI Shows Live Backup Logs
The system SHALL provide a task log viewer in the Web UI for running and recent backup-related tasks.

#### Scenario: View logs from task history
- **WHEN** a user opens logs for a backup or verification task
- **THEN** the UI displays log lines in chronological order with timestamp, phase, stream, and message text

#### Scenario: Auto-follow running logs
- **WHEN** the log viewer is open for a running task
- **THEN** the UI polls incrementally and keeps the view scrolled to the latest line unless the user pauses follow mode

#### Scenario: Empty logs are explained
- **WHEN** no live logs are available for a task
- **THEN** the UI shows whether logs are unavailable because the task has no message ID, the Agent is too old, the buffer expired, or no lines have been emitted yet

### Requirement: Live Logs Do Not Replace Existing Progress
The system SHALL continue returning existing progress and final task result fields independently of live logs.

#### Scenario: Progress remains visible
- **WHEN** a running backup emits both progress and live logs
- **THEN** task list responses still include the latest backup progress and the UI continues showing the existing progress summary

#### Scenario: Final errors remain in task history
- **WHEN** a backup fails after emitting live logs
- **THEN** the final task history still records the structured status and error log as it does today
