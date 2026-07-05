export type BackupMode = "snapshot" | "archive";
export type ArchiveFormat = "tar.gz" | "zip";

export interface PolicyHook {
  command: string;
  timeout_seconds?: number;
}

export interface BackupPolicy {
  id: string;
  agent_id: string;
  storage_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  repo_path: string;
  backup_dirs: string[];
  exclude_patterns: string[];
  pre_backup_hook?: PolicyHook;
  post_backup_hook?: PolicyHook;
  schedule: string;
  retention: RetentionConfig;
  synced: boolean;
  created_at: string;
  updated_at: string;
  restic_password?: string;
  rclone_args?: Record<string, string>;
  timeout_hours?: number;
  backup_sources?: BackupSource[];
}

export interface RetentionConfig {
  keep_last?: number;
  keep_daily?: number;
  keep_weekly?: number;
  keep_monthly?: number;
  keep_yearly?: number;
}

export interface PolicyInput {
  agent_id: string;
  storage_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  repo_path: string;
  restic_password?: string;
  backup_dirs: string[];
  exclude_patterns: string[];
  pre_backup_hook?: PolicyHook;
  post_backup_hook?: PolicyHook;
  schedule: string;
  retention: RetentionConfig;
  rclone_args?: Record<string, string>;
  timeout_hours?: number;
  backup_sources?: BackupSource[];
}

export type BackupSourceType = "path" | "docker_container";

export interface BackupSource {
  type: BackupSourceType;
  path?: string;
  docker_container?: DockerContainerBackupSource;
}

export interface DockerContainerBackupSource {
  container_id?: string;
  name?: string;
  image?: string;
  labels?: Record<string, string>;
  compose_project?: string;
  compose_service?: string;
  compose_working_dir?: string;
  compose_config_files?: string[];
  include_bind_mounts: boolean;
  include_volumes: boolean;
  include_compose_files: boolean;
}
