## 1. Cron Domain Utilities

- [x] 1.1 Define typed visual schedule models for daily, weekly, monthly, interval, and custom modes.
- [x] 1.2 Implement pure functions to generate canonical five-field Cron expressions from visual schedules.
- [x] 1.3 Implement lossless Cron recognition that maps only exactly supported expressions back to visual schedules and otherwise returns custom mode.
- [x] 1.4 Extend schedule descriptions and node-local-time summaries for visual and custom expressions.
- [x] 1.5 Add frontend unit tests for generation, recognition, round trips, weekday ordering, intervals, invalid expressions, and complex-expression preservation.

## 2. Master Validation

- [x] 2.1 Add Master-side schedule validation using the same robfig/cron parser semantics as the Agent scheduler.
- [x] 2.2 Validate schedules on policy creation and update while retaining five-field, six-field, and supported descriptor compatibility.
- [x] 2.3 Normalize and validate the four retention values as non-negative integers and reject configurations where all values are zero.
- [x] 2.4 Add API tests for valid visual output, supported legacy Cron, invalid Cron, negative or fractional retention, and all-zero retention.

## 3. Policy Editor Experience

- [x] 3.1 Replace the primary Cron text field with a schedule mode selector and mode-specific controls for time, weekdays, month day, and interval unit/value.
- [x] 3.2 Add a custom Cron mode with inline validation, readable description, raw expression preservation, and explicit confirmation before replacing a complex expression.
- [x] 3.3 Display “节点本地时间” beside schedule summaries and avoid presenting browser-local timestamps as authoritative execution times.
- [x] 3.4 Refactor retention presets and custom controls with explicit labels, zero-value semantics, union explanation, and inline validation.
- [x] 3.5 Add separate “执行计划” and “保留规则” review summaries before policy submission.
- [x] 3.6 Ensure create, edit, copy, and bulk-assignment workflows preserve the generated schedule and retention values.

## 4. Policy List And Responsive UI

- [x] 4.1 Replace raw-Cron-first policy table content with a concise readable schedule and retain the raw expression as secondary detail or tooltip.
- [x] 4.2 Add readable retention summaries that distinguish recent, daily, weekly, and monthly tiers.
- [x] 4.3 Verify schedule and retention controls on desktop and mobile without clipped weekday controls, overflowing summaries, or nested cards.
- [x] 4.4 Verify both light and dark themes, keyboard navigation, labels, error states, and disabled submit behavior.

## 5. Tests And Documentation

- [x] 5.1 Add policy page interaction tests for each visual mode, custom Cron editing, complex Cron preservation, preset retention, and custom retention.
- [x] 5.2 Add regression tests proving existing API payload shapes and stored policy records require no migration.
- [x] 5.3 Document schedule modes, Agent-local timezone behavior, custom Cron syntax, retention union semantics, and practical examples.
- [x] 5.4 Run focused Go and frontend tests, production frontend build, and browser checks for create/edit policy workflows.
- [x] 5.5 Run `openspec validate improve-policy-scheduling-retention --strict` and resolve all validation errors.
