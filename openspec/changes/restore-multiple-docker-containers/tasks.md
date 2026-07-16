## 1. Protocol and Compatibility Contract

- [x] 1.1 Extend Master/Web restore input with `docker_source_ids` while preserving legacy `docker_source_id` parsing and precedence rules.
- [x] 1.2 Add the `docker_multi_container_restore` Agent capability and advertise it only when batch semantics are implemented.
- [x] 1.3 Add protocol fields for batch progress, current source identity, per-source outcome, aggregate counts, and retryability.
- [x] 1.4 Add protocol tests for legacy single-source decoding, multi-source round trips, partial-success results, and old payload compatibility.

## 2. Master Restore Planning and Preflight

- [x] 2.1 Extract a restore-plan helper that normalizes Docker source IDs, rejects empty or oversized selections, and preserves snapshot metadata order.
- [x] 2.2 Resolve all selected IDs to `DockerResolvedSource` values and reject unknown or duplicate selections before command creation.
- [x] 2.3 Gate multi-source plans on `docker_multi_container_restore` while retaining existing single-source capability behavior.
- [x] 2.4 Extend restore preflight checks with optional source ID and source name attribution.
- [x] 2.5 Detect conflicts inside the selected batch for container names, Compose project/service identities, resolved paths, and published host ports.
- [x] 2.6 Reuse the same normalized restore plan for preflight and execution so the dispatched selection exactly matches the preflighted selection.
- [x] 2.7 Add Master handler tests for valid multi-select, legacy single-select, unknown IDs, empty selection, item limit, capability mismatch, stable ordering, and no command creation on validation failure.

## 3. Agent Batch Execution

- [x] 3.1 Normalize and deduplicate resolved restore paths across all selected Docker sources before invoking the file restore runner.
- [x] 3.2 Refactor Docker restore execution to return one structured outcome per source instead of returning after the first source error.
- [x] 3.3 Execute Docker source rebuilds sequentially in request order and continue after source-specific failures.
- [x] 3.4 Mark unstarted sources as skipped when the shared data restore fails before Docker rebuild begins.
- [x] 3.5 Check cancellation before each source, propagate cancellation to active commands, and record completed versus skipped items.
- [x] 3.6 Emit batch progress with total, completed, failed, current source ID, and current source name under the restore message ID.
- [x] 3.7 Derive `success`, `partial_success`, `failed`, and `canceled` aggregate states from item outcomes.
- [x] 3.8 Add Agent executor and handler tests for two successes, first-item failure with continuation, shared-path deduplication, data-restore failure, mid-batch cancellation, and stable progress IDs.

## 4. Task Persistence and Retry

- [x] 4.1 Extend task history persistence with backward-compatible structured restore item results and `partial_success` status handling.
- [x] 4.2 Persist batch progress, logs, and final item outcomes using the same message ID and verify restart recovery.
- [x] 4.3 Return item results from task list/detail APIs without changing the shape required by legacy task records.
- [x] 4.4 Add a retry-failed restore endpoint that derives retryable failed source IDs from a completed task and creates a new preflightable plan.
- [x] 4.5 Reject retry when the original task is not a Docker restore, has no retryable failures, or references unavailable snapshot metadata.
- [x] 4.6 Add database and API tests for partial-success persistence, old rows, restart recovery, retry selection, and immutability of the original task.

## 5. Multi-Container Restore UI

- [x] 5.1 Extend frontend restore and task types plus services for multiple source IDs, attributed preflight checks, item outcomes, and retry-failed requests.
- [x] 5.2 Replace the Docker source single-select with accessible checkboxes, select-all/clear actions, stable ordering, and a selected-count summary.
- [x] 5.3 Keep the single-select experience when the target Agent lacks `docker_multi_container_restore` and display upgrade guidance.
- [x] 5.4 Group preflight findings by batch and Docker source while keeping all error-severity checks blocking.
- [x] 5.5 Show target node, snapshot, ordered source list, affected paths, overwrite warnings, and continue-on-error semantics in the confirmation step.
- [x] 5.6 Display current source and aggregate item counts in live restore progress without resizing or overlapping the task layout.
- [x] 5.7 Render per-source outcomes in task details and expose “retry failed items” only when retryable failures exist.
- [x] 5.8 Add React tests for multi-select submission, capability fallback, grouped preflight, confirmation scope, partial-success rendering, retry payload, and stale-selection reset when snapshot or target changes.

## 6. Documentation and Operational Safety

- [x] 6.1 Update `README.md` and `README.en.md` with batch restore scope, sequential execution, partial-success semantics, limits, and Agent version requirements.
- [x] 6.2 Document that image layers, external networks, secrets, registry credentials, dependency inference, and application health checks remain outside the restore guarantee.
- [x] 6.3 Add upgrade and rollback notes explaining capability gating and compatibility with legacy single-container restore.

## 7. Loop Verification Gates

- [x] 7.1 Run focused protocol, Master restore/preflight, Agent Docker/handler, task persistence, and frontend restore tests after their respective task groups.
- [x] 7.2 Add or update an integration test that restores at least two Docker sources and proves one source failure does not suppress the other result.
- [x] 7.3 Run `gofmt` on changed Go files and `git diff --check`.
- [x] 7.4 Run `go test ./... -count=1` and resolve all failures without skipping unrelated existing tests.
- [x] 7.5 Run `cd web && npm run test -- --run` and `npm run build`.
- [x] 7.6 Run `openspec validate restore-multiple-docker-containers --strict` and ensure every task checkbox reflects actual repository state.
- [x] 7.7 Perform a manual two-container restore against disposable fixtures, capture the message ID and task outcome evidence, and verify failed-item retry creates a separate task.

## 8. Compose Environment Recovery

- [x] 8.1 Extend Docker Compose protocol metadata with backward-compatible environment-file paths.
- [x] 8.2 Discover label-declared and conventional `.env` files, normalize and deduplicate them, and include readable files in resolved backup paths.
- [x] 8.3 Restore Compose services with explicit project-directory and environment-file arguments.
- [x] 8.4 Block preflight when Compose variable references have no usable environment file while preserving variable-free legacy snapshots.
- [x] 8.5 Add protocol and Agent regression tests proving environment discovery, safe metadata, restore arguments, and missing-environment preflight behavior.
- [x] 8.6 Run formatting, focused and full Go tests, diff checks, and strict OpenSpec validation.
