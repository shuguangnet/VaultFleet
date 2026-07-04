## Context

VaultFleet policies currently store `backup_dirs` as a JSON array of host paths. The Web UI exposes those paths as a textarea plus an Agent-backed directory browser. Master pushes the paths through `PolicyPushPayload.BackupDirs`, and the Agent passes them directly to the existing restic/plain rclone executor.

This path-only model works for normal directories, but it is a poor fit for Docker workloads. Operators must know which bind mounts and volumes belong to each container, avoid backing up Docker engine internals blindly, and remember related compose files. Storage configuration also has a `container` field for some rclone backends, but that means object-storage container/bucket, not Docker container.

## Goals / Non-Goals

**Goals:**

- Make Docker containers visible and selectable from backup policy creation/editing.
- Preserve existing directory backup behavior and existing restic/rclone execution paths.
- Resolve Docker selections on the Agent, where Docker socket access and mount paths exist.
- Back up persistent Docker data paths: bind mounts, named volume mountpoints, anonymous volume mountpoints, and discoverable compose project files.
- Report clear warnings and errors for unavailable Docker, missing socket permissions, disappeared containers, unreadable mounts, and empty selections.
- Keep Master as control plane only; backup data still moves directly from Agent to storage.

**Non-Goals:**

- Backing up Docker images, container writable layers, networks, secrets, or the whole Docker data root by default.
- Quiescing databases or providing application-specific pre/post hooks in this change.
- Restoring Docker containers automatically end-to-end.
- Supporting remote Docker daemons from Master.
- Replacing manual path backups.

## Decisions

1. Add typed backup sources and keep `backup_dirs` for compatibility.
   - Rationale: Docker is a workload source, not a storage backend. A typed `backup_sources` field allows `path` and `docker_container` inputs to coexist while old policies and UI flows keep working.
   - Alternative considered: encode Docker selections as generated paths in `backup_dirs` at save time. That would become stale when containers are recreated or volumes move and would hide why paths were selected.

2. Discover and resolve Docker on the Agent.
   - Rationale: the Agent is the only component that can safely inspect local containers and verify filesystem readability. Master should not need Docker access.
   - Alternative considered: have users paste Docker paths manually. That is the current failure mode and does not solve discoverability or validation.

3. Use Docker Engine API over the local Unix socket through a small internal client.
   - Rationale: the Agent can query `/containers/json`, `/containers/{id}/json`, and `/volumes/{name}` without requiring the Docker CLI or adding a large SDK dependency. It also works in minimal installations where only the daemon socket is mounted.
   - Alternative considered: shell out to `docker inspect`. That is easy to prototype but adds CLI dependency, parsing fragility, and poorer timeout/error control.

4. Select containers by stable identity hints, then re-resolve at run time.
   - Rationale: container IDs can change when compose recreates services. A selection should store `container_id`, `name`, and compose labels (`project`, `service`, `working_dir`) when available. At execution, the Agent first tries exact ID, then matching compose identity, then name.
   - Alternative considered: store only current mount paths. That is simpler but misses recreated containers and removes Docker-aware warnings.

5. Resolve only persistent mount targets by default.
   - Rationale: backing up `/var/lib/docker` or overlay layers is unsafe and implementation-specific. Mounts are the durable state Docker exposes. Compose config files are included when labels expose `com.docker.compose.project.config_files` and `com.docker.compose.project.working_dir`.
   - Alternative considered: include container root filesystems. That produces inconsistent backups and duplicates image-managed content.

6. Gate Docker features through Agent capabilities.
   - Rationale: existing protocol already reports capabilities. Master can hide or disable Docker selection for older Agents and reject Docker-source policies if the selected Agent does not advertise Docker support.
   - Alternative considered: send Docker policy fields to every Agent and let old Agents ignore them. That would create false success with missing Docker data.

## Risks / Trade-offs

- Docker socket access is privileged. Mitigation: document required deployment options, show capability/permission status in the UI, and keep access local to the Agent.
- Live container data can be inconsistent. Mitigation: make the backup scope explicit and document that application-consistent backups still require app-level dump or quiesce workflows outside this change.
- Container identity can be ambiguous after recreation. Mitigation: store multiple identity hints and surface ambiguous matches as policy validation errors instead of guessing.
- Named volume mountpoints may live under Docker's data root. Mitigation: resolve volume mountpoints through Docker metadata and include only selected volume paths, never the whole data root.
- Older Agents cannot run Docker-source policies. Mitigation: Master rejects such policies for Agents lacking the Docker capability and continues sending legacy `backup_dirs` to older Agents.

## Migration Plan

1. Add nullable typed-source storage alongside existing `backup_dirs`.
2. Backfill existing policies as `path` sources derived from `backup_dirs` or continue deriving `backup_dirs` from path sources during serialization.
3. Add protocol fields with backward-compatible JSON tags and capability checks.
4. Roll out Agent Docker discovery first; UI enables Docker controls only when an online Agent reports support.
5. If rollback is needed, ignore `backup_sources` and continue using `backup_dirs`; Docker-source-only policies will require manual path conversion before use on older versions.

## Open Questions

- Should Docker-source policies fail when a selected container is stopped, or should stopped containers still resolve their mounts and back up data?
- Should compose config files be selected by default or shown as a separate checkbox per compose project?
- Should task results store Docker metadata in the existing `TaskHistory` row, snapshot metadata, or a new table keyed by snapshot ID?
