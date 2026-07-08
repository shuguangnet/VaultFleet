## 1. Policy Model And Protocol

- [x] 1.1 Add artifact naming fields to protocol policy payloads, policy API request/response types, frontend policy types, and persisted policy models.
- [x] 1.2 Add database migration handling for nullable `artifact_context_name`, `archive_remote_dir_template`, and `archive_name_template` fields.
- [x] 1.3 Persist and return artifact naming fields in policy create, update, list, and get flows without exposing unrelated secrets.
- [x] 1.4 Include artifact naming fields in Agent policy push payloads and bulk policy assignment/cloning flows.
- [x] 1.5 Add policy API and database tests for field persistence, default compatibility, update behavior, and payload propagation.

## 2. Template Renderer And Context Inference

- [x] 2.1 Implement a shared archive artifact naming renderer with legacy defaults, readable recommended defaults, and the supported `{{variable}}` token set.
- [x] 2.2 Implement context name suggestion from Docker Compose, single container, database, single path, and mixed policy sources.
- [x] 2.3 Implement source type detection for `path`, `docker`, `database`, and `mixed` naming contexts.
- [x] 2.4 Add safety validation for unknown tokens, absolute paths, traversal segments, empty filenames, filename separators, control characters, and unsafe rendered paths.
- [x] 2.5 Add variable-value sanitization and collision warnings for filename templates without `{{datetime}}`, `{{date}}`, or `{{time}}`.
- [x] 2.6 Add renderer unit tests for defaults, variable rendering, Docker/database/path inference, sanitization, validation failures, and collision warnings.

## 3. Preview API

- [x] 3.1 Add a server-side artifact naming preview API for policy configuration.
- [x] 3.2 Return rendered context name, source type, remote directory, artifact filename, full relative artifact path, rendered variables, and warnings from preview.
- [x] 3.3 Ensure preview distinguishes legacy compatibility output from recommended readable defaults for new configuration.
- [x] 3.4 Add API tests for preview success, missing runtime-only values, invalid templates, unsafe paths, and collision warnings.

## 4. Agent Archive Execution

- [x] 4.1 Extend Agent executor configuration and backup handler wiring to carry artifact naming fields and runtime render context.
- [x] 4.2 Render archive remote directory and filename at backup runtime using Agent, policy, time, archive format, Docker metadata, and database metadata.
- [x] 4.3 Upload archive artifacts to the rendered relative path inside the configured repository.
- [x] 4.4 Record rendered naming metadata, artifact path, size, content type, archive format, and warnings in task results.
- [x] 4.5 Update archive download/fetch logic to use stored rendered artifact paths for both named and legacy archive artifacts.
- [x] 4.6 Add Agent executor, backup handler, and Master download tests for rendered archive paths and legacy compatibility.

## 5. Manifest And Task History Metadata

- [x] 5.1 Extend manifest protocol structures with artifact context name, source type, rendered archive naming metadata, templates used, and warnings.
- [x] 5.2 Populate manifest naming metadata for archive backups and context/source metadata for snapshot backups.
- [x] 5.3 Persist rendered naming metadata in task history alongside existing artifact and manifest fields.
- [x] 5.4 Ensure naming metadata redacts or omits storage credentials, database passwords, API tokens, Docker environment values, hook output, and rclone config values.
- [x] 5.5 Add manifest builder, task result processing, and task API tests for archive, snapshot, missing metadata, and secret redaction cases.

## 6. Web UI

- [x] 6.1 Add artifact naming controls to the policy drawer, including context/site name, remote directory template, filename template, and preset/default actions for archive mode.
- [x] 6.2 Show server-side preview with rendered path, source type, variables, and warnings while editing archive-mode policies.
- [x] 6.3 Preserve snapshot behavior while showing context-name metadata as useful identification for snapshot policies.
- [x] 6.4 Show artifact context name and rendered artifact path in task history expanded rows, with a legacy hint when metadata is missing.
- [x] 6.5 Add frontend service/type coverage and policy/task page tests for naming fields, preview rendering, validation errors, warnings, and submit payloads.

## 7. Documentation And Validation

- [x] 7.1 Update operator documentation with supported variables, legacy defaults, recommended defaults, examples for websites/Docker/database/path backups, and collision guidance.
- [x] 7.2 Run focused Go tests for policy API, renderer, Agent archive execution, manifest generation, task history, and artifact download flows.
- [x] 7.3 Run focused frontend tests for policy naming controls and task history display.
- [x] 7.4 Run frontend build and OpenSpec validation for `add-backup-artifact-naming-strategy`.
- [x] 7.5 Review local changes and commit only files related to this OpenSpec change during implementation.
