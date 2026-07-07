## Context

VaultFleet currently has one user table with `username` and `password_hash`, an in-memory session store, and one protected API group guarded by session cookie authentication. This works for single-operator deployments but forces teams to share one administrator password and makes automation depend on browser sessions or high-trust credentials.

This change adds identity and authorization as a control-plane capability. Agent enrollment tokens and long-lived agent tokens remain separate from human/API-token access.

## Goals / Non-Goals

**Goals:**

- Support multiple human users with built-in roles: `admin`, `operator`, and `viewer`.
- Enforce authorization consistently across Web UI actions and HTTP API routes.
- Provide scoped API tokens for automation without exposing administrator passwords.
- Record audit events for security-sensitive actions.
- Preserve current deployments by migrating the existing initialized user to `admin`.

**Non-Goals:**

- External identity providers such as OIDC, LDAP, SAML, or OAuth.
- Per-agent or per-policy custom ACLs.
- Fine-grained dynamic role creation.
- Changing agent authentication, enrollment, or WebSocket trust boundaries.
- Full immutable audit storage with external log shipping.

## Decisions

### Use Built-In Roles With Static Permissions

Roles are static and intentionally coarse:

| Role | Intent | Access |
| --- | --- | --- |
| `admin` | System owner | Full access, including users, API tokens, system import/export, password resets, and destructive configuration changes |
| `operator` | Day-to-day backup operator | Read operational data; create/update nodes, storage, policies, notifications; run backups, verification, snapshot refresh, restore, cancel tasks, collect diagnostics; no user management or system import/export |
| `viewer` | Auditor/read-only observer | Read dashboards, nodes, storage summaries, policies, tasks, snapshots, notifications, system version/readiness, and audit logs; no mutation, restore, backup trigger, token creation, secret reveal, or artifact download |

Rationale: static roles are easier to reason about, test, document, and expose in a small operational product. Custom roles would add a permissions UI, migration complexity, and more support burden before the core team-access value exists.

Alternative considered: scope-only access for every account. Rejected for the first version because it would make the UI and tests much larger while still needing role presets for normal users.

### Add Database Fields Instead of Replacing User Auth

Extend `users` with role and status fields:

- `role`: `admin`, `operator`, or `viewer`
- `disabled_at`: nullable timestamp
- `last_login_at`: nullable timestamp

Existing users are migrated to `admin` when the new columns are added. The setup flow creates the initial user as `admin`.

Rationale: this keeps the current bcrypt password and session model intact while adding authorization state.

Alternative considered: replacing sessions with database-backed sessions. That can be useful later, but it is not required to enforce disabled users if middleware reloads the user on each request.

### Authenticate API Tokens With Bearer Credentials

API tokens use `Authorization: Bearer <token>` and are accepted on protected `/api` routes, not on `/ws/agent` or `/api/agent/enroll`. Tokens are generated once, shown once, and stored hashed at rest.

Token model:

- `id`, `name`, `token_prefix`
- `secret_hash`
- `owner_user_id`, `role`
- `scopes` JSON array
- `expires_at`, `revoked_at`, `last_used_at`
- `created_at`, `updated_at`

Token strings should include a clear token id/prefix plus a random secret so lookup can avoid scanning every hash. The clear prefix is not a credential.

Rationale: scoped bearer tokens are the common shape for automation and work cleanly with curl, CI, and scripts.

Alternative considered: basic auth with username/password. Rejected because it encourages password reuse in automation and cannot be revoked independently.

### Enforce Permissions in Middleware and Route Registration

Authentication should produce an actor context:

- `actor_type`: `user` or `api_token`
- `user_id`, `username`, `role`
- `token_id`, `token_name`, `scopes` when applicable

Authorization middleware checks both role permissions and token scopes. Route groups should make required permissions explicit where routes are registered, for example:

- `read:operational`
- `write:nodes`
- `write:storage`
- `write:policies`
- `run:backup`
- `run:restore`
- `write:notifications`
- `read:system`
- `admin:system`
- `admin:users`
- `admin:tokens`
- `read:audit`

For API tokens, the effective permission is the intersection of token role permissions and token scopes. A token must never exceed the role assigned to it.

Rationale: route-level requirements are visible and testable. Handler-local ad hoc checks are easier to miss.

Alternative considered: checking permissions inside each handler. Rejected because this is a cross-cutting security rule and should be centralized.

### Redact Sensitive Values for Non-Admin Reads

The existing APIs already avoid returning encrypted secrets directly in most cases, but this change should formalize behavior:

- `viewer` reads must never reveal stored secrets, tokens, restic passwords, rclone secret fields, notification credentials, or full install/agent tokens.
- `operator` may use operational credentials indirectly through actions, but responses still avoid raw secret disclosure.
- API tokens are only returned once at creation.

Rationale: read-only access must be safe to grant to auditors or support staff.

### Audit Security-Sensitive Actions

Add audit events for:

- login success/failure and logout
- user create/update/disable/delete/password reset
- API token create/revoke/use failure
- storage create/update/delete/test
- policy create/update/delete/sync-triggered manual actions
- backup now, verify now, snapshot refresh, restore preflight, restore submit, task cancel
- system export/import/confirm and password change

Audit records store actor metadata, target metadata, action, result, request IP, user agent, and a compact message. They must not store request bodies or secrets.

Rationale: RBAC is incomplete without visibility into who performed sensitive operations.

## Risks / Trade-offs

- **Route accidentally left unprotected** -> Define a route permission matrix and add router tests that enumerate protected routes by role and token scope.
- **Disabled user keeps an existing in-memory session** -> Authentication middleware reloads the user on each request and rejects disabled users.
- **API token hash lookup is inefficient** -> Token format includes a non-secret id/prefix so the database can load one token row before verifying the secret hash.
- **Viewer can infer secrets from existing responses** -> Add response-level redaction tests for storage, notifications, install tokens, diagnostics, and task artifacts.
- **Operator permission is too broad for some teams** -> Keep the first version coarse and document boundaries; per-resource ACLs can be a later change.
- **Audit log grows indefinitely** -> Add a configurable retention default or a bounded cleanup task if growth becomes an issue; first version can rely on SQLite size monitoring and admin export.
- **System import can overwrite users/tokens/audit state** -> Restrict import/export to `admin` only and audit both operations.

## Migration Plan

1. Add nullable/defaulted columns to `users` and create `api_tokens` and `audit_events` tables.
2. Backfill all existing users to `role = admin` and enabled status.
3. Update setup/login/session response to include role and disabled-state validation.
4. Add authorization middleware in report-only tests first, then gate routes.
5. Add user, token, and audit APIs plus UI pages.
6. Document role permissions, token handling, and rollback notes.

Rollback requires using a previous binary against the same SQLite data. Older binaries will ignore extra columns/tables, but users created after migration still share the same `users` table shape enough for username/password login only if their password hashes are present. API tokens and role restrictions will not function after rollback.

## Open Questions

- Should `operator` be allowed to delete storage configs and policies, or only create/update and disable? The proposed first version allows delete because current UI treats delete as an operational action, but this is the sharpest operator permission.
- Should audit logs have a built-in retention setting in the first version, or only list/export with later cleanup?
