# Backup Content Manifest

VaultFleet writes a non-secret backup manifest named `VAULTFLEET-MANIFEST.json` for new backup runs.

## Where It Appears

- Archive backups include `VAULTFLEET-MANIFEST.json` at the archive root.
- Snapshot backups include `VAULTFLEET-MANIFEST.json` through an Agent-managed staging path that is passed to the snapshot runner.
- Master stores the manifest JSON in task history and returns it through task APIs so operators can inspect backup contents without downloading the artifact.

## Fields

The manifest is versioned JSON. Version `1` includes:

- `version`: manifest schema version.
- `generated_at`: Agent-side generation time.
- `backup_mode` and `archive_format`: effective backup mode and archive format.
- `agent`: Agent ID, Agent version, and hostname when available.
- `policy`: non-secret policy summary such as storage type and repository path.
- `sources.paths`: effective filesystem paths included in the run.
- `sources.docker`: container name or ID, image, Compose project/service, Compose config files, selected mounts, and resolved paths.
- `sources.databases`: database engine, execution mode, database name or all-database mode, dump output name, compression flag, dump size, and container name when applicable.
- `exclude_patterns`: effective exclude patterns.
- `artifact`: archive artifact name, relative path, format, content type, and size when available.
- `warnings`: bounded warning summaries from source resolution or dump generation.

## Intentional Exclusions

The manifest must not contain storage credentials, rclone configuration values, restic passwords, database passwords, Docker environment values, hook command output, API tokens, or private command output.

## Follow-Ups

P1:

- Add policy `context_name` / `site_name`.
- Add manifest preview before running a backup.
- Add UI actions to view or download the raw manifest JSON.
- Keep the full manifest JSON visible in task history without opening an artifact.

P2:

- Add bounded integrity summaries such as file count, total size, and dump checksums.
- Let the restore wizard read manifests and recommend restore targets.
- Show explicit compatibility messaging for older backups that do not have a manifest.
