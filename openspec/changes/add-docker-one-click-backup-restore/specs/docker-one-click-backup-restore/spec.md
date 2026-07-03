## ADDED Requirements

### Requirement: Docker Discovery
VaultFleet SHALL allow an operator to discover Docker containers on an online agent and return container metadata needed for backup selection.

#### Scenario: Discover running containers
- **WHEN** an operator requests Docker discovery for an online agent
- **THEN** VaultFleet returns discovered containers with names, IDs, image names, status, mount paths, named volumes, ports, labels, and compose project metadata when available

#### Scenario: Docker unavailable
- **WHEN** Docker is not installed or the agent cannot access the Docker daemon
- **THEN** VaultFleet returns a clear discovery error without creating or modifying backup policies

### Requirement: Docker Backup Profile Creation
VaultFleet SHALL create a standard backup policy from selected Docker containers and discovered host-visible data paths.

#### Scenario: Create policy from selected containers
- **WHEN** an operator selects one or more discovered containers and confirms one-click Docker backup
- **THEN** VaultFleet creates or updates a backup policy whose backup paths include selected bind mounts, available volume mountpoints, compose/deployment files, and a VaultFleet Docker metadata directory

#### Scenario: Trigger immediate backup
- **WHEN** an operator enables immediate backup during Docker profile creation
- **THEN** VaultFleet pushes the policy to the agent and sends a backup-now command using the generated policy

### Requirement: Docker Backup Manifest
VaultFleet SHALL capture Docker restore metadata as files included in the backup snapshot.

#### Scenario: Generate manifest before backup
- **WHEN** a Docker-generated policy backup starts
- **THEN** the agent writes a manifest containing selected containers, images, mounts, volume metadata, compose metadata, generated restore guidance, and capture time into the policy metadata directory before data collection

#### Scenario: Redact sensitive response fields
- **WHEN** Docker discovery or profile preview returns environment or label-like values to the UI
- **THEN** VaultFleet redacts values whose keys indicate passwords, tokens, secrets, or keys

### Requirement: Docker Restore Precheck
VaultFleet SHALL perform a Docker restore precheck before executing any container startup command.

#### Scenario: Restore precheck only
- **WHEN** an operator requests Docker restore dry-run for a snapshot
- **THEN** VaultFleet restores or reads the Docker manifest in a staging context and reports Docker availability, existing container name conflicts, port conflicts, missing compose tooling, and planned commands without starting containers

#### Scenario: Missing manifest
- **WHEN** an operator requests Docker restore for a snapshot without a Docker manifest
- **THEN** VaultFleet rejects the Docker restore request with an explanation that the snapshot is not a Docker one-click backup

### Requirement: Guarded Docker Restore Execution
VaultFleet SHALL require explicit operator confirmation before running Docker startup commands during restore.

#### Scenario: Restore files without starting containers
- **WHEN** an operator submits Docker restore with startup disabled
- **THEN** VaultFleet restores files to the target path and records the planned Docker startup command without executing it

#### Scenario: Restore and start containers
- **WHEN** an operator submits Docker restore with startup enabled and precheck passes
- **THEN** VaultFleet restores files and executes the generated compose or container startup command, recording command output in task history

#### Scenario: Startup blocked by precheck
- **WHEN** startup is enabled but precheck detects a blocking conflict
- **THEN** VaultFleet fails the restore task before executing any Docker startup command and records the blocking reason

### Requirement: Docker UI Workflow
VaultFleet SHALL provide UI actions for one-click Docker backup and guided Docker restore.

#### Scenario: Start one-click Docker backup from node
- **WHEN** an operator opens an eligible node
- **THEN** VaultFleet provides a Docker backup action that discovers containers, lets the operator choose scope, and creates the policy with an optional immediate backup

#### Scenario: Start Docker restore from snapshot
- **WHEN** an operator opens a snapshot that contains Docker backup metadata
- **THEN** VaultFleet provides a Docker restore action that displays precheck results and requires confirmation before starting containers
