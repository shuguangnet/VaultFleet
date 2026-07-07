## ADDED Requirements

### Requirement: Initial Admin Compatibility
The system SHALL preserve existing initialized deployments by treating every pre-existing user as an enabled `admin` user after the identity access control migration.

#### Scenario: Existing deployment after migration
- **WHEN** an existing deployment with one or more users starts on the new version
- **THEN** each existing user can log in and receives the `admin` role

#### Scenario: New deployment setup
- **WHEN** the first setup user is created
- **THEN** the system creates that user as an enabled `admin`

### Requirement: User Account Management
The system SHALL allow `admin` users to create, list, update, disable, enable, delete, and reset passwords for human user accounts.

#### Scenario: Admin creates operator
- **WHEN** an `admin` creates a user with role `operator`
- **THEN** the new user can log in with `operator` permissions

#### Scenario: Non-admin cannot manage users
- **WHEN** an `operator`, `viewer`, or API token without user administration permission requests a user management action
- **THEN** the system rejects the request with a forbidden response

#### Scenario: Disabled user login
- **WHEN** a disabled user attempts to log in
- **THEN** the system rejects the login without creating a session

#### Scenario: Disabled active session
- **WHEN** a user is disabled after a session was created
- **THEN** subsequent authenticated requests from that session are rejected

### Requirement: Role-Based Access Control
The system SHALL enforce built-in `admin`, `operator`, and `viewer` roles for all protected Web UI and HTTP API actions.

#### Scenario: Admin full access
- **WHEN** an `admin` requests any protected control-plane action
- **THEN** the system authorizes the action subject to normal validation

#### Scenario: Operator runs backup operation
- **WHEN** an `operator` triggers backup now, verification now, snapshot refresh, restore preflight, restore submit, task cancel, or diagnostics collection
- **THEN** the system authorizes the action subject to normal validation

#### Scenario: Operator blocked from system administration
- **WHEN** an `operator` requests user management, API token administration beyond allowed own-token operations, system import, system export, or import confirmation
- **THEN** the system rejects the request with a forbidden response

#### Scenario: Viewer read-only access
- **WHEN** a `viewer` requests dashboard data, node lists, storage summaries, policies, task history, snapshots, notifications, system version, readiness, or audit logs
- **THEN** the system returns the requested read-only data with sensitive values redacted

#### Scenario: Viewer mutation blocked
- **WHEN** a `viewer` requests any create, update, delete, backup, verification, restore, cancellation, token creation, diagnostic collection, or system import/export action
- **THEN** the system rejects the request with a forbidden response

### Requirement: Sensitive Value Redaction
The system SHALL avoid exposing raw secrets to `operator`, `viewer`, and API-token callers unless the value is the one-time plaintext of a newly created API token.

#### Scenario: Storage read hides credentials
- **WHEN** an `operator`, `viewer`, or API-token caller reads a storage configuration
- **THEN** the response does not include raw rclone secret fields, restic passwords, access keys, or secret keys

#### Scenario: Token shown once
- **WHEN** a user creates an API token
- **THEN** the system returns the plaintext token exactly in the creation response and never returns that plaintext token from later list or detail requests

#### Scenario: Install token restricted
- **WHEN** a non-admin requests an agent install token or regenerated agent token
- **THEN** the system rejects the request unless the route is explicitly permitted for that role

### Requirement: API Token Lifecycle
The system SHALL support scoped API tokens for automation with hashed-at-rest secrets, expiration, revocation, last-used tracking, and role/scope-bounded permissions.

#### Scenario: Admin creates scoped token
- **WHEN** an `admin` creates an API token with role `operator`, scopes, and an expiration time
- **THEN** the system stores only a token hash and returns a one-time plaintext token

#### Scenario: Bearer token authentication
- **WHEN** a request includes `Authorization: Bearer <token>` for a protected API route
- **THEN** the system authenticates the token, loads its role and scopes, updates last-used metadata, and evaluates authorization without requiring a session cookie

#### Scenario: Revoked token rejected
- **WHEN** a request uses a revoked API token
- **THEN** the system rejects the request as unauthorized

#### Scenario: Expired token rejected
- **WHEN** a request uses an expired API token
- **THEN** the system rejects the request as unauthorized

#### Scenario: Token scope denies route
- **WHEN** a valid API token lacks the scope required by the requested route
- **THEN** the system rejects the request with a forbidden response

#### Scenario: Token cannot exceed assigned role
- **WHEN** a valid API token includes a scope that is not permitted by its assigned role
- **THEN** the system treats that scope as ineffective and rejects actions outside the role

### Requirement: Authentication Context
The system SHALL expose the authenticated actor identity, actor type, role, and effective permissions to backend handlers and the Web UI session check response.

#### Scenario: Session check returns role
- **WHEN** a logged-in user calls the auth check endpoint
- **THEN** the response includes the username and role

#### Scenario: Handler receives actor context
- **WHEN** an authenticated user or API token calls a protected route
- **THEN** the handler can read the actor type, user id, username, role, token id if applicable, and effective permissions from the request context

### Requirement: Audit Logging
The system SHALL record audit events for authentication, user administration, API token management, and sensitive backup, restore, storage, policy, notification, diagnostic, and system actions.

#### Scenario: Successful sensitive action audited
- **WHEN** an authenticated actor successfully performs a sensitive action
- **THEN** the system records an audit event with actor metadata, target metadata, action, result, timestamp, IP address, user agent, and a compact message

#### Scenario: Failed sensitive action audited
- **WHEN** an authenticated actor is denied or a sensitive action fails validation after authorization
- **THEN** the system records an audit event with failure result and no secret values

#### Scenario: Audit log readable
- **WHEN** an authorized user requests audit events with filters for actor, action, target, result, or time range
- **THEN** the system returns matching audit events ordered from newest to oldest

#### Scenario: Audit log immutable through UI
- **WHEN** any user or API token attempts to edit or delete an audit event through the application API
- **THEN** the system rejects the request because audit events are append-only

### Requirement: Web UI Permission Awareness
The Web UI SHALL hide or disable actions that the current actor cannot perform while still relying on backend authorization as the source of truth.

#### Scenario: Viewer UI hides mutation actions
- **WHEN** a `viewer` opens operational pages
- **THEN** create, edit, delete, backup, verification, restore, cancel, token, diagnostic, import, and export controls are hidden or disabled

#### Scenario: Operator UI hides admin actions
- **WHEN** an `operator` opens the system area
- **THEN** user management, system import/export, and unrestricted token administration controls are hidden or disabled

#### Scenario: Backend remains authoritative
- **WHEN** a caller manually invokes a hidden or disabled action through HTTP
- **THEN** backend authorization still rejects the request if the actor lacks permission

### Requirement: Agent Authentication Isolation
The system SHALL keep agent enrollment tokens and agent WebSocket tokens separate from human user sessions and API tokens.

#### Scenario: API token cannot enroll agent
- **WHEN** a request uses a human API token to call the agent enrollment endpoint or agent WebSocket endpoint
- **THEN** the system does not treat that token as an agent credential

#### Scenario: Agent token cannot call user API
- **WHEN** a request uses an agent token against protected user API routes
- **THEN** the system does not treat that token as a user or API-token credential
