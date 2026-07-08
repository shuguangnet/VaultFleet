## Context

VaultFleet already supports path backup sources, Docker container backup sources, pre/post backup hooks, archive backups, snapshot backups, task logs, and typed source propagation from Master to Agent. Operators can manually run `pg_dump` or `mysqldump` through hooks today, but hooks are free-form shell commands and do not provide structured source validation, credential redaction, dump staging, or database-specific UI.

Database backups should be treated as logical export sources. The Agent creates dump files before the normal backup runner starts, adds those dump files or their staging directory to the backup path list, then removes the staging files after the backup finishes. This keeps the existing restic/archive execution model intact while adding database-aware source preparation.

## Goals / Non-Goals

**Goals:**
- Add typed database backup sources for PostgreSQL and MySQL.
- Support logical dumps for one database or all databases.
- Support dump execution on the Agent host and inside a Docker container.
- Keep database passwords out of persisted API responses, task logs, process arguments where practical, and command output.
- Include generated dump files in both snapshot and archive backup modes.
- Emit structured task logs and metadata for database dump preparation.
- Validate database source configuration before saving and before execution.

**Non-Goals:**
- Automatic database restore.
- Physical backup tools such as `pg_basebackup`, WAL archiving, Percona XtraBackup, or filesystem-level hot backup coordination.
- Query-level consistency checks beyond the dump tool's own exit status.
- Installing database client binaries automatically.
- Long-term local retention of generated dump files on the Agent.

## Decisions

### 1. Model databases as typed backup sources

Extend `BackupSource` with a `database` source object and add a database backup capability flag. The source configuration includes:
- engine: `postgresql` or `mysql`
- execution mode: `host` or `docker`
- Docker container identity when execution mode is `docker`
- host, port, username, password, database name, all-databases flag, and optional extra dump options
- output filename template or generated default
- compression flag for generated dump files

Rationale: Database dumps are first-class backup inputs, not storage backends and not generic hooks. Typed sources allow validation, redaction, UI controls, task metadata, and future restore workflows.

Alternative considered: Generate pre-backup hooks from policy templates. That is quicker but keeps credentials and correctness inside shell text and makes validation difficult.

### 2. Stage dump files under Agent-controlled temporary directories

For each database source, Agent creates a per-task staging directory under its config/work directory, writes dump files there, appends the generated files or staging directory to `BackupDirs`, then cleans the directory after backup completes or fails.

Rationale: The existing executor already consumes filesystem paths for both restic and archive modes. Staging lets database sources reuse that path flow without teaching restic/archive runners about database protocols.

Alternative considered: Stream dump output directly into restic or archive writers. That would reduce local disk use, but it couples database dump execution to each backup mode and makes retries and task metadata harder.

### 3. Avoid credentials in command arguments

PostgreSQL uses `PGPASSWORD` or a temporary `.pgpass` file. MySQL uses a temporary defaults file containing credentials and passes it with `--defaults-extra-file`. Docker execution passes credentials through `docker exec --env` for PostgreSQL where practical, and through a mounted or copied temporary defaults file for MySQL if needed.

Rationale: Command-line arguments are visible through process listings and often appear in logs. Temporary files and environment variables are easier to redact and clean up.

Alternative considered: Put passwords directly in `pg_dump` / `mysqldump` arguments. That is simple but unsafe.

### 4. Use native dump tools and fail clearly when missing

Agent invokes:
- PostgreSQL single database: `pg_dump`
- PostgreSQL all databases: `psql` to list databases, then `pg_dump` once per database
- MySQL single database: `mysqldump <database>`
- MySQL all databases: `mysql` to list databases, then `mysqldump <database>` once per database

For Docker execution, the commands run inside the selected container. For host execution, the commands must exist on the Agent host. Missing binaries produce actionable task errors.

Rationale: Native logical dump tools are widely available, well understood, and support consistent exports for the first iteration.

Alternative considered: Use Go database drivers and implement dump logic directly. That would create correctness and compatibility risk for database-specific DDL/data formats.

### 5. Keep generated dumps portable and optionally compressed

Default output extensions:
- PostgreSQL single/all: `.sql`
- MySQL single/all: `.sql`
- compressed dumps: `.sql.gz`

The first iteration should prefer plain SQL dumps, optionally gzip-compressed. PostgreSQL custom format can be a later extension because it changes restore tooling and file semantics.

### 6. Redact database source secrets at API and log boundaries

Database passwords are encrypted at rest with the existing Master key pattern used for policy secrets, omitted or masked from API responses, included only in Agent policy payloads, and redacted from task logs. Dump command stderr/stdout must pass through existing task log redaction plus database-specific secret values.

### 7. Surface database dump metadata without storing secrets

Task results can include non-secret database metadata: engine, database name or all-databases flag, execution mode, container name, dump filename, size, and warnings. This helps operators confirm what was exported without exposing credentials.

## Risks / Trade-offs

- [Risk] Large databases require enough Agent disk space for staged dumps. -> Mitigation: show staging/disk warning in UI and task errors; future streaming can optimize this.
- [Risk] Dump tools may not exist on host or inside container. -> Mitigation: preflight checks and clear task failures naming the missing command.
- [Risk] MySQL/PostgreSQL permissions vary by deployment. -> Mitigation: validate required fields early, surface dump stderr after redaction, and document minimal privileges.
- [Risk] Docker MySQL credential files are awkward to pass safely. -> Mitigation: prefer container-local temporary files cleaned after dump; fall back with explicit validation errors if unavailable.
- [Risk] Dumps may not represent application-consistent state for multi-service apps. -> Mitigation: keep existing pre/post hooks for app quiescing and document them as complementary.
- [Risk] Password redaction can miss variants. -> Mitigation: add tests for payload redaction, task logs, and command error paths with known secret values.

## Migration Plan

1. Add nullable database source fields in the existing typed `backup_sources` JSON model instead of a new table.
2. Add encrypted handling for database passwords when policies are saved.
3. Older policies remain unchanged because path and Docker sources keep their existing shape.
4. Agents without database backup capability reject database-source policies at Master validation time.
5. Rollback by hiding database source UI and refusing new database sources; existing path/Docker policies continue to run.

## Open Questions

- Should remote TCP execution be included in the first implementation or deferred after host/Docker execution? The model can include host/port now, but Docker/local socket use cases may cover most initial deployments.
- Should all database dumps be gzip-compressed by default to reduce storage usage, or should plain `.sql` be the default for easiest inspection?
