## Context

VaultFleet's policy model currently binds each `backup_policies` row to exactly one Agent through `agent_id`. This keeps Agent scheduling, policy push, task history, snapshots, and restore behavior simple and predictable. Operators managing many nodes still need a grouping layer and a way to roll out the same policy shape to several nodes without manually creating each policy.

## Goals / Non-Goals

**Goals:**
- Add lightweight node tags that can be edited from the Master UI and used for filtering.
- Support bulk policy assignment to selected nodes and/or tag-matched nodes.
- Return per-node results so partial failures are visible.
- Keep existing Agent protocol and per-node policy execution semantics.
- Reuse existing RBAC permissions and audit middleware patterns.

**Non-Goals:**
- Do not introduce central policy groups that dynamically apply to future nodes.
- Do not change Agent-side scheduling, storage access, or restic/rclone behavior.
- Do not add OpenStack cloud inventory discovery in this change.
- Do not implement policy template presets; that remains a separate feature.

## Decisions

1. Store tags on `agents` as a JSON string column.

   Rationale: VaultFleet already stores compact structured fields such as backup sources, retention, verification, and hooks as JSON strings in SQLite. A JSON array keeps migration and response shaping simple. The expected tag count per node is small, so filtering in Go after loading agents is acceptable for the current fleet size.

   Alternative considered: a normalized `agent_tags` table. It improves queryability for very large fleets but adds join complexity and migration surface that is not needed for a first practical implementation.

2. Keep one physical `backup_policies` row per target Agent.

   Rationale: Current policy sync finds unsynced policies by Agent, pushes a single policy payload, and records history by `policy_id`. Cloning a policy request into per-node policies preserves all existing execution behavior and makes rollback straightforward.

   Alternative considered: a policy group table plus dynamic expansion at execution time. That would require deeper scheduler and UI changes and make task attribution less direct.

3. Bulk assignment clones from an existing source policy.

   Rationale: Operators usually want to roll out a known-good policy shape to more nodes. Cloning an existing policy reuses already validated storage, hooks, retention, backup source, rclone, timeout, and verification settings instead of duplicating complex policy validation in a second endpoint.

   Alternative considered: accepting a full policy payload in the bulk endpoint. That is flexible, but it creates a second policy creation path with higher risk of validation drift.

4. Repo path defaults remain per-node.

   Rationale: Each cloned policy uses `vaultfleet/<target-agent-id>` by default, even if the source policy has its own repository path. This avoids accidental multi-node writes into the same restic repository path. A future enhancement can add explicit repo path templates if needed.

## Risks / Trade-offs

- Duplicate policies on the same node → The bulk API will not try to infer semantic duplicates. Operators can intentionally create multiple policies per node; the UI should show created results clearly.
- Partial success confusion → The API returns a summary and per-target results including errors, and successful policies are normal policy rows.
- Tag drift → Tags are manual metadata, not cloud inventory truth. Documentation should position tags as operator-owned grouping labels.
- JSON tag filtering limits → For very large fleets, server-side JSON queries or a normalized table may be needed later. The current approach optimizes for small-to-medium deployments and implementation safety.

## Migration Plan

- Add a nullable `tags` column to `agents` through GORM migration/backfill helper.
- Existing nodes get an empty tag list.
- Rollback to older binaries ignores the extra column; policies created by the bulk API remain ordinary policies and continue to work.
