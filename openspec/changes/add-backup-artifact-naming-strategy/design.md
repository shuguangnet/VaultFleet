## Context

VaultFleet now supports path, Docker, database, archive, restic snapshot, live logs, recoverability checks, restore workflows, and backup content manifests. Archive-mode backups still produce generic artifacts unless a future template feature is implemented, and snapshot-mode backups remain readable mostly through VaultFleet metadata rather than object storage paths. Users need a consistent way to label "what this backup represents" across archive filenames, remote directories, task history, and `VAULTFLEET-MANIFEST.json`.

An older active change, `add-archive-output-templates`, covers configurable archive folders and names. This change supersedes that narrower idea by adding a workload/context naming model and linking naming to source inference, manifest metadata, and task history display.

## Goals / Non-Goals

**Goals:**
- Add a user-facing context/workload name to backup policies.
- Let archive-mode policies configure remote directory and filename templates.
- Provide safe, readable defaults for new archive naming configuration.
- Preview rendered archive paths before saving a policy.
- Render archive names at backup runtime using actual Agent/source context.
- Record rendered artifact naming metadata in task history and manifest JSON.
- Improve snapshot task/history/manifest identification without changing restic repository layout.
- Keep old policies compatible and downloadable.

**Non-Goals:**
- Implement a full template language with conditionals, loops, functions, or user code.
- Change restic repository paths, pack layout, snapshot IDs, or restore semantics.
- Encrypt archive files.
- Guarantee globally unique object keys when operators intentionally remove time-varying tokens.
- Automatically rename old artifacts or migrate old task history paths.
- Implement automatic database restore or application-aware consistency.

## Decisions

### 1. Use policy-level naming fields

Add optional policy fields:
- `artifact_context_name`
- `archive_remote_dir_template`
- `archive_name_template`

`artifact_context_name` is the primary user-facing label for a site, application, database, or workload. `site_name` remains an alias in templates because many users think in terms of websites.

Rationale: Artifact naming is policy behavior. Storing it on policies supports per-workload naming, bulk policy cloning, previews, and Agent payload propagation.

Alternative considered: Store naming on the storage config. That would force all policies using the same storage target to share one layout and would not distinguish multiple sites on one node.

### 2. Keep archive templates simple

Support only `{{variable}}` tokens. Unknown variables fail validation. No expressions, defaults, filters, or conditionals.

Supported variables:
- `{{date}}` as `YYYY-MM-DD`
- `{{time}}` as `HHmmss`
- `{{datetime}}` as `YYYYMMDD-HHmmss`
- `{{agent_id}}`
- `{{agent_name}}`
- `{{policy_id}}`
- `{{policy_name}}`
- `{{context_name}}`
- `{{site_name}}` as an alias for `context_name`
- `{{source_type}}` as `path`, `docker`, `database`, or `mixed`
- `{{container_name}}`
- `{{compose_project}}`
- `{{compose_service}}`
- `{{database_engine}}`
- `{{database_name}}`
- `{{format}}`
- `{{ext}}`

Rationale: A constrained renderer is easy to validate on Master, reproduce on Agent, document, and test. It keeps template output deterministic and avoids exposing system state.

Alternative considered: Use Go `text/template`. It is more flexible but creates a larger security and UX surface, especially with escaping, missing values, and future function support.

### 3. Use conservative defaults and preserve legacy behavior

For policies that have no naming fields at all, preserve the current archive behavior:
- remote directory: `artifacts`
- archive name: `backup-{{datetime}}.{{ext}}`

For new policies or policies where the operator enables custom naming, suggest:
- remote directory: `archives/{{agent_name}}/{{context_name}}/{{date}}`
- archive name: `{{context_name}}_{{agent_name}}_{{datetime}}.{{ext}}`

Rationale: Existing operators should not see their artifact layout change unexpectedly. New configuration can guide users toward readable names.

Alternative considered: Change defaults globally. That improves readability but risks surprising existing automation that watches `artifacts/backup-*`.

### 4. Infer context names but keep user override authoritative

When `artifact_context_name` is empty, derive a suggestion from sources:
- Docker Compose: `compose_project`
- single Docker container: `container_name`
- database source: `database_engine-database_name` or `database_engine-all-databases`
- single path source: final path segment
- mixed sources: policy name

