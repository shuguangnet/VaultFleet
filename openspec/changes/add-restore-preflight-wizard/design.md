## Context

VaultFleet already supports snapshot browsing, selective restore, Docker container restore, and cross-agent restore by sending the restore command to a target agent while resolving snapshot metadata from a source agent. The current UI exposes these pieces in one drawer, but it does not force operators to validate a restore plan before executing it. Common problems such as an offline target agent, missing selective-restore capability, unwritable target paths, Docker socket access problems, missing Docker metadata, or container name conflicts are only discovered after the restore command is queued or starts running.

Restore planning spans Master state, stored snapshot metadata, target Agent capabilities, and target host runtime checks. A design is needed because the change crosses API, protocol, Agent handlers, Docker checks, and frontend workflow.

## Goals / Non-Goals

**Goals:**

- Provide a guided restore workflow that makes source agent, snapshot, target agent, restore mode, target path, and Docker source selection explicit.
- Add a preflight API that returns structured blocking errors, warnings, and informational checks before the final restore command is queued.
- Validate Master-known conditions synchronously on the Master, including source snapshot resolution, Docker metadata availability, target agent existence/status, and capability requirements.
- Validate target-host conditions through the target Agent when it is online, including file target path writeability and Docker readiness.
- Keep existing restore submission behavior compatible for direct API callers that already call `/api/agents/:id/restore`.

**Non-Goals:**

- Do not guarantee application-level consistency of restored data.
- Do not add image-layer backup, image export/import, or full Docker network reconstruction.
- Do not require preflight for non-UI API callers in the first iteration.
- Do not introduce a new storage engine, worker queue, or external dependency.
- Do not make offline target agents executable immediately; offline targets can still receive queued restore commands, but host runtime preflight cannot pass while offline.

## Decisions

### 1. Model preflight as a restore plan, not as a side effect of restore execution

Add a new Master endpoint that accepts the same core restore inputs as restore execution: source agent, snapshot ID, target agent, restore mode, target path, include paths, and Docker source ID. The endpoint returns a structured report without creating a restore command.

Alternative considered: make restore execution internally perform preflight and reject failures. That catches mistakes but does not let the UI explain and fix them before the operator commits to restoring.

### 2. Split checks between Master-known and target-Agent-known conditions

The Master should check what it already owns: agent records, capabilities, snapshot ID resolution, stored Docker metadata, and whether selective restore is required. The target Agent should check host-local facts: target path can be created/written, Docker is available, selected mount paths are plausible, container/compose conflicts are detectable, and required commands are available.

Alternative considered: perform all checks on the Agent. That would force the Agent to know more about Master metadata and would make offline/metadata failures harder to report cleanly.

### 3. Return structured results with severities and codes

Preflight responses should contain an overall status plus check items with `code`, `severity`, `message`, and optional `detail`. The UI should treat `error` severity as blocking, `warning` as visible but not blocking, and `info` as explanatory. Codes make tests and future localization stable.

Alternative considered: return one text error string. That is simpler but makes UI remediation, testing, and partial success reporting weak.

### 4. Keep the existing restore endpoint compatible

The existing restore endpoint should continue accepting direct requests. The guided UI should require a successful preflight before enabling final submit, but API clients remain responsible for their own safety unless a future change introduces an explicit enforcement option.

Alternative considered: require a preflight token for every restore call. That is safer but is a breaking API change and complicates queued/offline restore behavior.

### 5. Treat offline target agents as a clear preflight failure in the guided UI

The restore command system can queue commands for offline agents, but the guided restore workflow is intended to reduce operator risk before execution. If the target is offline, preflight should report that host runtime checks cannot be completed and block the guided final action.

Alternative considered: allow warning-only offline preflight so operators can queue restores. That preserves queueing convenience but undermines the purpose of "preflight passed" because key host checks were never run.

### 6. Docker restore preflight validates readiness, not perfect topology equivalence

Docker checks should verify that Docker is reachable, metadata is present, selected source paths are known, compose files or container metadata are usable, and obvious conflicts are reported. It should not promise that the restored application will start correctly, because images, external networks, secrets, registry access, and application-level migrations are outside backup metadata.

Alternative considered: block unless the target can fully recreate the container. That would be attractive but too brittle and would reject valid restore plans that require operator-supplied environment fixes.

## Risks / Trade-offs

- [Risk] Preflight can pass but restore still fails due to changes after the check, storage outages, permission changes, or application-specific problems. -> Mitigation: label preflight as readiness validation, not a guarantee, and keep restore task errors visible.
- [Risk] Online-only preflight conflicts with existing queued restore workflows. -> Mitigation: keep direct restore API compatible; only the guided UI requires preflight success.
- [Risk] Docker conflict detection can be incomplete across Compose versions and custom runtimes. -> Mitigation: report best-effort warnings and keep final Docker restore errors in task history.
- [Risk] Target path writeability checks might create probe files or directories. -> Mitigation: use temporary probe files under the requested target directory and remove them; report cleanup failure as a warning.
- [Risk] More protocol messages can create version skew with old Agents. -> Mitigation: gate target-side preflight behind a new Agent capability and return an actionable upgrade error when missing.

## Migration Plan

1. Add protocol types and capability flags for restore preflight.
2. Add Agent handler support while preserving existing restore request handling.
3. Add Master preflight API and tests.
4. Update the Web UI guided restore flow to call preflight before final restore.
5. Update README documentation and release notes.

Rollback is straightforward: hide the guided UI preflight step and continue using the existing restore endpoint. New Agents can keep advertising the preflight capability without affecting older Masters.

## Open Questions

- Should the UI allow an administrator override for warning-only or offline preflight results, or should guided restore always require online target validation?
- Should preflight reports be persisted in task history or audit logs when the final restore is submitted?
- Should Docker image availability be checked by inspecting local images, pulling from registries, or left as a warning-only concern?
