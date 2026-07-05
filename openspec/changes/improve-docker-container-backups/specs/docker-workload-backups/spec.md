## ADDED Requirements

### Requirement: Agent reports Docker backup capability
The Agent SHALL report whether Docker workload backup discovery is available through its heartbeat capabilities and discovery responses.

#### Scenario: Docker socket is usable
- **WHEN** the Agent can connect to the local Docker Engine API and inspect containers
- **THEN** its reported capabilities include `docker_workload_backups`

#### Scenario: Docker socket is unavailable
- **WHEN** Docker is not installed, the socket is missing, or the Agent lacks permission to access it
- **THEN** Docker discovery returns a structured unavailable error and the Agent does not advertise `docker_workload_backups`

### Requirement: Master exposes Docker container discovery
The system SHALL allow the Web UI to request Docker workload discovery for a specific online Agent.

#### Scenario: Agent supports Docker discovery
- **WHEN** the UI requests Docker workloads for an online Agent with `docker_workload_backups`
- **THEN** Master sends a Docker discovery request to that Agent and returns the Agent response to the UI

#### Scenario: Agent does not support Docker discovery
- **WHEN** the UI requests Docker workloads for an Agent that is offline or lacks `docker_workload_backups`
- **THEN** Master rejects the request with a clear error and does not create a pending backup command

### Requirement: Docker discovery returns selectable workload metadata
Docker discovery SHALL return enough metadata for an operator to identify containers and understand what data would be protected.

#### Scenario: Containers are discovered
- **WHEN** the Agent inspects local Docker containers
- **THEN** the response includes container ID, names, image, state, labels, compose project/service hints, mounts, mount source paths or volume names, mount destinations, read/write mode, and warnings

#### Scenario: Container has no persistent mounts
- **WHEN** a discovered container has no bind mounts, named volumes, anonymous volumes, or discoverable compose files
- **THEN** the response marks the container as selectable with a warning or not selectable with a reason, according to policy validation rules

### Requirement: Selected Docker containers resolve to backup paths
The Agent SHALL resolve selected Docker container sources into concrete filesystem paths immediately before a backup runs.

#### Scenario: Selected container still exists
- **WHEN** a backup policy includes a Docker container source and the selected container can be matched by ID, compose identity, or name
- **THEN** the Agent resolves bind mount paths, named volume mountpoints, anonymous volume mountpoints, and selected compose files into backup targets

#### Scenario: Selected container disappeared
- **WHEN** a backup policy includes a Docker container source that cannot be matched at execution time
- **THEN** the backup fails before restic/rclone execution with an error naming the missing container selection

#### Scenario: Selected mount is unreadable
- **WHEN** a selected container resolves to a mount path that the Agent cannot stat or read
- **THEN** the backup fails before restic/rclone execution with an error naming the unreadable path

### Requirement: Docker backups preserve Docker-aware metadata
Docker workload backups SHALL record Docker-aware source metadata in task or snapshot metadata without putting backup data through Master.

#### Scenario: Docker-source backup succeeds
- **WHEN** a backup containing Docker container sources completes successfully
- **THEN** the task or snapshot metadata records selected containers, resolved paths, images, compose hints, and warnings visible to the operator

#### Scenario: Backup data is uploaded
- **WHEN** the Agent backs up Docker workload paths
- **THEN** data is still written directly from Agent to the configured storage through the existing restic or plain rclone executor

### Requirement: Docker backup scope excludes unsafe engine internals by default
Docker workload backup SHALL protect persistent workload data but SHALL NOT back up Docker engine internals as a container selection side effect.

#### Scenario: Container is selected
- **WHEN** the Agent resolves a selected Docker container
- **THEN** it includes only discovered persistent mounts and selected compose files, not overlay layers, image layers, Docker networks, or the whole Docker data root
