## Why

Archive backups currently produce a fixed `backup-<timestamp>` file under a fixed `artifacts/` remote path. Operators who use archive mode for websites, Docker apps, and plain directories need predictable paths and names that can include dates, node identity, policy identity, and workload labels.

## What Changes

- Add archive-mode policy settings for a remote directory template and an archive filename template.
- Render archive output paths from a safe variable context including date/time, agent identity, policy identity, archive format, Docker source hints, and operator-provided workload/site name.
- Keep existing behavior as the default when no templates are configured.
- Add backend validation so rendered archive paths cannot escape the configured storage repository path or create empty/invalid filenames.
- Add a policy UI template preview for archive-mode backups before saving.
- Do not add archive encryption, archive verification, or new restore semantics in this change.

## Capabilities

### New Capabilities
- `archive-output-templates`: Configurable archive backup remote directories, filenames, and previews.

### Modified Capabilities
- None.

## Impact

- Protocol and policy model: new optional archive output template fields in policy requests, responses, pushed policy payloads, and stored policy rows.
- Agent executor: render archive artifact names and remote object paths during archive backups.
- Master API: validate, persist, redact/return, and preview archive output templates.
- Web UI: expose archive output template fields and a live preview in the policy drawer.
- Tests: backend validation/rendering, archive job output path behavior, policy API payload propagation, and frontend preview/form coverage.
