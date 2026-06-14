# Review

## Completed

- Added `POST /api/notifications/test` to send a test notification using unsaved form config.
- Added `POST /api/notifications/:id/test-config` to test edited draft config while preserving existing redacted secrets.
- Added tests proving unsaved/draft tests do not persist notification config changes.
- Added a drawer-level "测试当前配置" button in the notification settings UI.
- Updated the OpenSpec notification delta with draft testing requirements.

## Checks

- `go test ./internal/master/api ./internal/master/notify`: passed
- `npm run build`: passed
- `npm test`: passed when rerun alone
- `git diff --check`: passed

## Notes

- A parallel `npm test` + `npm run build` run produced two unrelated UI test timeouts in nodes/snapshots pages. Rerunning `npm test` alone passed all 11 files / 37 tests.
- The CCG external dual-model review wrapper is unavailable in this environment (`~/.claude/bin/codeagent-wrapper` is not present), so review was limited to local tests and diff inspection.
