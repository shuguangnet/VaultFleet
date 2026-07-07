## Why

VaultFleet currently behaves like a single-admin appliance, which is simple but weak for team operations and automation. Multi-user access, role-based permissions, and scoped API tokens are needed so teams can safely let operators inspect backups, run recovery workflows, and integrate automation without sharing the administrator password.

## What Changes

- Add first-class users beyond the initial admin account, including create, update, disable, delete, and password reset flows.
- Add built-in roles for `admin`, `operator`, and `viewer`.
- Enforce role-based permissions for Web UI and API routes.
- Add scoped API tokens for automation with hashed-at-rest secrets, expiration, revocation, last-used tracking, and role/scope restrictions.
- Add audit records for authentication, user management, token management, and sensitive backup/restore/storage operations.
- Preserve existing single-admin installations by migrating the current user to `admin`.
- Keep agent authentication separate from user/API-token authentication.

## Capabilities

### New Capabilities

- `identity-access-control`: Multi-user account management, RBAC enforcement, API tokens, and audit logging for control-plane actions.

### Modified Capabilities

None.

## Impact

- Backend database models and migrations for user roles/status, API tokens, and audit events.
- Authentication middleware and session handling to include user identity and role.
- Authorization middleware for protected API routes and automation token access.
- Web UI changes for user management, API token management, route/action gating, and audit log viewing.
- Tests for auth, authorization, token lifecycle, audit records, and critical route permissions.
- Documentation updates for team access, token handling, and operational security boundaries.
