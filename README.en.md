# VaultFleet

<!-- markdownlint-disable MD013 -->

> Deploy Agents like probes, manage backups like a centralized backup platform.

**Language:** [中文](README.md) | English

VaultFleet is a centralized backup management system for multiple VPS or Linux servers. It uses a **Master + Agent** architecture: the Master provides the Web UI, API, policy management, task history, snapshots, and notifications; each Agent connects back to the Master, receives backup policies, and writes backup data directly to object storage, WebDAV, SFTP, cloud drives, or other rclone backends through `restic` and `rclone`.

Backup data does not pass through the Master. The Master manages the control plane and metadata; Agents execute jobs and upload data.

![Dashboard](docs/screenshots/dashboard.png)

## Features

- **Outbound-only Agents**: nodes do not need inbound ports.
- **Centralized backup policies** for paths, excludes, cron schedules, retention, task timeout, and storage settings.
- **Web console** for dashboard, nodes, storage, policies, tasks, snapshots, notifications, and system management.
- **One-time enrollment tokens** exchanged for long-lived Agent tokens after enrollment.
- **restic encrypted repositories** with per-Agent repository passwords; Master-side secrets are encrypted with `/data/master.key`.
- **Direct storage writes** through rclone to S3 / R2 / MinIO, WebDAV, SFTP, local paths, or other backends.
- **Docker workload friendly** with support for mounted container data, `docker-compose.yml`, `.env`, and optional pre/post backup hooks for export or service control steps.
- **Backup progress and cancellation** for long-running jobs, with policy-level timeout settings.
- **Snapshot browsing, preflight, and selective restore** for cross-node restore, whole snapshots, or selected paths, with a Web UI preflight gate before execution.
- **Diagnostics and notifications** through Telegram, Webhook, health checks, diagnostic bundles, and Agent log collection.
- **Agent version reporting and self-update** through GitHub Release assets.

## Requirements

| Component | Requirement |
| --- | --- |
| Master | Docker / Docker Compose, or a Linux environment capable of building Go binaries |
| Agent | Linux `amd64` or `arm64`; installer requires root |
| Agent service manager | systemd, OpenRC, or installer `nohup` fallback |
| Source development | Go version from `go.mod`; Web UI uses npm scripts |
| Storage backend | Any backend supported by rclone, including S3, R2, MinIO, WebDAV, SFTP, and local paths |

## Quick Start

### 1. Start The Master

With Docker Compose:

```bash
docker compose up -d
```

This pulls:

```text
ghcr.io/momo-z/vaultfleet:latest
```

The service listens on `http://localhost:8080` and stores persistent data in `./data`:

```text
data/
├── vaultfleet.db
├── master.key
└── rollback/
```

Or run the container directly:

```bash
docker run -d \
  --name vaultfleet \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --restart unless-stopped \
  ghcr.io/momo-z/vaultfleet:latest
```

Initialize the administrator account on first Web UI access.

> For production, use a fixed version tag and expose the Master through HTTPS/WSS. `http://` examples are for local or trusted LAN testing.

### 2. Add A Node And Install The Agent

Create a node from **Nodes** in the Web UI. The Master generates a one-time enrollment token and install command. The Web UI supports three script sources:

- GitHub raw
- GitHub with a proxy prefix
- Master-hosted `/install.sh`

Master-hosted example:

```bash
curl -fsSL https://MASTER_HOST/install.sh | bash -s -- \
  --server https://MASTER_HOST \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx
```

GitHub proxy example:

```bash
curl -fsSL https://MASTER_HOST/install.sh | bash -s -- \
  --server https://MASTER_HOST \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx \
  --github-proxy https://gh-proxy.example.com
```

The installer detects Linux architecture, downloads `vaultfleet-agent`, installs or prepares `restic` and `rclone`, creates `/etc/vaultfleet/`, enrolls the Agent, and starts it with systemd, OpenRC, or `nohup`.

`--agent-url` is an advanced override for a full Agent binary URL, mainly for unpublished builds, private mirrors, internal CDNs, or temporary download sources.

### 3. Uninstall The Agent

```bash
curl -fsSL https://raw.githubusercontent.com/momo-z/VaultFleet/main/build/uninstall.sh | bash
```

This stops the service and removes `vaultfleet-agent`, `restic`, `rclone`, and Agent configuration.

