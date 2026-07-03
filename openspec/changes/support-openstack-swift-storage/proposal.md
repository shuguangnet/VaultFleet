# Support OpenStack Swift Storage

## Why

VaultFleet can pass arbitrary rclone backends through the API, but the UI and path handling only model S3 buckets explicitly. Operators using OpenStack Swift have to guess the `swift` configuration in advanced mode, and container paths are not handled consistently for connection tests or policy repository targets.

## What Changes

- Add OpenStack Swift as a first-class storage option in the Web UI.
- Treat Swift `container` like S3 `bucket`: keep it out of `rclone.conf` and use it in the rclone target path.
- Ensure storage connection tests verify the configured Swift container when one is provided.
- Ensure policy payloads sent to Agents build restic repository paths under the Swift container.
- Add focused backend and frontend tests for Swift/OpenStack behavior.

## Capabilities

### New Capabilities

- `openstack-swift-storage`: Configure and use OpenStack Swift storage for backup repositories.

### Modified Capabilities

None.

## Impact

- Backend storage connection testing in `internal/master/storagecheck`.
- Policy payload assembly in `internal/master/api`.
- Storage configuration UI in `web/src/pages/storage`.
- Tests covering rclone target construction, policy payloads, and UI templates.
