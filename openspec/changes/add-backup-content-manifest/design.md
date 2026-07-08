## Context

VaultFleet already stores some backup metadata in Master task history, including archive artifact fields, Docker resolved source metadata, and database dump metadata. That helps inside the UI, but the backup artifact itself is not self-describing. A downloaded archive or browsed snapshot only shows raw paths and files; operators cannot reliably tell whether it represents a website, a Docker Compose app, a single container, mounted data, database dumps, or a mix of sources.

Archive output templates are planned separately under `add-archive-output-templates`. That change improves where archives are written and how they are named, but it does not prove what is inside the backup. This change adds a per-backup manifest as durable evidence inside the backup content and in task history.

## Goals / Non-Goals

**Goals:**
- Generate a `VAULTFLEET-MANIFEST.json` for every successful backup attempt before content is uploaded.
- Include the manifest inside archive backups at the archive root.
- Include the manifest inside snapshot backups through an Agent-managed staged path.
- Persist the manifest JSON in Master task history and return it through task APIs.
- Show a concise manifest summary in the task history UI.
- Capture path, Docker, database, exclude, agent, policy, timing, and artifact basics without secrets.
- Keep the manifest schema versioned and stable enough for future restore workflows.

**Non-Goals:**
- Implement checksums, file counts, or total-size summaries in the first P0 implementation.
- Add automatic restore recommendations in the first P0 implementation.
- Add policy `context_name` / `site_name` in the first P0 implementation unless already provided by another change.
- Add a manifest preview endpoint in the first P0 implementation.
- Guarantee application-level consistency beyond existing hooks, database dumps, and source resolution.
- Store secrets, environment values, database passwords, storage credentials, or rclone configuration in the manifest.

## Decisions

### 1. Use a versioned JSON manifest with a fixed filename

The manifest file name SHALL be `VAULTFLEET-MANIFEST.json`. The manifest contains:
- schema version
- generated timestamp
- Agent identity and host summary
- policy identity and backup mode
- source summaries for paths, Docker sources, and database dumps
- exclude patterns
- archive artifact metadata when available
- warnings

Rationale: A fixed filename is easy to find manually after download or restore, and a versioned JSON shape is easy for the UI and future restore wizard to parse.

Alternative considered: Store a YAML or Markdown report. That is more readable for humans, but JSON is easier to validate, persist, and consume programmatically.

### 2. Build manifest data in the Agent backup flow

The Agent has the best runtime context: resolved Docker metadata, staged database dump names, effective backup paths, backup mode, and actual artifact metadata. The manifest should be assembled in the Agent after source resolution and database dump preparation, then attached to the task result.

Rationale: Master policy data alone may be stale or incomplete, especially for Docker mounts and database dump output files. Agent-side generation reflects what the backup run actually included.

Alternative considered: Generate the manifest only on Master from task history. That would help UI display but would not place the manifest inside the backup artifact.

### 3. Stage the manifest as another backup input for snapshot mode

For snapshot/restic backups, the Agent writes the manifest to a per-task staging directory and appends that manifest path to `BackupDirs`. The path inside restic will be the staged filesystem path unless the executor is extended to remap it.

Rationale: The existing snapshot executor accepts filesystem paths. Staging reuses that flow without changing restic command wrappers deeply.

Alternative considered: Inject manifest bytes directly into restic. That would require a different backup path abstraction and is not necessary for the first version.

### 4. Add manifest bytes to archive root

For archive backups, the executor should support extra virtual files or a manifest file path that is written as `VAULTFLEET-MANIFEST.json` at archive root. This avoids exposing the Agent staging directory structure inside the archive.

Rationale: Operators downloading an archive should find the manifest immediately at the top level, regardless of where the Agent staged it locally.

Alternative considered: Add the staged manifest path as a normal archive input. That would work mechanically but would bury the manifest under a temporary Agent path, making it hard to find.

### 5. Persist manifest JSON in task history

Master should add a nullable `manifest` JSON/text column to `task_histories`, parse the task result manifest, and return it in task list/detail responses. The UI should render a concise summary from task history rather than requiring artifact download.

Rationale: Operators usually inspect backups from VaultFleet first. Persisting manifest JSON makes backup content visible even for encrypted restic snapshots or remote archives that are expensive to download.

Alternative considered: Store only a manifest path and require downloading/browsing the backup to see details. That preserves storage but does not solve the UI visibility problem.

### 6. Keep P1/P2 as schema-compatible extensions

The P0 manifest schema should leave room for:
- `context_name` / `site_name`
- manifest preview data
- file counts, total sizes, and checksums
- restore recommendations
- legacy missing-manifest messaging

Rationale: The immediate problem is identification, but the manifest is a foundation for verification and guided restore. Schema versioning and optional fields keep later additions compatible.

## Risks / Trade-offs

- [Risk] Snapshot manifest path may appear under an Agent staging directory in restic listings. -> Mitigation: use a stable file name and task-history manifest summary for UI; consider path remapping in a future refinement.
- [Risk] Manifest may accidentally expose sensitive environment or credential data from Docker metadata. -> Mitigation: include only allowlisted non-secret fields and do not copy env values into the manifest.
- [Risk] Archive creation needs an executor interface change to inject a root-level file. -> Mitigation: add a small archive-extra-file abstraction and cover both zip and tar.gz writers with tests.
- [Risk] Manifest task-history storage can grow if future versions include file-level details. -> Mitigation: P0 stores summaries only; P2 checksum/file-count data should remain bounded.
- [Risk] Backup can fail after manifest staging but before task result persistence. -> Mitigation: normal failed-task behavior remains; manifest is primarily required for successful backups.
- [Risk] Old backups have no manifest. -> Mitigation: UI should tolerate missing manifest and display a clear "manifest unavailable for this backup" state in a later compatibility task.

## Migration Plan

1. Add a nullable `manifest` column to `task_histories`.
2. Extend protocol task result payloads with optional manifest metadata.
3. Roll out Agents that generate manifests; older Agents continue to return task results without manifest.
4. Update Master task processing to persist manifest when present and tolerate absence.
5. Update UI to show manifest summary when present and fall back to existing Docker/database/task fields when absent.
6. Rollback by ignoring the manifest field and leaving staged manifest files as harmless extra backed-up files in backups already created.

## Open Questions

- Should snapshot/restic manifests be stored under a stable logical path such as `.vaultfleet/VAULTFLEET-MANIFEST.json`, or is a staged path acceptable for P0?
- Should task history persist the full manifest exactly as generated, or a normalized/redacted copy produced by Master?
- Should policy `context_name` be pulled into this change if `add-archive-output-templates` is not implemented first, or should it remain a follow-up?
