# Goal Template: Multi-Container Docker Restore

## Objective

Implement the OpenSpec change `restore-multiple-docker-containers` end to end. The finished product must let an operator select and restore multiple Docker containers or Compose services from one snapshot in one task, preflight the complete selection, execute sequentially, preserve per-item outcomes, report partial success, and retry only failed items.

## Source of Truth

Read these files completely before implementation:

- `AGENTS.md`
- `openspec/changes/restore-multiple-docker-containers/proposal.md`
- `openspec/changes/restore-multiple-docker-containers/design.md`
- `openspec/changes/restore-multiple-docker-containers/specs/multi-container-restore/spec.md`
- `openspec/changes/restore-multiple-docker-containers/tasks.md`

Use `/opsx:apply` to execute the change. Treat the specification scenarios as acceptance tests and `tasks.md` as the durable progress ledger.

## Loop Contract

Repeat this loop until every task and verification gate is complete:

1. Select the earliest unchecked task whose dependencies are complete.
2. Inspect the relevant existing code and tests before editing.
3. State the intended behavioral change and the smallest verification that proves it.
4. Add or update a failing test when the task changes behavior.
5. Implement only the selected task and necessary compatibility plumbing.
6. Run the smallest relevant test set; diagnose failures from evidence rather than bypassing tests.
7. Run formatting and `git diff --check` for touched files.
8. Mark the task complete immediately only when its behavior is implemented and verified.
9. Record changed files, commands run, results, and any residual risk in the loop report.
10. Continue automatically with the next ready task unless genuinely blocked by missing user authority or external state.

## Engineering Invariants

- Preserve legacy `docker_source_id` and existing single-container restore behavior.
- Never send multiple Docker sources to an Agent without `docker_multi_container_restore`.
- Preflight and execution must consume the same normalized plan.
- Restore shared data paths once, then rebuild containers sequentially.
- A source-specific failure must not prevent remaining sources from running.
- A shared data-restore failure must prevent all Docker rebuilds and mark them skipped.
- Logs, progress, command state, and final task result must use one message ID.
- Task history must survive Master restart and retain per-source outcomes.
- Retrying failures must create a new task and must not mutate the original task.
- Do not expose credentials, tokens, private addresses, database files, or real customer data in tests or commits.
- Preserve unrelated worktree changes and do not rewrite completed OpenSpec changes.

## Required Evidence Per Loop

Report progress in this format:

```text
Task: <tasks.md checkbox ID and description>
Behavior: <observable behavior added or corrected>
Files: <changed files>
Tests: <exact commands and pass/fail result>
Evidence: <key assertion, API payload, log, or UI state proving completion>
Risks: <remaining risk, or none>
Next: <next ready checkbox>
```

## Completion Gate

Do not mark the Goal complete until all of the following are true:

- Every checkbox in `tasks.md` is complete and accurately reflects the repository.
- Focused tests exist for every specification scenario or equivalent behavior boundary.
- `go test ./... -count=1` passes.
- `cd web && npm run test -- --run` passes.
- `cd web && npm run build` passes.
- `git diff --check` passes and changed Go files are formatted.
- `openspec validate restore-multiple-docker-containers --strict` passes.
- A disposable two-container restore proves success/partial-success reporting and failed-item retry.
- The final report lists implementation commits, verification evidence, deployment impact, rollback steps, and any known limitations.

If a loop uncovers a defect outside the current checkbox that blocks acceptance, add a new unchecked task under the appropriate section before fixing it. This keeps the Goal self-correcting and prevents hidden work.
