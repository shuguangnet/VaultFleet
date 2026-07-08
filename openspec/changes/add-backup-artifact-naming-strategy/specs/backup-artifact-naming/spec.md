## ADDED Requirements

### Requirement: Configure backup artifact naming
VaultFleet SHALL allow backup policies to define optional artifact naming fields for identifying backup contents.

#### Scenario: Save artifact naming fields
- **WHEN** an operator creates or updates a policy with `artifact_context_name`, `archive_remote_dir_template`, and `archive_name_template`
- **THEN** VaultFleet persists those fields and returns them in policy API responses
- **AND** VaultFleet includes those fields in Agent policy payloads

#### Scenario: Preserve legacy archive defaults
- **WHEN** an archive-mode policy has no artifact naming fields configured
- **THEN** VaultFleet uses the legacy archive output behavior of remote directory `artifacts` and filename template `backup-{{datetime}}.{{ext}}`

#### Scenario: Do not change restic repository layout
- **WHEN** a snapshot-mode policy has artifact naming fields configured
- **THEN** VaultFleet SHALL NOT change restic repository paths, snapshot IDs, or restore semantics
- **AND** VaultFleet SHALL still expose the configured context name in manifest and task history metadata

### Requirement: Suggest context names from backup sources
VaultFleet SHALL provide a best-effort context name suggestion from the policy backup sources when the operator has not set an explicit context name.

#### Scenario: Suggest Docker Compose context
- **WHEN** a policy contains a Docker source with Compose project metadata
- **THEN** VaultFleet suggests the Compose project as the artifact context name

#### Scenario: Suggest container context
- **WHEN** a policy contains a single Docker container source without Compose project metadata
- **THEN** VaultFleet suggests the container name as the artifact context name

#### Scenario: Suggest database context
- **WHEN** a policy contains a database source for one database
- **THEN** VaultFleet suggests a context name derived from database engine and database name

#### Scenario: Suggest path context
- **WHEN** a policy contains one path source and no richer source metadata
- **THEN** VaultFleet suggests the final path segment as the artifact context name

#### Scenario: Suggest mixed context
- **WHEN** a policy contains multiple mixed source types and no explicit context name
- **THEN** VaultFleet suggests the policy name when available

### Requirement: Render supported naming template variables
VaultFleet SHALL render artifact naming templates using a constrained `{{variable}}` token set and SHALL reject unsupported variables.

#### Scenario: Render common variables
- **WHEN** an archive backup renders templates containing `{{date}}`, `{{time}}`, `{{datetime}}`, `{{agent_id}}`, `{{agent_name}}`, `{{policy_id}}`, `{{policy_name}}`, `{{context_name}}`, `{{site_name}}`, `{{source_type}}`, `{{format}}`, and `{{ext}}`
- **THEN** VaultFleet renders those variables from the backup runtime context

#### Scenario: Render Docker variables
- **WHEN** an archive backup renders templates for a Docker source with known container or Compose metadata
- **THEN** VaultFleet makes `{{container_name}}`, `{{compose_project}}`, and `{{compose_service}}` available to the templates

#### Scenario: Render database variables
- **WHEN** an archive backup renders templates for a database source or staged database dump metadata
- **THEN** VaultFleet makes `{{database_engine}}` and `{{database_name}}` available to the templates when unambiguous

#### Scenario: Reject unsupported variables
- **WHEN** an operator previews or saves a naming template containing an unsupported token such as `{{hostname}}`
- **THEN** VaultFleet rejects the template with a validation error naming the unsupported token

### Requirement: Validate artifact naming output safely
VaultFleet SHALL validate rendered artifact directories and filenames so backups cannot create unsafe object keys or escape the configured repository prefix.

#### Scenario: Reject path traversal
- **WHEN** a remote directory or filename template contains or renders to a `..` path segment
- **THEN** VaultFleet rejects the template with a path safety validation error

#### Scenario: Reject absolute paths
- **WHEN** a remote directory or filename template contains or renders to an absolute path
- **THEN** VaultFleet rejects the template with a path safety validation error

