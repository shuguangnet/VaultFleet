## Why

VaultFleet can report that a backup task succeeded, but operators still have no periodic proof that the latest snapshot is readable and restorable. This leaves the highest-risk failure mode unresolved: discovering repository corruption, missing credentials, unreadable snapshots, or broken restore paths only during an emergency restore.

## What Changes

- Add scheduled backup recoverability verification for snapshot backup policies.
- Verify the latest restic snapshot by running repository checks, listing snapshot contents, sampling entries, and optionally restoring a small sample to a temporary directory.
- Add an on-demand "verify now" action so operators can validate a policy after setup, storage migration, credential rotation, or Agent upgrade.
- Store verification task history with structured check results, the verified snapshot ID, durations, and failure details.
- Surface each policy's latest verification state in the Web UI and task history.
- Emit notifications for verification failures using the existing task/notification path where possible.
- Do not change restore execution semantics or require every backup task to run a verification inline.

## Capabilities

### New Capabilities

- `backup-recoverability-verification`: Periodic and on-demand validation that the latest snapshot for a policy can be checked, listed, sampled, and optionally restored.

### Modified Capabilities

- None.

## Impact

- Agent executor: add restic `check` support and a bounded sample restore helper.
- Agent protocol and handlers: add verification request/result payloads and a capability flag for Agents that support verification.
- Master command/task handling: queue verification commands, persist task history, associate results with policy and storage metadata, and expose APIs for manual verification.
- Scheduler: dispatch configured periodic verification independently from normal backup schedules.
- Database: store verification settings and structured verification result details, either in task history metadata or a dedicated verification table.
- Web UI: show verification status on policies/tasks, add a "verify now" action, and display per-check results.
