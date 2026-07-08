## Why

Operators can successfully create backups today but cannot reliably tell what a restored archive, folder, or snapshot contains just by inspecting the artifact. This is especially risky for Docker, website, path, and database workloads where the backup may contain many similar files, mounts, dumps, or Compose components.

## What Changes

- Generate a `VAULTFLEET-MANIFEST.json` file for each backup run.
- Include the manifest at the root of archive backups.
- Include the manifest in restic snapshot backups through an Agent-managed staged path.
- Record path, Docker, database, exclude, policy, agent, timing, and artifact basics in the manifest without secrets.
- Persist the manifest JSON in task history after a backup completes so the UI can show a backup-content summary without downloading the artifact.
- Show a manifest summary in the task history UI, including backed-up paths, Docker containers/Compose services, database dump files, excludes, and archive artifact details.
- Plan follow-up support for policy `context_name` / `site_name`, manifest preview, manifest download/view, checksum summaries, restore recommendations, and legacy-backup compatibility messaging.

## Capabilities

### New Capabilities
- `backup-content-manifest`: Per-backup self-describing manifest generation, persistence, and UI summary.

### Modified Capabilities
- None.

## Impact

- Protocol: add manifest metadata structures to task results and backup task history responses.
- Agent backup flow: build a non-secret manifest after resolving sources and before executing the backup, stage it for snapshot/archive modes, and include it in the backed-up content.
- Agent archive executor: support adding an Agent-generated manifest file at archive root.
- Master database/API: persist manifest JSON in task history and return it through task APIs.
- Web UI: render a manifest summary in task history expanded rows.
- Documentation/tests: cover manifest schema, archive inclusion, restic inclusion, task-history persistence, UI summary, and secret redaction.
