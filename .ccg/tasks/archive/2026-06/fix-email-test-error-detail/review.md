# Review

## Completed

- Changed notification test-send API errors to include a safe `detail` field with the underlying notifier error.
- Updated the web HTTP client to include `detail` in `ApiError.message`, so the notification drawer toast can show why SMTP sending failed.
- Added backend coverage proving test-send details are returned without leaking the saved SMTP password.
- Added frontend HTTP client coverage for `{ error, detail }` responses.

## Checks

- `go test ./internal/master/api ./internal/master/notify`: passed
- `npm test -- src/services/http.test.ts`: passed
- `npm run build`: passed
- `git diff --check`: passed

## Notes

- The CCG external dual-model review wrapper is unavailable in this environment (`~/.claude/bin/codeagent-wrapper` is missing), so review was limited to local tests and diff inspection.
- Existing full frontend test instability remains outside this change: nodes/snapshots page tests can fail while stuck in loading state.
