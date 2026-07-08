## Context

VaultFleet already supports two backup modes: snapshot backups through restic and archive backups through local `zip`/`tar.gz` creation followed by rclone upload. Archive backups currently write files named `backup-<UTC timestamp>.<ext>` under a fixed `artifacts/` path inside the configured storage repository. Policy configuration already carries archive format, backup sources, Docker source metadata, agent identity, storage repository path, and task history artifact fields.

Operators using archive mode for websites, Docker Compose applications, and plain directories need output paths that are meaningful outside VaultFleet, for example `archives/node-a/2026-07-08/site-a-015124.zip`. The feature crosses policy persistence, protocol payloads, Agent archive execution, task history, download lookup, and the policy UI.

## Goals / Non-Goals

**Goals:**
- Let archive policies configure a remote directory template and filename template.
- Provide a deterministic preview in the policy UI before saving.
- Preserve the existing `artifacts/backup-<datetime>.<ext>` behavior when templates are empty.
- Render path variables from safe, well-defined policy, agent, time, archive format, and source metadata.
- Prevent rendered paths from escaping the repository prefix or producing invalid object keys.
- Record and expose the rendered artifact name/path exactly as uploaded.

**Non-Goals:**
- Add encrypted archive files.
- Add archive recoverability verification.
- Change snapshot/restic repository layout.
- Change restore semantics for existing archive artifacts.
- Add a full template language with conditionals, loops, or arbitrary functions.

## Decisions

### 1. Store simple template strings on the policy

Add optional policy fields:
- `archive_remote_dir_template`
- `archive_name_template`
- `archive_context_name`

The first two control output layout. `archive_context_name` is an operator-facing label for a website, app, or workload and supplies the `{{context_name}}` / `{{site_name}}` variable.

Rationale: Existing policy settings such as backup sources, hooks, verification, rclone args, and timeout live on the policy row and are pushed to the Agent. Archive output rules are policy behavior, not storage configuration. Keeping them on the policy also makes bulk policy cloning and per-node overrides straightforward.

Alternative considered: Put archive folders on the storage config. That would force one layout for every policy using the storage target and does not solve per-site or per-container naming.

### 2. Use a limited token renderer instead of Go templates

Support `{{name}}` tokens only. Unknown tokens fail validation. Supported initial variables:
- `{{date}}` as `YYYY-MM-DD`
- `{{time}}` as `HHmmss`
- `{{datetime}}` as `YYYYMMDD-HHmmss`
- `{{agent_id}}`
- `{{agent_name}}`
- `{{policy_id}}`
- `{{policy_name}}`
- `{{context_name}}`
- `{{site_name}}` as an alias for `context_name`
- `{{container_name}}`
- `{{compose_project}}`
- `{{compose_service}}`
- `{{format}}` as `zip` or `tar.gz`
- `{{ext}}` as `zip` or `tar.gz`

Rationale: A constrained renderer is easier to validate, preview, test, and keep safe across Master and Agent. It avoids exposing filesystem or environment functions and avoids divergent behavior from template escaping rules.

Alternative considered: Use `text/template`. That is more flexible, but increases validation complexity and makes it easier to create surprising path output.

### 3. Render the final remote path on the Agent using command payload context

Master validates and previews templates with available policy and agent data. Agent renders the final path at backup runtime with the actual timestamp and resolved source metadata. Master records whatever path the Agent reports in task history.

Rationale: The Agent already owns archive creation and rclone upload. Runtime rendering prevents clock mismatch between command creation and execution, and it allows Docker source resolution to provide current container/Compose metadata.

Alternative considered: Render the path on Master before dispatch. That makes preview exactly match dispatch time, but queued tasks can run later and Docker metadata is only reliably known on the Agent.

### 4. Keep defaults compatible

If `archive_remote_dir_template` is empty, use `artifacts`. If `archive_name_template` is empty, use `backup-{{datetime}}.{{ext}}`. Existing policies continue to upload to the same logical location.

Rationale: Archive download and task history already rely on stored artifact paths. Avoiding a migration keeps old task records and policies valid.

### 5. Sanitize rendered path segments

Validation and rendering MUST reject or sanitize unsafe output:
- no absolute paths
- no `.` or `..` path segments
- no empty filename
- no path separators in the filename after rendering
- no control characters
- no rendered path outside the policy repository prefix

Prefer replacing unsafe characters inside variable values with `_`, while treating unsafe literal template structure such as `../` as a validation error.

Rationale: rclone targets are object keys, but many backends map keys to filesystem paths. Treating templates as untrusted prevents accidental overwrite or traversal behavior.

Alternative considered: Let rclone/backend normalize keys. That makes safety backend-dependent and less testable.

### 6. Preview endpoint returns both rendered path and warnings

Add a policy/template preview path through the existing policy API surface. The frontend can also perform lightweight local preview for responsiveness, but server preview is authoritative. Preview returns:
- `artifact_name`
- `remote_dir`
- `remote_path`
- `variables`
- `warnings`

Rationale: Server preview catches unknown variables and safety errors using the same validation rules that policy save and Agent payload generation use. Returning variables helps the UI explain missing Docker-derived values before a container is selected or resolved.

## Risks / Trade-offs

- [Risk] Preview differs from actual backup timestamp. -> Mitigation: mark preview as an example and use the same date/time format with current server time.
- [Risk] Docker-derived variables may be unavailable before runtime. -> Mitigation: derive from selected Docker source when present; otherwise render empty/sanitized values and return preview warnings.
- [Risk] Existing archive download code assumes `artifacts/`. -> Mitigation: use the stored task history `artifact_path` as source of truth and update remote fetch logic to accept templated relative paths.
- [Risk] Operators can configure colliding filenames. -> Mitigation: document that templates should include `{{datetime}}`; optionally warn when neither date nor time token is present.
- [Risk] Bulk-assigned policies may inherit source names that are wrong on target nodes. -> Mitigation: keep `archive_context_name` editable per cloned policy and rely on agent/policy variables for node-specific defaults.

## Migration Plan

1. Add nullable policy columns or JSON-backed fields for archive output templates and context name.
2. Default missing fields in API responses and Agent payloads without rewriting existing rows.
3. Keep existing archive task records downloadable through their stored artifact paths.
4. Rollback by hiding the UI fields and ignoring new policy fields; old policies still fall back to the default archive path if fields are absent.
