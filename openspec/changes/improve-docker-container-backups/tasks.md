## 1. Protocol and Data Model

- [x] 1.1 Add protocol constants, capability names, request/response payloads, Docker workload structs, and round-trip tests.
- [x] 1.2 Add typed backup source models for path and Docker container sources in shared Go and TypeScript types.
- [x] 1.3 Add database storage for typed backup sources with migration/backfill from existing `backup_dirs`.
- [x] 1.4 Preserve `backup_dirs` serialization for existing API responses, policy push payloads, and legacy Agents.

## 2. Agent Docker Discovery and Resolution

- [x] 2.1 Implement a small Docker Engine API client over the local Unix socket with timeouts and structured errors.
- [x] 2.2 Add Agent Docker discovery that returns containers, image/state, labels, compose hints, mounts, selectable status, and warnings.
- [x] 2.3 Advertise `docker_workload_backups` only when Docker inspection is available.
- [x] 2.4 Resolve selected Docker container sources at backup execution time using ID, compose identity, then name.
- [x] 2.5 Convert resolved bind mounts, volume mountpoints, anonymous volume mountpoints, and selected compose files into backup targets.
- [x] 2.6 Fail before backup execution when selected containers are missing, ambiguous, empty, or resolve to unreadable paths.
- [x] 2.7 Record Docker source metadata and resolution warnings in task or snapshot metadata.

## 3. Master API and Command Flow

- [x] 3.1 Add an authenticated API endpoint for Docker workload discovery on an Agent.
- [x] 3.2 Route Docker discovery requests through the existing WebSocket waiter flow with online/capability checks.
- [x] 3.3 Validate policy submissions so Docker sources require a Docker-capable Agent and at least one backup source.
- [x] 3.4 Include typed backup sources in policy push payloads for capable Agents.
- [x] 3.5 Keep path-only policies compatible with old Agents and existing queued command behavior.

## 4. Web UI

- [x] 4.1 Extend policy form state, services, and tests for typed backup sources.
- [x] 4.2 Add a backup source section with path mode and Docker container mode in the policy drawer.
- [x] 4.3 Add Docker container discovery UI with refresh, loading, unavailable, warning, and selected-count states.
- [x] 4.4 Show container identity, image, state, compose project/service, and mount preview before selection.
- [x] 4.5 Keep manual path textarea and directory browser behavior unchanged for path backups.
- [x] 4.6 Ensure storage configuration UI continues to treat `container` as storage container/bucket terminology, not Docker.

## 5. Tests and Documentation

- [x] 5.1 Add Agent unit tests for Docker unavailable, permission denied, discovery success, ambiguous matching, missing containers, and unreadable mounts.
- [x] 5.2 Add Master API tests for discovery routing, capability rejection, policy validation, and legacy compatibility.
- [x] 5.3 Add frontend tests for Docker-capable and non-Docker Agent policy flows.
- [x] 5.4 Run targeted Go tests for protocol, Agent handler/executor, Master API/router, and command service.
- [x] 5.5 Run targeted web tests for policies and services.
- [x] 5.6 Update README documentation for Docker socket requirements, backup scope, consistency limitations, and restore expectations.
