## 1. Data Model And Migration

- [x] 1.1 Extend `db.User` with role, disabled timestamp, and last-login timestamp fields.
- [x] 1.2 Add `APIToken` and `AuditEvent` database models with indexes for lookup, filtering, and revocation checks.
- [x] 1.3 Add migration/backfill logic so existing users become enabled `admin` users.
- [x] 1.4 Add database tests for user role defaults, existing-user backfill, token persistence, and audit event persistence.

## 2. Actor Authentication

- [x] 2.1 Extend session data and auth responses to include user role and enabled/disabled status validation.
- [x] 2.2 Update setup and login flows so initial users are created as `admin`, disabled users cannot log in, and `last_login_at` is tracked.
- [x] 2.3 Implement API token generation, parsing, hashing, verification, expiration, revocation, and last-used tracking utilities.
- [x] 2.4 Update authentication middleware to accept either a valid session cookie or `Authorization: Bearer` API token and populate actor context.
- [x] 2.5 Add auth tests for role-bearing sessions, disabled active sessions, valid tokens, revoked tokens, expired tokens, malformed bearer headers, and agent-token isolation.

## 3. Authorization And Permissions

- [x] 3.1 Define built-in roles, permission constants, API token scopes, and effective-permission calculation.
- [x] 3.2 Add authorization middleware that rejects unauthorized actors with consistent forbidden responses.
- [x] 3.3 Apply route-level permissions across agents, storage, policies, tasks, snapshots, restore, notifications, system, diagnostics, users, tokens, and audit routes.
- [x] 3.4 Add router coverage tests that verify representative `admin`, `operator`, `viewer`, and scoped-token access for read, write, backup, restore, system, user, and token routes.
- [x] 3.5 Add response redaction tests for non-admin storage, notification, install-token, diagnostics, and artifact-sensitive paths.

## 4. User And Token APIs

- [x] 4.1 Add user management endpoints for list, create, update role, disable, enable, delete, and admin password reset.
- [x] 4.2 Add current-user endpoints for reading actor profile and changing the caller's own password.
- [x] 4.3 Add API token endpoints for list, create with one-time plaintext return, revoke, and delete.
- [x] 4.4 Enforce role and scope boundaries for token creation so tokens cannot exceed the assigned role or allowed actor capabilities.
- [x] 4.5 Add API tests for user lifecycle, token lifecycle, one-time token display, permission failures, and validation errors.

## 5. Audit Logging

- [x] 5.1 Implement an audit recorder that captures actor metadata, target metadata, action, result, timestamp, IP address, user agent, and compact messages without request bodies or secrets.
- [x] 5.2 Record audit events for login success/failure, logout, user management, token management, storage changes/tests, policy changes, backup/verify/snapshot/restore/task actions, diagnostics, and system import/export/password changes.
- [x] 5.3 Add audit list endpoint with filters for actor, action, target, result, and time range.
- [x] 5.4 Add tests proving successful actions, denied actions, and validation failures create audit events without sensitive values.

## 6. Web UI

- [x] 6.1 Extend frontend auth types and auth check handling to include role and effective permissions.
- [x] 6.2 Add permission helpers and use them to hide or disable unauthorized controls across dashboard, nodes, storage, policies, tasks, snapshots, notifications, and system pages.
- [x] 6.3 Add user management UI for admins.
- [x] 6.4 Add API token management UI with one-time token reveal and revoke/delete actions.
- [x] 6.5 Add audit log UI with filtering and detail view.
- [x] 6.6 Add frontend tests for viewer read-only behavior, operator admin-control hiding, token creation reveal, and backend error handling.

## 7. Documentation And Validation

- [x] 7.1 Document roles, permission boundaries, API token creation, token security, and audit log behavior in README/docs.
- [x] 7.2 Document migration and rollback notes for existing single-admin deployments.
- [x] 7.3 Run focused Go tests for db, auth, middleware, user/token/audit APIs, and router permissions.
- [x] 7.4 Run focused frontend tests for auth/session, permission gating, user management, token management, and audit UI.
- [x] 7.5 Run `openspec validate add-multi-user-rbac-api-tokens --strict`.
