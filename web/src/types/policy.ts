export type BackupMode = "snapshot" | "archive";
export type ArchiveFormat = "tar.gz" | "zip";

export interface BackupPolicy {
  id: string;
  agent_id: string;
  storage_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  repo_path: string;
  backup_dirs: string[];
  exclude_patterns: string[];
  schedule: string;
  retention: RetentionConfig;
  synced: boolean;
  created_at: string;
  updated_at: string;
  restic_password?: string;
  rclone_args?: Record<string, string>;
  timeout_hours?: number;
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
  schedule: string;
  retention: RetentionConfig;
  rclone_args?: Record<string, string>;
  timeout_hours?: number;
}
