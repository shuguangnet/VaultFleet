# Contributing to VaultFleet

Thanks for helping improve VaultFleet. This project manages backups, credentials, and restore workflows, so changes should be easy to review and conservative in scope.

## Before You Start

- Open an issue for larger behavior changes before sending a pull request.
- Keep pull requests focused on one problem.
- Do not include real tokens, storage credentials, database files, or private endpoints in issues, logs, screenshots, commits, or tests.
- For security vulnerabilities, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## Development Setup

Required tools:

- Go version from `go.mod`
- Node.js and npm for the Web UI
- Docker when building the Master image

Useful commands:

```bash
# Go tests
make test

# Frontend dependencies and checks
cd web
npm install
npm run build
npm run test

# Binaries
make build-master
make build-agent
make build-all

# Docker image
make docker-build
```

## Pull Request Checklist

- Run the relevant tests before submitting.
- Update `README.md`, `README.en.md`, or files under `docs/` when user-facing behavior changes.
- Add or update tests for behavior changes.
- Keep screenshots and generated assets out of the diff unless they are intentionally part of the documentation update.
- Explain operational impact, migration steps, and rollback notes when changing backup, restore, storage, or Agent enrollment behavior.

## Documentation Style

- Prefer precise operational wording over marketing claims.
- Use HTTPS examples for public deployments; mention HTTP only for local or trusted LAN testing.
- Describe trust boundaries explicitly when documenting credentials, encryption, restore, and diagnostics.
- Link only tracked public files from README.