#### Scenario: Reject invalid filenames
- **WHEN** an archive filename template renders to an empty filename or a filename containing path separators
- **THEN** VaultFleet rejects the template before backup execution

#### Scenario: Sanitize variable values
- **WHEN** a template variable value contains unsafe characters for object keys
- **THEN** VaultFleet replaces unsafe characters in that variable value with `_` before composing the final artifact path

#### Scenario: Warn about collision-prone templates
- **WHEN** an archive filename template does not include `{{datetime}}`, `{{date}}`, or `{{time}}`
- **THEN** VaultFleet returns a warning that future backups may collide or overwrite prior artifacts

### Requirement: Preview artifact naming templates
VaultFleet SHALL provide a server-side preview for artifact naming configuration.

#### Scenario: Preview custom archive path
- **WHEN** an operator enters artifact naming fields for an archive-mode policy
- **THEN** VaultFleet returns a preview containing rendered context name, remote directory, artifact filename, full relative artifact path, variables, and warnings

#### Scenario: Preview readable recommended defaults
- **WHEN** an operator configures a new archive-mode policy without custom templates
- **THEN** the policy UI can show a recommended readable preview based on `archives/{{agent_name}}/{{context_name}}/{{date}}` and `{{context_name}}_{{agent_name}}_{{datetime}}.{{ext}}`

#### Scenario: Preview legacy compatibility
- **WHEN** an existing archive-mode policy has no artifact naming fields configured
- **THEN** VaultFleet previews the legacy `artifacts/backup-{{datetime}}.{{ext}}` output

#### Scenario: Surface missing source values
- **WHEN** a preview references Docker or database variables that are unavailable before runtime
- **THEN** VaultFleet returns a warning explaining that the actual backup may fill those variables from resolved runtime metadata

### Requirement: Upload and record named archive artifacts
VaultFleet SHALL upload archive artifacts to the rendered relative remote path and record the rendered naming metadata.

#### Scenario: Upload archive to rendered path
- **WHEN** an archive backup runs with remote directory template `archives/{{agent_name}}/{{context_name}}/{{date}}` and filename template `{{context_name}}_{{agent_name}}_{{datetime}}.{{ext}}`
- **THEN** the Agent uploads the archive under the rendered relative path inside the configured repository

#### Scenario: Record rendered artifact metadata
- **WHEN** a named archive backup succeeds
- **THEN** VaultFleet records rendered context name, remote directory, artifact filename, relative artifact path, archive format, content type, size, and warnings in task history

#### Scenario: Download named archive artifact
- **WHEN** an operator downloads a completed archive backup with a rendered artifact path
- **THEN** VaultFleet fetches the artifact from the stored rendered artifact path rather than assuming the default `artifacts/` directory

### Requirement: Include naming metadata in backup manifests
VaultFleet SHALL include non-secret artifact naming metadata in `VAULTFLEET-MANIFEST.json`.

#### Scenario: Manifest includes archive naming metadata
- **WHEN** an archive backup generates a manifest
- **THEN** the manifest includes context name, source type, rendered archive directory, rendered archive filename, rendered relative artifact path, and naming warnings when available

#### Scenario: Manifest includes snapshot context metadata
- **WHEN** a snapshot backup generates a manifest
- **THEN** the manifest includes context name and source type without changing restic snapshot storage behavior

#### Scenario: Manifest omits secrets
- **WHEN** naming metadata is written to the manifest
- **THEN** VaultFleet SHALL NOT include storage credentials, database passwords, API tokens, Docker environment values, hook output, or rclone configuration values

### Requirement: Show artifact naming in task history UI
VaultFleet SHALL show artifact naming metadata in task history so operators can identify what each backup contains without opening the artifact.

#### Scenario: Show archive identity
- **WHEN** a task history row has rendered archive naming metadata
- **THEN** the UI shows the context name and rendered artifact path in the expanded task details

#### Scenario: Show snapshot identity
- **WHEN** a snapshot task history row has manifest context metadata
- **THEN** the UI shows the context name and source type in the expanded task details

#### Scenario: Handle legacy backups
- **WHEN** a task history row lacks naming metadata and manifest context
- **THEN** the UI remains functional and shows a legacy or unavailable naming hint
