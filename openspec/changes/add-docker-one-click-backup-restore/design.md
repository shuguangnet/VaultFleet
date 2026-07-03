## Context

VaultFleet currently backs up host-visible paths through policies pushed from master to agent. Docker support exists as guidance and optional hooks, but the system does not discover containers, compose metadata, or restore startup commands itself. Operators must manually translate Docker state into `backup_dirs`, hook commands, and restore steps.

The referenced `docker_backup_script` shows the desired operator experience: list containers, capture inspect/compose/mount metadata, verify before restore, and avoid destructive restore unless explicitly requested. VaultFleet should implement the product-shaped subset inside its existing master/agent protocol instead of downloading or invoking an external script.

## Goals / Non-Goals

**Goals:**

- Discover Docker containers on an agent via Docker CLI and return enough metadata to create a backup profile.
- Generate a normal VaultFleet backup policy from selected containers, bind mounts, named volumes, compose files, and a generated manifest path.
- Trigger an immediate backup after profile creation when requested.
- Restore a Docker snapshot through the existing restore pipeline into a staging directory, validate manifest content, run prechecks, and optionally execute a guarded startup command.
- Surface the workflow in the web UI as one-click backup and one-click restore actions.

**Non-Goals:**

- Backing up image layers with `docker save`, committing containers, or maintaining a private image registry.
- Automatically overwriting or deleting existing containers, volumes, networks, or bind mount data.
- Reconstructing every Docker Engine setting with perfect fidelity.
- Installing third-party shell scripts or running downloaded code.

## Decisions

### 1. Use Docker CLI inspection instead of Docker SDK

Agent will call `docker` and `docker compose` where available, parse JSON from `docker ps`, `docker inspect`, and `docker volume inspect`, and capture compose labels. This avoids adding a Docker socket SDK dependency and mirrors the operational environment users already have.

Alternative: Docker SDK. It gives typed APIs but still requires Docker socket permissions, adds dependency and compatibility surface, and does not remove the need for compose-file discovery.

### 2. Store Docker backup state as files inside the backup scope

The generated policy includes a VaultFleet-managed metadata directory such as `<configDir>/docker-backups/<profile-id>`. Pre-backup hooks regenerate `manifest.json`, `restore-plan.json`, and optional compose copies there. The policy backs up both workload paths and that metadata directory.

Alternative: Store Docker metadata only in the master database. That would make snapshots less portable and would not help cross-node restore from object storage.

### 3. Keep restore as staged restore plus explicit startup

Docker restore first restores snapshot data to a user-selected staging path or target root. Agent validates manifest and performs non-destructive checks for Docker availability, missing compose command, existing container names, and port conflicts. Starting containers requires explicit `start_containers` confirmation and defaults to false.

Alternative: restore directly over original paths and start services automatically. That is faster but unsafe for production and cross-node migration.

### 4. Model one-click backup as policy generation, not a separate backup engine

The Docker workflow creates or updates a standard policy and then uses existing `backup_now`, task history, progress, retention, and storage handling. Docker-specific behavior is limited to discovery and metadata hooks.

Alternative: a dedicated Docker backup command type. This would duplicate scheduling, retention, storage, and restore listing behavior already implemented for policies.

## Risks / Trade-offs

- [Docker CLI output varies by version] -> Keep parsers focused on stable JSON fields and return partial warnings instead of failing the whole discovery where possible.
- [Named volume paths require privileged host access] -> Report volume mountpoints and include them only when present; otherwise ask the operator to mount/export explicitly.
- [Restore startup commands can affect running services] -> Default to dry-run/non-starting restore and require explicit confirmation for command execution.
- [Secrets can appear in env/compose metadata] -> Redact common secret keys in API responses while storing full manifest only on the agent backup path with local file permissions.
- [One-click may imply full fidelity] -> UI and docs state that VaultFleet restores files and deployment metadata, while final service validation remains operator responsibility.

## Migration Plan

1. Add protocol message types and agent handlers; old agents will simply lack the new capability.
2. Add master API endpoints gated by agent online status/capability where possible.
3. Add UI actions that hide or disable the flow when the agent does not advertise Docker capability.
4. Existing policies remain valid; Docker profiles are opt-in and use ordinary backup policy rows.
5. Rollback can hide the UI and ignore the new protocol messages without changing existing backup data.

## Open Questions

- Whether the first UI should live on node detail, policies, or snapshots page only. Implementation can start with node detail for backup and snapshots for restore.
- Whether compose file discovery should support custom non-standard filenames beyond labels and common names in the compose working directory.
