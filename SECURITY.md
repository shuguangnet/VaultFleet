# Security Policy

VaultFleet coordinates backups across multiple servers and stores sensitive configuration. Treat the Master, its `/data` directory, Agent tokens, restic passwords, rclone credentials, and diagnostic bundles as sensitive.

## Reporting a Vulnerability

Please do not open a public issue for vulnerabilities.

Use GitHub's private vulnerability reporting flow for this repository when available. If private reporting is not available, open a minimal public issue that says you need a private security contact and do not include exploit details, tokens, logs, database files, or credentials.

## Trust Model

- Agents connect outbound to the Master and storage backend.
- Backup data is written directly by Agents to storage using restic and rclone; backup data is not relayed through the Master.
- restic encrypts repository data, but the Master stores and distributes the restic repository password needed by Agents.
- Master-side secrets are encrypted at rest using `/data/master.key`.
- Anyone with control of the Master host, `/data/vaultfleet.db`, and `/data/master.key` should be treated as able to access Master-side secrets and control backup or restore jobs.
- Production deployments should put the Master behind HTTPS/WSS and restrict administrative access.

## Redaction Rules

Before sharing logs, screenshots, diagnostic bundles, or configuration snippets, remove:

- enrollment tokens such as `ek_xxx`
- Agent tokens
- login cookies
- restic passwords
- rclone access keys and secret keys
- WebDAV, SFTP, object storage, Telegram, and webhook credentials
- private endpoints, internal hostnames, IP addresses, and filesystem paths when they reveal sensitive infrastructure

Never upload:

- `/data/master.key`
- full `/data/vaultfleet.db`
- full `/etc/vaultfleet/agent.yaml`
- full rclone configuration files containing credentials
