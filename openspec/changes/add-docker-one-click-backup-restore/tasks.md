## 1. Protocol And Agent Docker Core

- [x] 1.1 Add protocol capabilities, message types, and payload structs for Docker discovery, Docker restore, and Docker restore results.
- [x] 1.2 Implement an agent-side Docker package that discovers containers with the Docker CLI, normalizes mounts/ports/compose metadata, redacts sensitive values for responses, and builds a backup manifest.
- [x] 1.3 Add agent handlers for Docker discovery and Docker restore requests, including timeout handling, task result reporting, precheck-only mode, and guarded startup execution.
- [x] 1.4 Add agent unit tests for discovery parsing, Docker unavailable errors, manifest generation, restore precheck failures, and startup-disabled restore.

## 2. Master API And Policy Generation

- [x] 2.1 Add master API routes for Docker discovery and one-click Docker backup profile creation on an agent.
- [x] 2.2 Generate or update a standard backup policy from selected Docker containers, including metadata directory and manifest-generation pre-hook.
- [x] 2.3 Add a master API route for Docker restore that queues the new agent request and records command/task state consistently with existing restore commands.
- [x] 2.4 Add backend tests for discovery proxying, policy generation validation, immediate backup queuing, and restore request validation.

## 3. Frontend Workflow

- [x] 3.1 Add TypeScript types and service functions for Docker discovery, Docker backup profile creation, and Docker restore requests.
- [x] 3.2 Add a one-click Docker backup action in the node detail workflow with container selection, path preview, and immediate-backup option.
- [x] 3.3 Add Docker restore controls to the snapshot restore workflow with dry-run/precheck display and explicit start-containers confirmation.
- [x] 3.4 Add frontend tests covering the Docker backup and restore UI flows.

## 4. Documentation And Verification

- [x] 4.1 Update README.md and README.en.md with the one-click Docker backup/restore workflow, supported scope, and non-goals.
- [x] 4.2 Run focused Go and frontend test suites and record any residual limitations.
