## ADDED Requirements

### Requirement: Configure database backup sources
VaultFleet SHALL allow backup policies to include typed database backup sources for PostgreSQL and MySQL.

#### Scenario: Save PostgreSQL source
- **WHEN** an operator creates a policy with a PostgreSQL database source
- **THEN** VaultFleet stores the source engine, execution mode, target database or all-databases setting, username, non-secret connection fields, output options, and encrypted password

#### Scenario: Save MySQL source
- **WHEN** an operator creates a policy with a MySQL database source
- **THEN** VaultFleet stores the source engine, execution mode, target database or all-databases setting, username, non-secret connection fields, output options, and encrypted password

#### Scenario: Redact database secrets
- **WHEN** VaultFleet returns policy data through API responses or UI payloads
- **THEN** database source passwords are omitted or masked and are never returned in clear text

### Requirement: Validate database source compatibility
VaultFleet SHALL validate database backup source configuration before saving or pushing a policy.

#### Scenario: Reject unsupported Agent
- **WHEN** a policy includes a database source for an Agent that does not advertise database backup support
- **THEN** Master rejects the policy with a clear validation error

#### Scenario: Reject invalid database source
- **WHEN** a database source is missing its engine, execution mode, username, or required database selection
- **THEN** Master rejects the policy with a clear validation error naming the missing field

#### Scenario: Validate Docker execution target
- **WHEN** a database source uses Docker execution mode
- **THEN** VaultFleet requires a selected Docker container identity and validates that the Agent supports Docker-backed source execution

### Requirement: Execute PostgreSQL logical dumps
VaultFleet SHALL execute PostgreSQL database backup sources as logical dump files before the normal backup runner starts.

#### Scenario: Dump one PostgreSQL database
- **WHEN** a backup runs for a PostgreSQL source targeting one database
- **THEN** the Agent runs `pg_dump` with the configured connection and writes a staged SQL dump file

#### Scenario: Dump all PostgreSQL databases
- **WHEN** a backup runs for a PostgreSQL source configured for all databases
- **THEN** the Agent runs `pg_dumpall` with the configured connection and writes a staged SQL dump file

#### Scenario: PostgreSQL dump command missing
- **WHEN** the required PostgreSQL dump command is unavailable in the selected execution environment
- **THEN** the backup task fails with a clear error naming the missing command

### Requirement: Execute MySQL logical dumps
VaultFleet SHALL execute MySQL database backup sources as logical dump files before the normal backup runner starts.

#### Scenario: Dump one MySQL database
- **WHEN** a backup runs for a MySQL source targeting one database
- **THEN** the Agent runs `mysqldump` for that database and writes a staged SQL dump file

#### Scenario: Dump all MySQL databases
- **WHEN** a backup runs for a MySQL source configured for all databases
- **THEN** the Agent runs `mysqldump --all-databases` and writes a staged SQL dump file

#### Scenario: MySQL dump command missing
- **WHEN** `mysqldump` is unavailable in the selected execution environment
- **THEN** the backup task fails with a clear error naming the missing command

### Requirement: Include staged database dumps in backups
VaultFleet SHALL include generated database dump files in both snapshot and archive backup modes.

#### Scenario: Include dump in snapshot backup
- **WHEN** a snapshot-mode backup includes a database source and the dump succeeds
- **THEN** the generated dump file is included in the restic backup paths for that task

#### Scenario: Include dump in archive backup
- **WHEN** an archive-mode backup includes a database source and the dump succeeds
- **THEN** the generated dump file is included in the generated archive artifact

#### Scenario: Clean staged dumps
- **WHEN** a backup task with database sources finishes successfully or fails after staging files
- **THEN** the Agent removes the per-task staged database dump directory

### Requirement: Protect database credentials during execution
VaultFleet SHALL avoid exposing database passwords in command arguments, task logs, task results, and persisted metadata.

#### Scenario: Execute with redacted credentials
- **WHEN** the Agent runs a database dump command
- **THEN** the password is supplied through a safer execution mechanism such as environment variables or temporary credential files rather than clear-text command arguments where practical

#### Scenario: Redact command output
- **WHEN** a database dump command emits stdout or stderr containing a configured secret value
- **THEN** task logs redact the secret before sending logs to Master

#### Scenario: Remove temporary credential files
- **WHEN** a database dump command completes or fails
- **THEN** the Agent removes any temporary credential files created for that dump

### Requirement: Surface database backup status
VaultFleet SHALL expose non-secret database dump status and metadata in task logs and task results.

#### Scenario: Emit database dump logs
- **WHEN** a backup task prepares database dump files
- **THEN** the Agent emits task log lines under a database dump phase for start, success, failure, and cleanup events

#### Scenario: Record non-secret dump metadata
- **WHEN** a database dump succeeds
- **THEN** VaultFleet records non-secret metadata such as engine, database name or all-databases flag, execution mode, dump filename, size, and warnings

### Requirement: Configure database sources in the policy UI
The policy UI SHALL allow operators to add, edit, validate, and remove PostgreSQL and MySQL backup sources from the backup source section.

#### Scenario: Add database source
- **WHEN** an operator edits a backup policy for a capable Agent
- **THEN** the UI offers a database source type with PostgreSQL and MySQL configuration fields

#### Scenario: Configure Docker database dump
- **WHEN** an operator chooses Docker execution mode for a database source
- **THEN** the UI lets the operator select a Docker container and hides host-only command assumptions

#### Scenario: Show database backup guidance
- **WHEN** an operator configures a database source
- **THEN** the UI explains that the feature creates logical dump files and does not automatically restore databases
