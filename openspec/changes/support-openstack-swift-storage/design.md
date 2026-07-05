# Design

## Context

VaultFleet stores backend credentials as rclone config and sends them to Agents, which write `rclone.conf` and run restic through `rclone serve restic`. S3 has special handling because the UI captures `bucket` with the credential fields, while rclone expects the bucket in the remote path instead of the remote config. OpenStack Swift has the same shape for `container`: it belongs in paths such as `vaultfleet:container/repo`, not in `[vaultfleet]` as a Swift config key.

## Goals / Non-Goals

**Goals:**

- Provide a Web UI template for OpenStack Swift.
- Reuse the existing rclone backend pipeline without adding a storage SDK.
- Keep Swift `container` out of generated rclone configuration.
- Use the configured Swift container for connection tests and policy repository paths.

**Non-Goals:**

- Supporting every Swift authentication mode as separate custom UI forms.
- Migrating existing manually configured `other` storage entries.
- Changing Agent protocol fields or the database schema.

## Decisions

1. Model OpenStack as rclone `swift`.
   - Rationale: rclone already owns Swift authentication and transfer behavior, and the Agent path is backend-agnostic.
   - Alternative considered: add a dedicated OpenStack SDK path. This would duplicate rclone behavior and bypass the existing restic integration.

2. Treat `container` as a path segment, matching existing S3 `bucket` behavior.
   - Rationale: keeps rclone config valid and lets connection tests verify the actual container that will hold repositories.
   - Alternative considered: require operators to place the container in the policy repository path. That makes the UI error-prone and inconsistent with S3.

3. Keep path handling backend-specific and small.
   - Rationale: only S3 and Swift currently need config-to-path extraction; other rclone backends already use `repo_path` directly.
   - Alternative considered: introduce a generic `path_key` map across all backends. That is more abstraction than the current code needs.

## Risks / Trade-offs

- Existing Swift configs created in advanced mode with `container` inside rclone config may start routing repository paths under that container after upgrade. Mitigation: this matches the intended OpenStack behavior and avoids invalid rclone config lines.
- Swift deployments vary by auth version and domain fields. Mitigation: expose the common v3 fields in template mode and leave advanced mode available for additional rclone keys.
