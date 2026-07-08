## Why

Backup artifacts are currently hard to identify outside VaultFleet because archive names and remote folders do not clearly show which node, website, Docker workload, database, or policy produced them. Operators need backup paths and filenames that are readable in object storage, WebDAV, local folders, task history, and future restore flows without opening the archive or snapshot first.

## What Changes

- Add a first-class backup artifact naming strategy for archive-mode backups, centered on a user-facing workload/context name.
- Add policy fields for artifact context name, remote directory template, and archive filename template.
- Provide safe defaults that produce readable archive paths including node name, workload name, date, and timestamp.
- Render templates from a constrained `{{variable}}` token set covering time, agent, policy, source type, Docker, database, archive format, and context/site names.
- Auto-suggest a context name from selected sources when the operator has not set one explicitly.
- Validate rendered output so paths cannot escape the repository prefix, use empty names, contain path traversal, or create unsafe object keys.
- Add a server-authoritative preview and UI controls so operators can see the final example path before saving.
- Persist rendered artifact naming metadata in task history and reflect it in `VAULTFLEET-MANIFEST.json`.
- Keep snapshot/restic repository layout unchanged, while exposing the same context name in manifest and task history for snapshot identification.
- Keep existing policies compatible by falling back to the current archive path behavior when no naming fields are set.

## Capabilities

### New Capabilities
- `backup-artifact-naming`: Configurable backup artifact context names, archive remote path/name templates, preview, rendered metadata, and manifest/task-history display.

### Modified Capabilities
- None.

## Impact

- Protocol and policy model: new optional naming fields in policy request/response types, stored policy rows, and Agent policy payloads.
- Master API: validation, persistence, template preview endpoint, source-derived context suggestions, and policy push propagation.
- Agent executor: runtime rendering of archive remote directory and filename, safe upload path construction, and task result metadata.
- Manifest generation: include naming context and rendered archive artifact metadata.
- Task history UI: show context name and rendered artifact path so users can identify backup contents quickly.
- Web UI: add naming strategy controls, preset templates, preview, warnings, and validation feedback in the policy drawer.
- Documentation/tests: cover templates, supported variables, defaults, collision warnings, legacy compatibility, and source-specific naming behavior.
