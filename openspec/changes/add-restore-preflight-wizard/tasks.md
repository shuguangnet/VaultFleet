## 1. Protocol and Agent Checks

- [x] 1.1 Add protocol message types, payloads, check result structs, and a restore preflight capability flag.
- [x] 1.2 Implement Agent handler support for restore preflight requests and responses.
- [x] 1.3 Add Agent file restore readiness checks for target path creation/writeability and cleanup of temporary probe files.
- [x] 1.4 Add Agent Docker restore readiness checks for Docker availability, selected metadata shape, restore path risks, and obvious container or Compose conflicts.
- [x] 1.5 Add protocol and Agent tests covering successful file preflight, unwritable path, missing capability behavior, Docker unavailable, and Docker conflict reporting.

## 2. Master Preflight API

- [x] 2.1 Add a restore preflight route that accepts source agent, target agent, snapshot, restore mode, target path, include paths, and Docker source selection.
- [x] 2.2 Reuse or extract restore planning helpers so preflight and restore execution resolve snapshot IDs and Docker metadata consistently.
- [x] 2.3 Implement Master-side checks for target agent existence, online status, capability requirements, source snapshot resolution, selected path restore requirements, and Docker metadata availability.
- [x] 2.4 Dispatch target-side preflight requests with timeout handling and merge Agent results into one structured report.
- [x] 2.5 Add Master API tests proving preflight does not create commands or task history and returns expected reports for success, missing snapshot, offline target, missing selective restore capability, missing Docker metadata, and Agent-side failures.

## 3. Guided Restore UI

- [x] 3.1 Refactor the snapshot restore drawer into a guided flow with explicit source agent, snapshot, target agent, restore mode, target path, selected paths, and Docker source review.
- [x] 3.2 Add frontend service/types for restore preflight requests and reports.
- [x] 3.3 Require a successful preflight report before enabling final restore in the guided UI.
- [x] 3.4 Render preflight errors, warnings, and informational checks with actionable messages.
- [x] 3.5 Ensure the final restore request uses the same plan values that were preflighted.
- [x] 3.6 Add frontend tests for cross-agent file preflight success, failed preflight blocking submit, Docker preflight display, and preservation of direct restore request payloads.

## 4. Documentation and Operator Guidance

- [x] 4.1 Update `README.md` and `README.en.md` with the current cross-agent restore workflow and preflight behavior.
- [x] 4.2 Document Docker restore preflight limitations, including image availability, external networks, secrets, and application-level consistency.
- [x] 4.3 Add release-note style guidance for Agent version requirements and upgrade behavior when preflight capability is missing.

## 5. Verification

- [x] 5.1 Run focused Go tests for protocol, Agent handler/checks, and Master restore API.
- [x] 5.2 Run frontend tests for the snapshot restore workflow.
- [x] 5.3 Run `npm run build`.
- [x] 5.4 Run `go test ./... -count=1`.
- [x] 5.5 Run OpenSpec validation for `add-restore-preflight-wizard`.
