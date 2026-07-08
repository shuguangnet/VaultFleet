## 1. Policy Model And Protocol

- [ ] 1.1 Add archive output template fields to policy request/response types, protocol policy payloads, frontend policy types, and database model/migration handling.
- [ ] 1.2 Persist and return `archive_remote_dir_template`, `archive_name_template`, and `archive_context_name` for policy create/update/list/get flows.
- [ ] 1.3 Include archive output template fields when pushing policies to Agents and when cloning policies through bulk assignment.
- [ ] 1.4 Add backend API tests for create/update/list/get redaction compatibility, policy push payload propagation, and default values for existing policies.

## 2. Template Rendering And Validation

- [ ] 2.1 Implement a shared archive output template renderer with the supported `{{variable}}` token set and deterministic default templates.
- [ ] 2.2 Add path safety validation for unknown tokens, traversal segments, absolute paths, empty filenames, filename separators, and unsafe rendered characters.
- [ ] 2.3 Add warning generation for templates that may collide because they do not include time-varying tokens.
- [ ] 2.4 Cover renderer defaults, valid variables, Docker variables, sanitization, validation failures, and collision warnings in unit tests.

## 3. Archive Backup Execution

- [ ] 3.1 Extend Agent executor configuration and archive job execution to render artifact name and remote artifact path from policy templates at runtime.
- [ ] 3.2 Populate render context from Agent ID/name, policy identity, archive format, current UTC time, archive context name, and resolved Docker source metadata.
- [ ] 3.3 Upload archive artifacts to the rendered relative remote path and record the rendered artifact metadata in task results.
- [ ] 3.4 Update artifact download/fetch paths to use stored rendered artifact paths without assuming the `artifacts/` directory.
- [ ] 3.5 Add Agent executor and Master task/download tests for templated archive upload, default compatibility, and stored artifact metadata.

## 4. Preview API And Web UI

- [ ] 4.1 Add a server-side archive output preview API that returns rendered directory, artifact name, full relative path, variables, warnings, and validation errors.
- [ ] 4.2 Add archive output template fields and archive context name to the policy drawer only for archive mode.
- [ ] 4.3 Show a live preview of the rendered archive path, including warnings for missing Docker-derived values or non-unique filenames.
- [ ] 4.4 Ensure switching between snapshot and archive modes preserves safe form state and disables irrelevant archive controls for snapshot mode.
- [ ] 4.5 Add frontend service/type tests and policy page tests for preview rendering, validation feedback, and submit payload fields.

## 5. Verification And Documentation

- [ ] 5.1 Run focused Go tests for policy API, command payloads, Agent archive execution, and task artifact downloads.
- [ ] 5.2 Run focused frontend tests for policy archive output template controls.
- [ ] 5.3 Update operator documentation or README sections describing archive output templates, supported variables, defaults, and collision guidance.
- [ ] 5.4 Review existing untracked/local changes and commit only files related to this OpenSpec change during implementation.
