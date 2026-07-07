## Context

VaultFleet currently runs backup and restore tasks through Master-created commands and Agent-side handlers. Snapshot backups use restic via the Agent executor, and the runner already supports `snapshots`, `ls`, `stats`, and `restore`, but not `check`. Task history records backup and restore outcomes, while the Web UI displays policy, snapshot, and task state.

A successful backup task only proves that restic accepted data at backup time. It does not prove that the repository can later be checked, that the latest snapshot can be listed, or that a restore command can materialize files. Recoverability verification crosses policy configuration, command dispatch, Agent restic execution, task history, notifications, and Web UI state.

## Goals / Non-Goals

**Goals:**

- Add periodic and on-demand verification for snapshot backup policies.
- Verify the latest available snapshot for a policy without mutating repository data.
- Support a default verification profile using `restic snapshots`, `restic check`, `restic ls`, and sampled listing.
- Support an optional sample restore into an Agent-local temporary directory with cleanup.
- Persist structured per-check results and expose the latest verification status for policies.
- Keep verification failures visible through task history and notifications.

**Non-Goals:**

- Do not guarantee application-level consistency of databases, containers, or services.
- Do not verify archive-mode backups in the first iteration.
- Do not run verification inline after every backup by default.
- Do not restore into production paths as part of automated verification.
- Do not introduce a new queueing backend or external service.

## Decisions

### 1. Model verification as an independent maintenance task

Add a new task type such as `verify` and a new protocol command such as `backup_verify_req` / `backup_verify_resp`. The Master queues verification commands against an Agent and records them in the same command/task lifecycle used by backup and restore.

Alternative considered: append verification to every backup task. That gives fast feedback but makes backups slower, couples backup success to validation policy, and makes failure reporting ambiguous. Independent tasks let operators schedule validation separately and run it on demand after risky changes.

### 2. Verify the latest snapshot for the policy repository

The Agent loads the current policy, prepares the same rclone/restic configuration used for backup, lists snapshots, selects the newest snapshot, and verifies that snapshot. The verification result includes both the restic snapshot ID and policy/storage metadata supplied by the Master command.

Alternative considered: have the Master select a snapshot from its snapshot cache. That can be stale and would make verification depend on Master metadata freshness. The Agent should verify what the repository currently contains.

### 3. Use structured check results

Verification results should contain an overall status plus check items with stable `code`, `status`, `severity`, `message`, `detail`, and duration fields. Suggested codes:

- `snapshot_list`
- `restic_check`
- `snapshot_ls`
- `sample_ls`
- `sample_restore`
- `cleanup`

Alternative considered: store one error string in task history. That is simpler, but weak for UI display, localization, tests, and distinguishing a repository integrity failure from a cleanup warning.

### 4. Keep default verification read-only

The default profile runs repository and snapshot read checks only. `restic check` should use the existing read-only repository argument pattern where compatible, and `ls`/sample listing should use `--no-lock`. Optional sample restore writes only under an Agent-controlled temporary directory and removes the directory after inspection.

Alternative considered: always perform sample restore. It gives stronger evidence, but can be expensive, noisy on small hosts, and surprising in environments with tight disk quotas.

### 5. Add policy-level verification settings without disrupting existing policies

Add nullable policy settings, defaulting to enabled read-only verification on a conservative schedule or disabled until configured, depending on product preference during implementation. A compact JSON field is acceptable for the first iteration:

- `enabled`
- `schedule`
- `sample_count`
- `sample_restore_enabled`
- `timeout_minutes`

Alternative considered: create a separate verification policy table immediately. That is cleaner for future expansion but adds more CRUD surface before the first validation workflow proves itself. If the settings grow, a later migration can split them out.

### 6. Treat archive-mode backups as unsupported in this change

If a policy uses archive backup mode, the UI and API should either hide verification or return a clear unsupported result. Archive verification needs different mechanics, such as archive download/open validation, and should not be mixed with restic snapshot verification.

Alternative considered: implement archive validation at the same time. That broadens the storage and artifact surface and delays the P0 snapshot recoverability risk reduction.

## Risks / Trade-offs

- [Risk] `restic check` can be expensive on large repositories. -> Mitigation: schedule verification separately, add timeout settings, and keep manual runs visible as tasks.
- [Risk] Verification can pass but a later restore can still fail due to storage outages, permission changes, or application inconsistency. -> Mitigation: label results as recoverability validation, not a restore guarantee, and keep restore preflight for execution-time readiness.
- [Risk] Sample restore can consume disk or leave temporary data behind. -> Mitigation: make sample restore optional, bound sample count/size, restore under a dedicated temp directory, and report cleanup failures as warnings.
- [Risk] Old Agents do not support verification commands. -> Mitigation: add a capability flag and return an actionable upgrade error from Master/UI.
- [Risk] Verification competes with backup or restore tasks on the same Agent. -> Mitigation: use the task manager to limit concurrency or add a maintenance slot that does not run alongside backup/restore unless explicitly allowed.
- [Risk] Policy configuration may change while verification is queued. -> Mitigation: include a policy payload snapshot in the command, as backup-now already does, and record policy/storage IDs in task history.

## Migration Plan

1. Add protocol payloads, capability constants, and tests.
2. Extend restic runner with `check` and verification helpers.
3. Add Agent handler support for verification commands.
4. Add Master command creation/API support for manual verification and scheduled dispatch.
5. Persist verification settings and structured results.
6. Update Web UI policy/task displays and tests.
7. Update README/operator docs with verification semantics and limitations.

Rollback is straightforward if the database additions are nullable: hide the UI entry points and stop scheduling verification commands. Existing backup and restore commands remain compatible.

## Open Questions

- Should verification be enabled by default for every new snapshot policy, or should operators opt in per policy?
- Should `restic check` use full repository checks only, or expose a future "light" profile if performance becomes a problem?
- Should sample restore choose the smallest regular file automatically, or allow an operator-provided path pattern?
