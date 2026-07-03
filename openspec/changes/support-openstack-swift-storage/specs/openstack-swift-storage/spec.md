## ADDED Requirements

### Requirement: OpenStack Swift Storage Template
The system SHALL allow operators to configure OpenStack Swift storage from the storage configuration UI without using the generic manual backend option.

#### Scenario: Operator selects OpenStack Swift
- **WHEN** an operator opens the storage type selector
- **THEN** OpenStack Swift is available as a storage type backed by rclone type `swift`

#### Scenario: Operator enters common Swift credentials
- **WHEN** an operator selects OpenStack Swift
- **THEN** the UI presents fields for authentication endpoint, user, key, tenant/project, domain, auth version, region, and container

### Requirement: Swift Container Path Handling
The system SHALL treat the Swift `container` value as a remote path segment rather than an rclone config key.

#### Scenario: Testing Swift storage with a container
- **WHEN** a Swift storage connection test includes `container` value `backups`
- **THEN** the generated temporary rclone config excludes `container`
- **AND** rclone tests `vaultfleet:backups`

#### Scenario: Sending Swift policy to Agent
- **WHEN** a backup policy uses Swift storage with `container` value `backups` and repository path `vaultfleet/node-a`
- **THEN** the policy payload sent to the Agent excludes `container` from `storage.rclone_config`
- **AND** sets `storage.repo_path` to `backups/vaultfleet/node-a`

#### Scenario: Swift storage without a container
- **WHEN** Swift storage does not include a non-empty `container`
- **THEN** connection tests and policy payloads use the repository path without adding an empty leading segment