## Typical Workflow

1. Add a storage backend and run the connection test.
2. Create a node, copy the generated install command, and wait for Agent enrollment.
3. Create a backup policy with repository path, backup sources, excludes, cron schedule, retention, and timeout. Sources can be host directories or Docker containers discovered from a Docker-capable Agent.
4. Tune rclone transfer parameters when using WebDAV, AList proxies, or rate-limited storage.
5. For Docker-hosted workloads, back up mounted data directories, bind mounts, `docker-compose.yml`, and `.env`, and use optional hooks when you need logical exports or a brief stop/start window.
6. Track manual backups, scheduled backups, restore jobs, and running backup progress from task history; cancel running jobs when needed.
7. Browse snapshots, select the source node and snapshot, choose a target node, restore mode, target path or Docker source, run restore preflight, then confirm the restore task.
8. For cross-node migration, make sure the new Agent is online and can access the same storage; select that node as the target in the restore drawer instead of creating a matching policy just to reveal old snapshots.

## Docker Workload Backup Notes

Supported scope:

- Mounted container data such as `/srv/app/data` or `/var/lib/postgresql/data`
- Deployment files such as `docker-compose.yml` and `.env`
- Export artifacts produced by pre-backup hooks

Explicit non-goals:

- `docker commit`, `docker save`, and image-layer backup workflows
- Automatic reconstruction of containers, networks, port mappings, or Compose stacks

Recommended pattern:

```bash
# Backup paths
/srv/app/data
/srv/app/docker-compose.yml
/srv/app/.env

# Example pre-backup hook
docker exec db pg_dump -U app app >/srv/app/backup/db.sql

# Example post-backup hook
docker compose start app
```

Operational caveats:

- Hooks run on the agent host, and hook failures fail the backup task.
- For database containers, prefer logical exports or application-aware consistency steps over raw file copies alone.
- If you stop services before backup, ensure your post-backup hook restores service availability.

## Docker Container Backups

Docker backups are configured from **Backup Policies**, not storage settings. A storage `container` / bucket means an object-storage container or bucket, not a Docker container.

When an Agent can access the local Docker Engine API, it reports Docker backup capability and the policy form can list local containers. The Agent process usually needs permission to read `/var/run/docker.sock`; if the Agent itself runs in a container, mount the Docker socket explicitly. Docker socket access is highly privileged, so enable it only on trusted Agent hosts.

When a Docker container is selected, the Agent re-inspects it immediately before the backup runs and resolves bind mounts, named/anonymous volume mountpoints, and discoverable Compose config files into concrete backup paths. VaultFleet does not back up the whole `/var/lib/docker` tree, overlay/image layers, Docker networks, or image contents by default.

Running container files may not be application-consistent. For databases and similar services, use the application's own dump, snapshot, write-quiesce, or pre/post hook workflow to produce consistent files, then back up those paths. Restore by restoring Compose files and data directories first, then recreate containers using the application's normal process.

## Snapshot Restore And Preflight

The Web UI **Snapshots** restore flow makes the restore plan explicit: source node, snapshot, target node, restore mode, target path, selected paths, and Docker source. A preflight check is required before the final restore button is enabled. Preflight validates the current plan only; it does not create an Agent command or task history entry.

Cross-node file restore workflow:

1. Select the source node in **Snapshots** and refresh snapshots.
2. Choose the snapshot to restore, either as a whole snapshot or selected paths from the browser.
3. Pick the target node and target directory in the restore drawer.
4. Run restore preflight to verify that the target Agent is online, supports the required capabilities, and can create or write the target path.
5. After preflight passes, confirm and submit the restore task.

Docker container restore workflow:

1. Open a snapshot with Docker backup metadata and choose **Restore container**.
2. Select the target node and the Docker source from the snapshot.
3. Run preflight to check Docker Engine access on the target Agent, Docker metadata availability, and obvious container or Compose conflicts.
4. Submit the restore after preflight passes. VaultFleet restores the backed-up Compose files and mounted data and, when supported by the Agent, attempts to recreate the container.

Docker restore preflight is a readiness check, not a full topology guarantee. Operators must still verify image availability or registry access, external Docker networks, secrets, external volumes, port conflicts, runtime permissions, database migrations, and application-level consistency. If the target host changes after preflight, the restore task can still fail; task history remains the source of truth for final execution.

