## 1. Protocol And Agent Logging

- [x] 1.1 Add task log protocol payload, message type, and Agent capability constant.
- [x] 1.2 Implement an Agent task log emitter with sequence numbers, timestamps, levels, phases, streams, line length limits, and redaction.
- [x] 1.3 Wire structured stage log events into backup, verification, archive, Docker source resolution, and hook execution paths.
- [x] 1.4 Capture bounded restic/rclone stdout and stderr where command wrappers support it, without leaking command arguments or config secrets.
- [x] 1.5 Add Agent/protocol unit tests for payload compatibility, redaction, truncation, sequence ordering, and old-Master tolerance.

## 2. Master Buffering And APIs

- [x] 2.1 Add a bounded live task log buffer keyed by Agent ID and message ID with line/byte limits and completion TTL cleanup.
- [x] 2.2 Handle incoming task log WebSocket messages, apply defensive redaction, and store them in the buffer.
- [x] 2.3 Add authenticated APIs for task and command log retrieval with `after` sequence and `limit` support.
- [x] 2.4 Return clear empty/unavailable states for missing message IDs, expired buffers, unsupported Agents, and tasks with no emitted lines.
- [x] 2.5 Add Master tests for buffering, truncation metadata, permissions, redaction, task lookup, and incremental retrieval.

## 3. Web UI Log Viewer

- [x] 3.1 Extend frontend task/log types and services for live log retrieval.
- [x] 3.2 Add a task log drawer or panel from task history rows for backup and verification tasks.
- [x] 3.3 Implement auto-follow polling, pause/resume, copy visible logs, and empty-state messaging.
- [x] 3.4 Keep existing progress rendering unchanged while adding log access alongside running task details.
- [x] 3.5 Add frontend tests for log service calls, auto-follow behavior, empty states, and permission-aware rendering.

## 4. Documentation And Validation

- [x] 4.1 Document live backup log behavior, retention limits, redaction boundaries, and operational guidance.
- [x] 4.2 Run focused Go tests, frontend tests, frontend build, and strict OpenSpec validation.
