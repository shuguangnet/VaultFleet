## MODIFIED Requirements

### Requirement: Docker Backup Scope Guidance
VaultFleet SHALL present Docker workload backup as host-level data protection for container-mounted data, deployment files, and VaultFleet-generated Docker restore metadata, not as image-layer or container-instance capture.

#### Scenario: Explain recommended Docker backup scope
- **WHEN** an operator configures or reviews a backup policy for a Docker-hosted workload
- **THEN** VaultFleet identifies mounted data directories, available Docker volume mountpoints, deployment files, and generated Docker manifest files as the recommended backup scope

#### Scenario: Exclude unsupported Docker backup modes
- **WHEN** an operator reads product guidance for Docker workload backup
- **THEN** VaultFleet states that image-layer backup, `docker commit`, `docker save`, and automatic full-fidelity container reconstruction are not supported by this feature

### Requirement: Docker-Focused Policy Guidance
VaultFleet SHALL provide Docker-focused field guidance and examples in the policy workflow for mounted volumes, bind mounts, compose files, consistency hooks, and one-click Docker profile generation.

#### Scenario: Show Docker-specific examples in policy workflow
- **WHEN** an operator opens the backup policy form
- **THEN** VaultFleet provides examples that reference mounted data paths, compose manifests, optional export commands for Docker workloads, and the one-click Docker backup workflow
