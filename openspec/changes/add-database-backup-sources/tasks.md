## 1. Protocol And Policy Model

- [x] 1.1 Add database backup source protocol types, constants, capability flag, and task metadata structures for PostgreSQL/MySQL dump sources.
- [x] 1.2 Extend frontend policy types and API types to include database source configuration and redacted password state.
- [x] 1.3 Update policy normalization so path, Docker, and database sources can coexist without breaking legacy `backup_dirs`.
- [x] 1.4 Add protocol and policy model tests for database source serialization, redaction shape, and backward compatibility.

## 2. Master API Validation And Secret Handling

- [x] 2.1 Persist database source configuration inside `backup_sources` while encrypting database passwords before storage.
- [x] 2.2 Redact database passwords from create/update/list/get responses and audit-sensitive payloads.
- [x] 2.3 Validate database source engine, execution mode, database selection, username, Docker target, and Agent capability before saving or pushing policies.
- [x] 2.4 Include decrypted database source secrets only in Agent policy push payloads.
- [x] 2.5 Add Master API tests for validation failures, encrypted storage, response redaction, and pushed payload contents.

## 3. Agent Database Dump Execution

- [x] 3.1 Add an Agent database dump package or service that prepares per-task staging directories and builds PostgreSQL/MySQL dump commands.
- [x] 3.2 Implement host execution for `pg_dump`, `psql` database discovery, `mysqldump`, and `mysql` database discovery with safe credential passing and timeout/cancellation handling.
- [x] 3.3 Implement Docker execution for PostgreSQL/MySQL dump sources using selected container identity and safe credential passing.
- [x] 3.4 Support single-database and all-databases dump modes, optional gzip compression, deterministic dump filenames, and non-secret metadata.
- [x] 3.5 Redact database secrets from dump stdout/stderr task logs and remove temporary credential files after each dump.
- [x] 3.6 Add Agent unit tests for command construction, missing binary errors, secret redaction, staging cleanup, Docker execution, and dump metadata.

## 4. Backup Flow Integration

- [x] 4.1 Resolve database sources before normal backup execution and append generated dump paths to the effective backup directory list.
- [x] 4.2 Ensure snapshot-mode backups include staged database dumps in restic paths.
- [x] 4.3 Ensure archive-mode backups include staged database dumps in generated archive artifacts.
- [x] 4.4 Clean database staging directories after success, failure, cancellation, hook failure, or backup runner failure.
- [x] 4.5 Add integration-style Agent handler tests covering database dump preparation with snapshot and archive backup modes.

## 5. Web UI

- [x] 5.1 Add database source controls to the policy drawer with engine, execution mode, Docker container, host/port, username, password, database/all-databases, and compression fields.
- [x] 5.2 Show database-specific validation help and explain that database restore is not automatic in this change.
- [x] 5.3 Hide or disable database source controls for Agents that do not advertise database backup support.
- [x] 5.4 Preserve database source form state when editing policies while keeping passwords masked unless replaced.
- [x] 5.5 Add frontend tests for adding/editing/removing database sources, Docker mode selection, redacted passwords, and submit payloads.

## 6. Verification And Documentation

- [x] 6.1 Run focused Go tests for protocol, Master policy API, Agent database dump execution, and backup handler integration.
- [x] 6.2 Run focused frontend tests for policy database source controls.
- [x] 6.3 Update operator documentation with MySQL/PostgreSQL examples, required dump tools, minimal privileges, Docker execution notes, and restore limitations.
- [x] 6.4 Review local/untracked files and commit only files related to this OpenSpec change during implementation.
