## ADDED Requirements

### Requirement: Configure archive output templates
VaultFleet SHALL allow archive-mode backup policies to define an optional remote directory template, filename template, and archive context name.

#### Scenario: Save archive output templates
- **WHEN** an operator creates or updates an archive-mode policy with `archive_remote_dir_template`, `archive_name_template`, and `archive_context_name`
- **THEN** VaultFleet persists those fields and includes them in policy API responses and Agent policy payloads

#### Scenario: Preserve existing archive defaults
- **WHEN** an archive-mode policy has no output templates configured
- **THEN** VaultFleet uses `artifacts` as the remote directory and `backup-{{datetime}}.{{ext}}` as the archive filename template

#### Scenario: Ignore archive templates for snapshot execution
- **WHEN** a snapshot-mode policy contains archive output template fields
- **THEN** snapshot backup execution continues to use the existing restic repository path behavior and does not create archive artifacts

### Requirement: Render supported archive template variables
VaultFleet SHALL render archive output templates using a limited set of supported `{{variable}}` tokens and SHALL reject unknown template variables during validation.

#### Scenario: Render common variables
- **WHEN** an archive backup runs with templates containing `{{date}}`, `{{time}}`, `{{datetime}}`, `{{agent_id}}`, `{{agent_name}}`, `{{policy_id}}`, `{{policy_name}}`, `{{context_name}}`, `{{site_name}}`, `{{format}}`, and `{{ext}}`
- **THEN** VaultFleet renders those variables into the archive remote path using the backup runtime context

#### Scenario: Render Docker source variables
- **WHEN** an archive backup policy includes a Docker container source with known container or Compose metadata
- **THEN** VaultFleet makes `{{container_name}}`, `{{compose_project}}`, and `{{compose_service}}` available to archive output templates

#### Scenario: Reject unknown variables
- **WHEN** an operator saves or previews an archive template containing an unsupported token such as `{{hostname}}`
- **THEN** VaultFleet rejects the template with a clear validation error naming the unsupported token

### Requirement: Validate archive output paths safely
VaultFleet SHALL validate rendered archive output paths so an archive backup cannot write outside the configured repository prefix or create unsafe object keys.

#### Scenario: Reject path traversal
- **WHEN** an operator saves or previews an archive remote directory or filename template that renders to a path containing `..` segments
- **THEN** VaultFleet rejects the template with a path safety validation error

#### Scenario: Reject invalid filename
- **WHEN** an archive filename template renders to an empty filename or a filename containing path separators
- **THEN** VaultFleet rejects the template before backup execution

#### Scenario: Sanitize variable values
- **WHEN** a variable value contains characters unsafe for archive object keys
- **THEN** VaultFleet replaces unsafe characters in that variable value with `_` before composing the final remote path

### Requirement: Preview archive output templates
VaultFleet SHALL provide a server-side preview of archive output templates for archive-mode policy configuration.

#### Scenario: Preview rendered archive path
- **WHEN** an operator enters archive output templates in the policy form
- **THEN** VaultFleet returns a preview containing the rendered remote directory, artifact filename, full remote artifact path, rendered variables, and any warnings

#### Scenario: Show default preview
- **WHEN** an operator selects archive mode without entering custom templates
- **THEN** the policy form shows a preview using the default `artifacts/backup-{{datetime}}.{{ext}}` output

#### Scenario: Warn about non-unique filenames
- **WHEN** an archive filename template does not include a time-varying token such as `{{datetime}}`, `{{date}}`, or `{{time}}`
- **THEN** VaultFleet returns a preview warning that future backups may overwrite or collide with prior artifacts

### Requirement: Upload and record templated archive artifacts
VaultFleet SHALL upload archive artifacts to the rendered relative remote path and record the rendered artifact metadata in task history.

#### Scenario: Upload to templated path
- **WHEN** an archive backup runs with remote directory template `archives/{{agent_name}}/{{date}}` and filename template `{{context_name}}-{{datetime}}.{{ext}}`
- **THEN** the Agent uploads the generated archive under the rendered relative remote path within the configured repository

#### Scenario: Record rendered artifact path
- **WHEN** a templated archive backup succeeds
- **THEN** VaultFleet records the rendered artifact name, relative artifact path, content type, size, backup mode, and archive format in task history

#### Scenario: Download templated artifact
- **WHEN** an operator downloads a completed archive backup whose artifact path was produced from templates
- **THEN** VaultFleet fetches the artifact from the stored rendered artifact path rather than assuming the default `artifacts/` directory
