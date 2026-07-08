## Why

VaultFleet can currently back up MySQL and PostgreSQL only through manual pre-backup hook commands. That works for expert operators, but it is easy to misconfigure, exposes credentials in shell text, produces inconsistent dump locations, and does not give structured task logs or validation.

## What Changes

- Add first-class database backup sources for PostgreSQL and MySQL.
- Support logical dump backups using `pg_dump` / `pg_dumpall` and `mysqldump`.
- Support running dump commands from an Agent host or inside a selected Docker container.
- Store database connection and dump options as typed policy source configuration instead of free-form hook commands.
- Write dump files into an Agent-managed staging directory and include them in both snapshot and archive backup modes.
- Redact database passwords from API responses, logs, task results, and command output.
- Add policy UI controls for database source configuration, validation, and operator guidance.
- Do not implement automatic database restore in this change.

## Capabilities

### New Capabilities
- `database-backup-sources`: MySQL and PostgreSQL logical dump backup sources, validation, execution, metadata, and UI configuration.

### Modified Capabilities
- None.

## Impact

- Protocol: extend backup source payloads with database source configuration and add a database backup capability flag.
- Master API/database: persist encrypted database source secrets, validate source compatibility, redact responses, and push typed database sources to capable Agents.
- Agent: execute database dump commands safely, stage dump artifacts, include staged artifacts in backup paths, emit task logs, and clean up temporary files.
- Docker integration: allow database dumps to run through `docker exec` for containerized databases.
- Web UI: add database source controls to policy creation/editing and show validation feedback.
- Tests: policy validation/redaction, command payloads, dump command construction, Agent staging/cleanup, task logging, and frontend form behavior.
