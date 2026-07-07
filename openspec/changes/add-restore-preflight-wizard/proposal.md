## Why

Restore is the highest-risk operation in VaultFleet: it can write files on a remote server, recreate Docker workloads, and now can target a different agent from the one that produced the snapshot. Operators need a guided workflow and a preflight check before execution so common failures are caught before data is written.

## What Changes

- Add a guided restore flow for snapshot restores that makes the source agent, snapshot, target agent, restore mode, target path, and Docker source selection explicit.
- Add a restore preflight API that validates whether a planned restore is likely to succeed before queuing the restore command.
- Preflight checks cover target agent availability, required agent capabilities, snapshot metadata availability, selective restore support, target path writeability for file restores, and Docker availability/conflicts for container restores.
- Display preflight results in the Web UI with blocking errors, warnings, and actionable remediation hints.
- Require the guided restore flow to run preflight before enabling the final restore action, while preserving existing restore APIs for compatible direct callers.
- Update operator documentation for cross-agent file and Docker restore workflows.

## Capabilities

### New Capabilities

- `restore-preflight`: Guided restore planning and preflight validation for file, selective, cross-agent, and Docker container restores.

### Modified Capabilities

- None.

## Impact

- Backend API: `internal/master/api/restore.go`, route registration, request/response types, and tests.
- Agent protocol and handler: new request/response payloads for target-side restore preflight checks.
- Agent execution helpers: target path writeability checks and Docker restore readiness checks.
- Web UI: `web/src/pages/snapshots/snapshots-page.tsx`, snapshot restore services/types, and tests.
- Documentation: `README.md`, `README.en.md`, and any restore-related docs/screenshots that mention cross-node restore.
