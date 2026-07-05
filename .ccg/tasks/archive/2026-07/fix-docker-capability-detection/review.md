# Review

## External model review

CCG dual-model analysis/review could not run because the configured wrapper is missing:

```text
/Users/shuguang/.claude/bin/codeagent-wrapper: no such file or directory
```

Sub-agent tools are available in Codex, but their tool policy requires explicit user authorization for sub-agent delegation, so they were not used as a substitute.

## Local review

### Critical

None found.

### Warning

Full repository tests are still failing in pre-existing flaky/unrelated areas:

- `cmd/master`: `TempDir RemoveAll cleanup: ... directory not empty`
- `internal/agent`: `TestHandleBackupNowUsesInlinePolicyPayloadForArchive` timed out waiting for `task_result`

These failures were observed before this change and still occur after it. The targeted packages affected by this fix pass.

### Info

- `archive_backup` was already defined in the protocol but was not reported by the Agent.
- The fix centralizes current Agent capabilities in `protocol.DefaultAgentCapabilities()`.
- Enrollment system info and runtime heartbeat now share the same capability list.
- Master heartbeat persistence test now covers the full default capability list, including `archive_backup`.

## Verification

Passed:

```bash
go test ./pkg/protocol ./internal/agent/enroll ./cmd/agent ./internal/master/ws -count=1
go test ./internal/master/api ./internal/master/ws ./pkg/protocol ./internal/agent/enroll ./cmd/agent -count=1
make build-agent
git diff --check
```

Failed with unrelated existing failures:

```bash
go test ./... -count=1
```
