# VaultFleet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a multi-VPS centralized backup system with Master + Agent architecture, using restic for backup and rclone for storage.

**Architecture:** Master (Go + Gin + SQLite + Vue 3) manages agents via WebSocket. Agents (Go single binary) register, receive policies, execute restic backups on schedule, and report results. Backup data flows directly from Agent to cloud storage via rclone, never through Master.

**Tech Stack:** Go 1.22+, Gin, GORM, SQLite, gorilla/websocket, robfig/cron/v3, Vue 3, Naive UI, Docker

**Design Spec:** `docs/superpowers/specs/2026-05-18-vaultfleet-design.md`

---

## File Structure

### Master
- `cmd/master/main.go` — entry point, wires all components
- `internal/master/api/auth.go` — login, session, init wizard
- `internal/master/api/middleware.go` — auth + init-required middleware
- `internal/master/api/agents.go` — agent CRUD
- `internal/master/api/enroll.go` — agent enrollment endpoint
- `internal/master/api/storage.go` — storage config CRUD
- `internal/master/api/policy.go` — backup policy CRUD
- `internal/master/api/browse.go` — directory browsing relay
- `internal/master/api/snapshots.go` — snapshot listing + refresh
- `internal/master/api/restore.go` — restore trigger
- `internal/master/api/notifications.go` — notification config CRUD
- `internal/master/api/system.go` — export, password change
- `internal/master/api/router.go` — route assembly
- `internal/master/api/frontend.go` — embedded SPA serving
- `internal/master/db/db.go` — SQLite init + WAL
- `internal/master/db/models.go` — GORM models
- `internal/master/db/crypto.go` — AES-256-GCM + master.key
- `internal/master/ws/safeconn.go` — thread-safe WebSocket wrapper
- `internal/master/ws/hub.go` — connection registry
- `internal/master/ws/handler.go` — WebSocket endpoint + dispatch
- `internal/master/ws/monitor.go` — offline detection
- `internal/master/events/events.go` — in-process event bus
- `internal/master/notify/notify.go` — notifier interface
- `internal/master/notify/telegram.go` — Telegram notifier
- `internal/master/notify/webhook.go` — Webhook notifier
- `internal/master/notify/dispatcher.go` — event→notification routing
- `internal/master/backup/export.go` — zip export
- `internal/master/backup/restore.go` — startup restore from backup.zip

### Agent
- `cmd/agent/main.go` — entry point
- `internal/agent/connect/client.go` — WebSocket client + reconnection
- `internal/agent/connect/heartbeat.go` — periodic heartbeat
- `internal/agent/enroll/enroll.go` — enrollment HTTP call
- `internal/agent/policy/store.go` — local policy + pending results
- `internal/agent/filebrowse/browse.go` — directory scanning
- `internal/agent/executor/rclone.go` — rclone.conf generation
- `internal/agent/executor/restic.go` — restic command builders
- `internal/agent/executor/executor.go` — backup job orchestrator
- `internal/agent/scheduler/scheduler.go` — cron scheduler
- `internal/agent/handler.go` — message dispatcher

### Shared
- `pkg/protocol/message.go` — WebSocket message types + payloads

### Frontend
- `web/` — Vue 3 + Naive UI SPA

### Build
- `build/Dockerfile` — multi-stage Docker build
- `build/install.sh` — agent install script
- `docker-compose.yml`
- `Makefile`

---

## Task Overview (18 Tasks, 4 Phases)

### Phase 1: Foundation (Tasks 1–4) → `2026-05-18-vaultfleet-impl-part1.md`

| Task | Summary | Key Deliverables |
|------|---------|-----------------|
| 1 | Project Scaffolding + Protocol Types | `go.mod`, directory tree, `pkg/protocol/message.go` with 11 message types |
| 2 | Master Database Layer | SQLite+GORM, 7 models, AES-256-GCM crypto, `master.key` |
| 3 | Auth + Init Wizard | Login, session, init wizard, `RequireAuth`/`RequireInit` middleware |
| 4 | Agent Management API | CRUD, enrollment token lifecycle, `POST /api/agent/enroll` |

### Phase 2: Communication (Tasks 5–8) → `2026-05-18-vaultfleet-impl-part2.md`

| Task | Summary | Key Deliverables |
|------|---------|-----------------|
| 5 | Master WebSocket Hub | SafeConn, Hub, Handler, EventBus |
| 6 | Agent Binary + WS Client | WebSocket client with exponential backoff, heartbeat, policy store |
| 7 | Agent Registration Flow (E2E) | Enrollment HTTP call, config save, policy auto-push on connect |
| 8 | Heartbeat + Offline Detection | Monitor goroutine, 60s threshold, MarkOnline/MarkOffline |

### Phase 3: Core Features (Tasks 9–13) → `2026-05-18-vaultfleet-plan-part3.md`

| Task | Summary | Key Deliverables |
|------|---------|-----------------|
| 9 | Storage Config + Backup Policy CRUD | Encrypted rclone_config, auto-generated restic password, synced flag |
| 10 | Directory Browsing | Agent filebrowse with depth limit + excluded paths, Master relay API |
| 11 | Agent Executor (restic + rclone) | rclone.conf generation, restic command builders, RunBackupJob |
| 12 | Agent Scheduler + Backup Execution | cron scheduler, policy_push handler, backup_now handler |
| 13 | Snapshot Management + Restore | Snapshot upsert/list, restore trigger via WebSocket |

### Phase 4: Polish + Deploy (Tasks 14–18) → `2026-05-18-vaultfleet-impl-part4.md`

| Task | Summary | Key Deliverables |
|------|---------|-----------------|
| 14 | Notification System | Telegram + Webhook notifiers, event dispatcher |
| 15 | Master Data Export/Restore | Zip export, startup restore from backup.zip |
| 16 | Master Entry Point + Router | cmd/master/main.go, route assembly, embedded frontend placeholder |
| 17 | Makefile + Docker Build | Makefile, Dockerfile, install.sh, docker-compose.yml |
| 18 | Integration Smoke Test | Full-stack test: init → login → create agent → enroll |

---

## Execution Instructions

Each task follows strict TDD:
1. Write tests → 2. Verify tests fail → 3. Implement → 4. Verify tests pass → 5. Commit

Tasks within a phase can only be executed sequentially (each depends on the prior).
Phases 1–4 must be executed in order.
All test commands use `go test ./... -v` for the final regression check.

---