Preflight requires the target Agent to advertise the `restore_preflight` capability. If the UI reports that the capability is missing, upgrade the target Agent and wait for it to reconnect to the Master. Older Agents can still receive compatible direct restore API requests, but the guided Web UI blocks submission when runtime checks cannot be completed.

## Architecture

```text
┌──────────────────────────────────────────────┐
│                   Master                      │
│  Web UI / API / SQLite / Policy / Notify      │
└──────────────────────┬───────────────────────┘
                       │ WebSocket control plane
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─────────┐    ┌─────────┐    ┌─────────┐
   │ Agent A │    │ Agent B │    │ Agent C │
   └────┬────┘    └────┬────┘    └────┬────┘
        │              │              │
        └──────────────┼──────────────┘
                       ▼
        S3 / R2 / MinIO / WebDAV / SFTP / rclone backends
```

Design rules:

- The Master manages the control plane only. It does not receive or relay backup data.
- Agents keep a local policy copy, so scheduled backups can continue during temporary Master downtime.
- Each server uses its own repository path and restic password.
- Agents only make outbound connections to the Master and storage backend.

## Security And Trust Boundary

- Expose the Master over HTTPS/WSS in production.
- One-time `ek_` enrollment tokens are exchanged for long-lived Agent tokens.
- Admin passwords are stored as bcrypt hashes.
- Storage credentials, restic passwords, and notification secrets are encrypted in the Master database.
- Keep `/data/master.key` safe; it is required to decrypt Master-side secrets.
- The Master stores and sends the restic repository password required by Agents, so the Master host, administrator account, `vaultfleet.db`, and `master.key` are part of the trust boundary.
- Agent configuration is stored under `/etc/vaultfleet/` and should be readable only by root.

Report security issues through [SECURITY.md](SECURITY.md). Do not post vulnerability details or secrets in public issues.

## Data Export And Restore

The Web UI **System** page can export and import Master data. The exported zip contains Master configuration, metadata, keys, and task records. It does not contain backup data stored in remote restic repositories.

Import requirements:

- The zip must be 100 MB or smaller.
- The zip must contain `vaultfleet.db` and `master.key`.
- After import confirmation, the Master saves the file as `/data/backup.zip` and exits; the container or process manager should restart it so restore can run at startup.
- Pre-restore data is saved under `/data/rollback/`.

## Operational Commands

```bash
# Pull the latest Master image and restart
docker compose pull
docker compose up -d

# Follow Master logs
docker compose logs --tail=200 -f vaultfleet

# Restart or stop Master
docker compose restart vaultfleet
docker compose down
```

Agent operations:

```bash
# systemd
systemctl status vaultfleet-agent
journalctl -u vaultfleet-agent --since "24 hours ago" --no-pager
systemctl restart vaultfleet-agent

# OpenRC
rc-service vaultfleet-agent status
rc-service vaultfleet-agent restart

# fallback mode when systemd / OpenRC is unavailable
tail -n 300 /var/log/vaultfleet-agent.log
```

## Development

```bash
# Go tests
make test

# Build binaries
make build-master
make build-agent
make build-all

# Build Master Docker image
make docker-build
```

Frontend:

```bash
cd web
npm install
npm run build
npm run test
```

## Documentation

- [中文 README](README.md)
- [Protocol reference](docs/protocol.md)
- [Release guide](docs/release.md)
- [Support and log collection guide](docs/support.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Report An Issue

For bugs or troubleshooting support, read the [support and log collection guide](docs/support.md), then open a GitHub Issue:

- [Choose an Issue type](https://github.com/momo-z/VaultFleet/issues/new/choose)
- [Bug report](https://github.com/momo-z/VaultFleet/issues/new?template=bug_report.yml)
- [Support request](https://github.com/momo-z/VaultFleet/issues/new?template=support_request.yml)

Redact tokens, passwords, cookies, rclone credentials, storage secrets, notification credentials, and private endpoints before posting logs.

## License

VaultFleet is released under the [MIT License](LICENSE).

## References

- [Komari Monitor](https://github.com/komari-monitor/komari)
- [Nezha](https://github.com/nezhahq/nezha)
- [restic](https://restic.net/)
- [rclone](https://rclone.org/)

<!-- markdownlint-enable MD013 -->
