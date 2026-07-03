## Why

VaultFleet already supports Docker workload guidance through directory backups and hooks, but operators still need to manually discover container mounts, compose files, and restore commands. A one-click Docker workflow reduces that operational guesswork while keeping VaultFleet's existing host-file backup model.

## What Changes

- Add agent-side Docker discovery that can inspect selected containers or all running containers and return bind mounts, named volumes, compose metadata, images, ports, labels, and generated restore guidance.
- Add a Docker backup profile API that turns discovered Docker assets into an ordinary VaultFleet backup policy and can immediately trigger a backup.
- Add Docker-aware restore orchestration that restores a snapshot to a staging path, validates the saved manifest, performs a dry-run precheck, and optionally starts a Compose project or generated container command after explicit confirmation.
- Add Web UI flows for one-click Docker backup and one-click Docker restore from node detail/snapshot contexts.
- Preserve existing non-goals: no image-layer backup in the first-party implementation, no implicit destructive overwrite, and no unaudited remote shell script execution.

## Capabilities

### New Capabilities
- `docker-one-click-backup-restore`: Covers Docker discovery, policy generation, backup triggering, manifest capture, restore precheck, and guarded restore startup.

### Modified Capabilities
- `docker-workload-backup`: Clarifies that Docker support now includes first-party discovery and guided restore orchestration while still backing up host-visible data and deployment metadata.

## Impact

- Backend API: new Docker discovery/profile/restore endpoints under agent-scoped routes.
- Protocol: new message types and payloads for Docker discovery and Docker restore requests/responses.
- Agent: Docker CLI inspection, manifest generation hook, restore precheck/start command execution, task result reporting.
- Database: optional Docker profile metadata on backup policies or policy responses if needed for UI presentation.
- Frontend: node detail or policy workflow additions, services/types, restore modal updates.
- Tests/docs: Go handler/API tests, frontend tests, README/README.en Docker workflow updates.
