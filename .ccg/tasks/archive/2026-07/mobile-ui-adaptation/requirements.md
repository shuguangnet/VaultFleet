# Mobile UI Adaptation Requirements

## Goal

Adapt the VaultFleet Web UI so the main console remains usable on phone-sized
viewports, especially 320px-767px widths.

## Scope

- React/Vite/Ant Design frontend under `web/src`.
- App shell navigation, content padding, page headers, action groups, tables,
  forms, drawers, and confirmation dialogs.
- High-frequency pages: dashboard, nodes, node detail, storage, policies,
  tasks, snapshots, notifications, and system.

## Requirements

- Mobile navigation must not reserve desktop sidebar width; users must be able
  to access all primary sections from a touch-friendly menu.
- Core actions must remain reachable on mobile, including add/create, refresh,
  manual backup, restore, test, edit, delete, and save.
- Tables must avoid page-level horizontal overflow; data-heavy tables may scroll
  inside their card and hide lower-priority columns at mobile breakpoints.
- Drawers and modals must fit within the viewport and use mobile-safe footer
  spacing.
- Form controls and common buttons should be touch-friendly on phones.
- Existing desktop behavior should remain intact.

## Constraints

- Respect the current `frontend-antd-refactor` worktree and do not revert
  unrelated changes.
- Use the existing Ant Design direction rather than reintroducing removed
  shadcn/ui components.
- Keep changes focused on responsive behavior and mobile usability.
- External dual-model tooling was unavailable in this environment because
  `~/.claude/bin/codeagent-wrapper` does not exist; document this in review.
