## ADDED Requirements

### Requirement: Generate backup content manifest
VaultFleet SHALL generate a non-secret `VAULTFLEET-MANIFEST.json` for each successful backup run.

#### Scenario: Generate manifest for backup
- **WHEN** a backup task resolves its effective backup sources
- **THEN** the Agent generates a manifest with schema version, generation timestamp, backup mode, Agent identity, policy identity when available, source summaries, exclude patterns, and warnings

#### Scenario: Exclude secrets from manifest
- **WHEN** a manifest is generated for storage, Docker, database, hook, or rclone-backed backup configuration
- **THEN** the manifest omits passwords, tokens, storage credentials, database passwords, Docker environment values, and hook command output

#### Scenario: Use stable manifest filename
- **WHEN** VaultFleet writes the generated manifest into backup content
- **THEN** the file is named `VAULTFLEET-MANIFEST.json`

### Requirement: Describe backed-up sources
VaultFleet SHALL record enough source summary information in the manifest for an operator to identify what workload was backed up.

#### Scenario: Describe path sources
- **WHEN** a backup includes filesystem path sources
- **THEN** the manifest lists the effective source paths and marks them as path sources

#### Scenario: Describe Docker sources
- **WHEN** a backup includes Docker container sources
- **THEN** the manifest lists non-secret Docker source identity including container name or ID, image, Compose project/service when known, selected mounts, and included Compose config files

#### Scenario: Describe database dump sources
- **WHEN** a backup includes database dump sources
- **THEN** the manifest lists database engine, execution mode, database names or all-database mode, dump output names, compression state, and container name when applicable

#### Scenario: Describe excludes
- **WHEN** a backup policy has exclude patterns
- **THEN** the manifest records the effective exclude patterns used by the backup runner

### Requirement: Include manifest in archive backups
VaultFleet SHALL include `VAULTFLEET-MANIFEST.json` at the root of generated archive backup artifacts.

#### Scenario: Include manifest in tar.gz archive
- **WHEN** an archive-mode backup uses `tar.gz`
- **THEN** the generated archive contains a root-level `VAULTFLEET-MANIFEST.json`

#### Scenario: Include manifest in zip archive
- **WHEN** an archive-mode backup uses `zip`
- **THEN** the generated archive contains a root-level `VAULTFLEET-MANIFEST.json`

#### Scenario: Record archive artifact details
- **WHEN** an archive backup succeeds
- **THEN** the manifest records archive artifact name, relative artifact path, content type, archive format, and size when available

### Requirement: Include manifest in snapshot backups
VaultFleet SHALL include `VAULTFLEET-MANIFEST.json` in restic snapshot backup content.

#### Scenario: Stage manifest for snapshot backup
- **WHEN** a snapshot-mode backup runs
- **THEN** the Agent writes the manifest to an Agent-managed staging path and includes that path in the restic backup input list

#### Scenario: Clean manifest staging
- **WHEN** a backup task finishes or fails after manifest staging
- **THEN** the Agent removes temporary manifest staging files after the backup flow completes

### Requirement: Persist manifest in task history
VaultFleet SHALL persist backup manifest JSON in Master task history when an Agent returns it in the task result.

#### Scenario: Persist returned manifest
- **WHEN** Master processes a successful backup task result containing a manifest
- **THEN** Master stores the manifest JSON on the corresponding task history record

#### Scenario: Return manifest through task API
- **WHEN** an operator requests task history
- **THEN** the task response includes the manifest summary for tasks that have one

#### Scenario: Tolerate missing manifest
- **WHEN** a task result comes from an older Agent or an older backup without manifest data
- **THEN** Master stores and returns the task without failing manifest parsing

### Requirement: Show manifest summary in task history UI
The task history UI SHALL show a concise backup-content summary when manifest data is available.

#### Scenario: Show source summary
- **WHEN** an operator expands a backup task with manifest data
- **THEN** the UI shows backed-up path sources, Docker sources, database dumps, exclude patterns, and warnings from the manifest

#### Scenario: Show artifact summary
- **WHEN** an operator expands an archive backup task with manifest artifact data
- **THEN** the UI shows archive artifact name, path, format, size, and content type from the manifest or task history

#### Scenario: Handle missing manifest in UI
- **WHEN** an operator views a backup task without manifest data
- **THEN** the UI does not break and may fall back to existing task, Docker, database, and artifact fields

### Requirement: Preserve manifest extensibility
VaultFleet SHALL version the manifest schema and keep P1/P2 manifest fields optional.

#### Scenario: Accept optional context fields
- **WHEN** future policy context fields such as `context_name` or `site_name` are available
- **THEN** the manifest schema can include them without breaking existing manifest consumers

#### Scenario: Accept optional integrity fields
- **WHEN** future checksum, file count, or total size summaries are available
- **THEN** the manifest schema can include them without changing existing required P0 fields
