## 1. Protocol And Data Model

- [x] 1.1 Add protocol constants, capability flag, verification request payload, verification result payload, and structured check result types.
- [x] 1.2 Add protocol round-trip tests for verification request/result payloads and default Agent capability reporting.
- [x] 1.3 Add database fields or tables for policy verification settings and structured verification results.
- [x] 1.4 Add migration tests proving existing policies remain valid and verification fields default safely.
- [x] 1.5 Extend task response/types to include verification task type and verification result details.

## 2. Agent Verification Execution

- [x] 2.1 Add restic runner support for `check` with tests for command arguments, password handling, rclone args, and error reporting.
- [x] 2.2 Implement snapshot selection for verification by listing repository snapshots and choosing the newest snapshot.
- [x] 2.3 Implement required verification checks for snapshot listing, restic check, full snapshot ls, and sampled listing.
- [x] 2.4 Implement optional sample restore into an Agent temporary directory with bounded sample selection and cleanup reporting.
- [x] 2.5 Add Agent handler support for verification commands, timeout handling, task slot/concurrency behavior, and structured result emission.
- [x] 2.6 Add Agent tests covering success, empty repository, restic check failure, sample restore success/failure, timeout, and cleanup warning paths.

## 3. Master API And Scheduling

- [x] 3.1 Extend policy create/update/list responses to accept and return verification settings and latest verification summary.
- [x] 3.2 Add a manual verify-now endpoint for snapshot policies with validation for policy existence, backup mode, Agent capability, and command creation.
- [x] 3.3 Persist incoming verification results into task history and verification result storage with policy and storage associations.
- [x] 3.4 Add scheduled verification dispatch that creates at most one pending or running verification command per due policy.
- [x] 3.5 Ensure verification failure uses the existing task failure notification path.
- [x] 3.6 Add Master API, scheduler, command dispatch, and result persistence tests.

## 4. Web UI

- [x] 4.1 Extend frontend policy/task types and services for verification settings, verify-now action, verification task type, and result details.
- [x] 4.2 Add policy form controls for enabling verification, schedule, timeout, sample count, and optional sample restore.
- [x] 4.3 Display latest verification status on policy list/detail views, including never-run, running, passed, and failed states.
- [x] 4.4 Add a verify-now action with unsupported archive-mode and unsupported Agent states.
- [x] 4.5 Display verification task details with per-check status, severity, message, detail, and duration.
- [x] 4.6 Add frontend tests for policy configuration, verify-now behavior, task filtering/display, and failed verification rendering.

## 5. Documentation And Validation

- [x] 5.1 Document recoverability verification semantics, default checks, optional sample restore, limitations, and operational cost.
- [x] 5.2 Document that verification is not an application-consistency guarantee and does not replace restore preflight.
- [x] 5.3 Run focused Go tests for protocol, executor, Agent handler, Master API, scheduler, and database migrations.
- [x] 5.4 Run focused frontend tests for policies and tasks.
- [x] 5.5 Run `openspec validate add-backup-recoverability-verification --strict` and resolve any spec issues.
