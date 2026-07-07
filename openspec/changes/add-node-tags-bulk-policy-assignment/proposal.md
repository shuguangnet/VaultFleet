## Why

VaultFleet already handles multi-node backup operations, but operators still need to repeat node selection and policy creation one node at a time. In larger VPS or OpenStack-style fleet environments, node grouping and batch rollout are the fastest way to reduce setup time and avoid inconsistent policy coverage.

## What Changes

- Add node tags so operators can group nodes by environment, region, workload, tenant, or any other operational dimension.
- Allow node lists to be filtered by tag.
- Add policy bulk assignment that creates per-node policy instances from one request, targeting explicitly selected nodes and/or nodes matched by tags.
- Include per-target success/failure results so partial failures are visible and retryable.
- Preserve the existing one-policy-per-agent execution model; no Agent protocol breaking change is required.

## Capabilities

### New Capabilities
- `node-tags-bulk-operations`: Node tagging, tag filtering, and policy bulk assignment behavior.

### Modified Capabilities
- None.

## Impact

- Database: store node tags and continue using per-node `backup_policies` rows for execution.
- Backend API: add endpoints for updating node tags, listing known tags, and creating policy copies for multiple target nodes.
- Frontend: add tag display/editing on nodes and bulk assignment controls in the policy workflow.
- Authorization/audit: restrict tag and bulk policy mutations to existing policy/node management permissions and audit sensitive batch changes.
- Documentation/tests: document operational usage and cover validation, partial failure, and UI service behavior.
