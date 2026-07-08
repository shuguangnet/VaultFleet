## 1. Protocol And Data Model

- [x] 1.1 Add protocol manifest structs for backup manifest, source summaries, artifact summary, Docker summary, database dump summary, warnings, and schema version.
- [x] 1.2 Extend `TaskResultPayload` and executor `TaskResult` with optional manifest metadata.
- [x] 1.3 Add a nullable `Manifest` field/column to `TaskHistory` with migration coverage.
- [x] 1.4 Extend task API response types and frontend task types to include manifest data.
- [x] 1.5 Add protocol and database tests for manifest JSON round-trip, optional field behavior, and missing-manifest compatibility.

## 2. Agent Manifest Generation

- [x] 2.1 Add an Agent manifest builder that accepts effective policy context, backup mode, backup paths, exclude patterns, Docker metadata, database metadata, and warnings.
- [x] 2.2 Ensure the manifest builder redacts or omits secrets, Docker env values, storage credentials, database passwords, hook output, and rclone configuration.
- [x] 2.3 Generate path source summaries from effective backup paths while distinguishing normal paths from staged database and manifest files where possible.
- [x] 2.4 Generate Docker source summaries from resolved Docker metadata including container identity, image, Compose project/service, selected mounts, and Compose config files.
- [x] 2.5 Generate database summaries from database dump metadata including engine, execution mode, database name, output name, compression, size, and container name.
- [x] 2.6 Add Agent unit tests for manifest content, source summaries, warnings, and secret redaction.

## 3. Backup Flow Integration

- [x] 3.1 Stage `VAULTFLEET-MANIFEST.json` in a per-task temporary directory during backup execution.
- [x] 3.2 Include the staged manifest in snapshot/restic backup inputs.
- [x] 3.3 Clean manifest staging files after success, failure, cancellation, hook failure, database dump failure, or backup runner failure.
- [x] 3.4 Attach the generated manifest to the final task result sent to Master.
- [x] 3.5 Add Agent handler tests proving snapshot backups include the staged manifest path and clean it up.

## 4. Archive Inclusion

- [x] 4.1 Extend archive executor configuration with extra root-level files or manifest bytes.
- [x] 4.2 Write `VAULTFLEET-MANIFEST.json` at archive root for `tar.gz` backups.
- [x] 4.3 Write `VAULTFLEET-MANIFEST.json` at archive root for `zip` backups.
- [x] 4.4 Update archive manifest artifact fields after upload so manifest task result includes artifact name, path, format, content type, and size.
- [x] 4.5 Add archive executor tests that inspect generated tar.gz and zip files for root-level manifest content.

## 5. Master Persistence And API

- [x] 5.1 Persist manifest JSON from backup task results into task history.
- [x] 5.2 Return manifest data through task list/detail APIs without requiring artifact download.
- [x] 5.3 Ensure task result processing tolerates older Agents that do not send manifest data.
- [x] 5.4 Add Master API/task processor tests for manifest persistence, API response shape, and missing-manifest compatibility.

## 6. Web UI

- [x] 6.1 Add task history UI summary sections for manifest path sources, Docker sources, database dumps, exclude patterns, artifact details, and warnings.
- [x] 6.2 Keep existing task Docker/database displays working when manifest data is absent.
- [x] 6.3 Add a clear empty state for older backups without manifest data.
- [x] 6.4 Add frontend tests for manifest summary rendering and fallback behavior.

## 7. Documentation And Follow-Ups

- [x] 7.1 Document `VAULTFLEET-MANIFEST.json` fields, where it appears in archive and snapshot backups, and what it intentionally excludes.
- [x] 7.2 Document P1 follow-ups: policy `context_name` / `site_name`, manifest preview, UI manifest view/download, and task-history manifest JSON display.
- [x] 7.3 Document P2 follow-ups: file counts, total sizes, dump checksums, restore-wizard recommendations, and legacy missing-manifest compatibility messaging.

## 8. Verification

- [x] 8.1 Run focused Go tests for protocol, database migration, task result processing, Agent manifest generation, Agent backup flow, and archive executor.
- [x] 8.2 Run focused frontend tests for task history manifest summary.
- [x] 8.3 Run OpenSpec validation for `add-backup-content-manifest`.
- [x] 8.4 Review local/untracked files and commit only files related to this OpenSpec change during implementation.
