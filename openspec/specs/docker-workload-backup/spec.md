# Docker Workload Backup

## Purpose

Define how VaultFleet supports Docker-hosted workloads through host-level backup policies, optional consistency hooks, and explicit scope boundaries.

## Requirements

### Requirement: Docker Backup Scope Guidance

VaultFleet SHALL present Docker workload backup as host-level data protection for container-mounted data and deployment files, not as image-layer or container-instance capture.

#### Scenario: Explain recommended Docker backup scope

- **WHEN** an operator configures or reviews a backup policy for a Docker-hosted workload
- **THEN** VaultFleet identifies mounted data directories and deployment files as the recommended backup scope

#### Scenario: Exclude unsupported Docker backup modes

- **WHEN** an operator reads product guidance for Docker workload backup
- **THEN** VaultFleet states that image-layer backup and automatic container reconstruction are not supported by this feature

### Requirement: Optional Backup Hooks

VaultFleet SHALL allow a backup policy to define optional pre-backup and post-backup commands executed on the agent host.

#### Scenario: Run pre-backup hook before data collection

- **WHEN** a backup policy defines a pre-backup hook and a backup job starts
- **THEN** the agent executes the pre-backup hook before invoking the configured backup runner

#### Scenario: Run post-backup hook after successful backup

- **WHEN** a backup policy defines a post-backup hook and the backup data collection succeeds
- **THEN** the agent executes the post-backup hook after the backup runner finishes

### Requirement: Hook Failure Handling

VaultFleet SHALL treat hook execution as part of the backup job outcome and expose failures in task history.

#### Scenario: Fail backup when pre-backup hook fails

- **WHEN** a pre-backup hook exits non-zero, times out, or cannot be started
- **THEN** VaultFleet marks the backup task as failed and does not start the backup runner

#### Scenario: Fail backup when post-backup hook fails

- **WHEN** a post-backup hook exits non-zero, times out, or cannot be started after backup data collection
- **THEN** VaultFleet marks the backup task as failed and records the hook failure in task results

### Requirement: Controlled Hook Configuration

VaultFleet SHALL validate hook configuration before accepting a policy so operators cannot save an empty or structurally invalid hook definition.

#### Scenario: Reject empty hook command

- **WHEN** an operator submits a policy with an enabled hook but no command content
- **THEN** VaultFleet rejects the policy update with a validation error

#### Scenario: Preserve policy without hooks

- **WHEN** an operator creates or updates a policy without any backup hooks
- **THEN** VaultFleet saves the policy and executes backups without any hook steps

### Requirement: Docker-Focused Policy Guidance

VaultFleet SHALL provide Docker-focused field guidance and examples in the policy workflow for mounted volumes, bind mounts, compose files, and consistency hooks.

#### Scenario: Show Docker-specific examples in policy workflow

- **WHEN** an operator opens the backup policy form
- **THEN** VaultFleet provides examples that reference mounted data paths, compose manifests, and optional export commands for Docker workloads
