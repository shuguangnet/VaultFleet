## Why

VaultFleet currently backs up Docker workloads only as manually entered filesystem paths. That makes Docker backups hard to configure correctly, hides the actual containers from the user, and can lead to incomplete or unsafe backups when volumes, bind mounts, compose metadata, or Docker engine paths are selected incorrectly.

Docker workloads should be first-class backup sources in policies, so an operator can discover containers on an Agent, select the containers to protect, and let the Agent derive the concrete backup paths and metadata consistently.

## What Changes

- Add Docker workload discovery from online Agents, including containers, image names, state, labels, compose project/service labels, bind mounts, named volumes, anonymous volumes, and relevant metadata.
- Add policy support for backup sources in addition to raw backup directories, with Docker container selections as the first supported workload source.
- Update the policy UI so Docker containers are selectable from the backup policy workflow, while still allowing manual path backups for non-Docker data.
- Have the Agent resolve selected containers into backup targets at execution time, including volume mountpoints and bind mount paths, then pass those targets to the existing restic/plain rclone executor.
- Store enough Docker metadata with snapshots/task results to make restore decisions understandable, while keeping backup data flowing directly from Agent to storage.
- Provide clear validation and failure states when Docker is unavailable, the Agent lacks Docker socket access, selected containers disappear, or selected mounts cannot be read.

## Capabilities

### New Capabilities

- `docker-workload-backups`: Discover Docker containers on Agents, select them as backup sources in policies, resolve their persistent data paths, and report Docker-aware backup metadata.
- `backup-policies`: Define typed backup sources for policies while preserving existing directory backup behavior.

### Modified Capabilities

None.

## Impact

- Agent: Docker discovery, permission checks, container/mount inspection, source resolution before backup execution, and tests with mocked Docker responses.
- Master API and protocol: new request/response messages for Docker discovery, policy payload fields for typed backup sources, compatibility handling for older Agents, and task/snapshot metadata updates.
- Database: policy schema migration for typed backup sources while keeping existing `backup_dirs` usable.
- Web UI: policy form changes to show backup source modes, Docker container selection, selected mount previews, unavailable-state messaging, and existing directory browser fallback.
- Documentation: explain Docker socket requirements, rootless Docker limitations, consistency tradeoffs, and recommended restore flow.
