## ADDED Requirements

### Requirement: Policies support typed backup sources
Backup policies SHALL support typed backup sources while preserving existing path-based backups.

#### Scenario: Policy contains manual paths
- **WHEN** a policy is created with manual directory paths
- **THEN** the system stores those paths as path backup sources and continues to expose them through `backup_dirs` for existing workflows

#### Scenario: Policy contains Docker containers
- **WHEN** a policy is created with Docker container selections
- **THEN** the system stores those selections as typed Docker container sources with identity hints and selected data categories

### Requirement: Policy validation enforces source compatibility
Master SHALL validate backup sources against the selected Agent before saving or pushing a policy.

#### Scenario: Docker source selected for unsupported Agent
- **WHEN** a policy includes Docker container sources for an Agent that does not advertise Docker workload support
- **THEN** Master rejects the policy with a clear validation error

#### Scenario: Policy has no backup sources
- **WHEN** a policy submission contains no path sources and no Docker sources
- **THEN** Master rejects the policy with a clear validation error

### Requirement: Policy push includes typed sources for capable Agents
Master SHALL include typed backup sources in policy push payloads for Agents that support them.

#### Scenario: Agent supports typed sources
- **WHEN** Master pushes a policy to an Agent that supports typed backup sources
- **THEN** the payload includes both typed backup sources and derived path sources needed by legacy executor flow

#### Scenario: Agent is legacy path-only
- **WHEN** Master pushes a path-only policy to an Agent that does not support typed backup sources
- **THEN** the payload remains compatible with the existing `backup_dirs` contract

### Requirement: Policy UI separates backup source selection from storage selection
The policy UI SHALL present backup sources independently from storage backend configuration.

#### Scenario: Operator configures Docker backup
- **WHEN** an operator edits a policy for an online Docker-capable Agent
- **THEN** the UI shows a Docker container selection control in the policy source section, not in the storage configuration page

#### Scenario: Operator configures normal directory backup
- **WHEN** an operator edits a path-only policy
- **THEN** the UI still supports manual path entry and the existing Agent directory browser

### Requirement: Existing policies remain usable after migration
Existing policies SHALL continue to run after typed backup sources are introduced.

#### Scenario: Existing path-only policy is loaded
- **WHEN** a policy created before this change is loaded
- **THEN** the system derives path backup sources from `backup_dirs` and displays the same directories in the UI

#### Scenario: Existing path-only policy is pushed
- **WHEN** an existing policy is pushed to an Agent
- **THEN** the backup command uses the same directories, excludes, retention, repository path, password behavior, timeout, and rclone arguments as before
