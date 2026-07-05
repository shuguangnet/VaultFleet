# Mobile UI Adaptation Plan

## Complexity

L+ / medium risk. The change spans the application shell and multiple frontend
pages, but does not change backend behavior or API contracts.

## Implementation Plan

1. Add responsive global CSS utilities for page layout, headers, actions,
   table cards, drawers, modals, and touch targets.
2. Update `AppLayout` to use the desktop sidebar only on larger screens and a
   Drawer-based mobile menu on phones.
3. Add responsive classes, `scroll.x`, mobile column visibility, and viewport
   safe Drawer widths to data-heavy pages.
4. Tighten component-level mobile behavior for confirmation dialogs,
   key-value editing, and install commands.
5. Run frontend build/tests and verify rendered mobile/desktop views with a
   local dev server and browser screenshots.
6. Record review findings, archive the CCG task, and summarize the result.

## Acceptance Criteria

- At 375px width, main pages render without body-level horizontal overflow.
- Mobile header exposes navigation and account actions.
- Tables remain usable through card-contained horizontal scrolling.
- Drawers are full-width or viewport-constrained on mobile.
- `cd web && npm run build` succeeds.
- Relevant tests pass or any failures are documented with cause.