The UI can show this as a suggestion. The persisted `artifact_context_name` remains an operator-controlled value when explicitly provided.

Rationale: Auto-suggestions reduce setup friction, but inferred names can be wrong for bind mounts, cloned policies, or multi-source applications. Operators need final control.

Alternative considered: Always infer and never store context names. That avoids one field but makes output unstable when source metadata changes.

### 5. Render final archive paths on the Agent

Master validates and previews templates using available policy/agent/source data. Agent renders the final archive path at runtime using actual time and resolved source metadata, then reports the rendered metadata back in the task result.

Rationale: Agent owns archive creation and upload. Runtime rendering avoids queue-time timestamp mismatch and can use resolved Docker/database metadata.

Alternative considered: Render entirely on Master before dispatch. Preview would match dispatch time more closely, but queued jobs can run later and source metadata can change.

### 6. Validate path safety at both save/preview and runtime

Validation MUST reject unsafe literal templates and rendered paths:
- absolute paths
- `.` or `..` segments
- empty remote directory when a custom directory is set
- empty filename
- path separators in filenames
- control characters
- unknown variables

Variable values are sanitized by replacing unsafe characters with `_`. Literal template path traversal remains an error.

Rationale: Storage backends vary. Some object keys map to filesystem-like paths, so VaultFleet should enforce backend-independent safety.

Alternative considered: Let rclone normalize paths. That makes behavior backend-specific and harder to audit.

### 7. Treat non-unique templates as warnings first

If the archive filename template lacks `{{datetime}}`, `{{date}}`, or `{{time}}`, preview returns a warning that future backups may collide or overwrite depending on backend behavior. Do not block saving unless the rendered path is unsafe.

Rationale: Some operators intentionally want stable object keys for "latest" artifacts. They should be warned, not blocked.

Alternative considered: Require a timestamp token. That is safer but prevents legitimate "latest backup" workflows.

### 8. Put naming data into manifest and task history

Extend manifest/task result metadata with:
- context name
- source type
- rendered archive directory
- rendered archive filename
- rendered relative artifact path
- template strings used when applicable
- warnings

Snapshot backups do not get archive paths, but they still get context name/source type in manifest and task history.

Rationale: Filenames help outside VaultFleet; manifest and task history provide the same identity inside VaultFleet and for restic snapshots.

Alternative considered: Only change archive file paths. That leaves snapshot users and task-history users with the same ambiguity.

## Risks / Trade-offs

- [Risk] Existing `add-archive-output-templates` overlaps this change. -> Mitigation: treat this change as the superseding plan and avoid implementing both independently.
- [Risk] Rendered preview can differ from runtime due to timestamp or source changes. -> Mitigation: label preview as an example and keep Agent-reported task metadata as the source of truth.
- [Risk] Auto-inferred context names may be misleading for mixed sources. -> Mitigation: show suggestions and warnings, but let operators override explicitly.
- [Risk] Operators may create colliding names. -> Mitigation: warn when time-varying tokens are missing and persist exact rendered paths for audit/download.
- [Risk] Template variables can produce long names. -> Mitigation: sanitize values and document practical limits; storage backends will still enforce hard limits.
- [Risk] Bulk-assigned policies may inherit the wrong context name. -> Mitigation: keep context editable and include agent/policy variables in recommended templates.

## Migration Plan

1. Add nullable policy fields and default API response values without rewriting old rows.
2. Preserve current archive output behavior when naming fields are empty.
3. Add rendering metadata to new task results and manifests; old task history remains readable with missing naming metadata.
4. Update archive download/fetch logic to use stored rendered artifact paths, including old default paths.
5. Rollback by hiding UI fields and ignoring naming fields in Agent payloads; existing policies fall back to old behavior if fields are absent.

## Open Questions

- Should the policy UI store an explicit "use readable defaults" flag, or should custom naming be considered active when any naming field is present?
- Should the first implementation include preset template buttons for Docker/database/path, or only provide defaults plus manual editing?
- Should non-unique template warnings be shown only in preview, or also in policy list/task history?
