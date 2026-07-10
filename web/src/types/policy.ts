export type BackupMode = "snapshot" | "archive";
export type ArchiveFormat = "tar.gz" | "zip";

export interface PolicyHook {
  command: string;
  timeout_seconds?: number;
}

export interface BackupPolicy {
  id: string;
  name?: string;
  agent_id: string;
  storage_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  artifact_context_name?: string;
  archive_remote_dir_template?: string;
  archive_name_template?: string;
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
  verification?: BackupVerificationSettings;
  latest_verification?: BackupVerificationSummary;
}

export interface BulkAssignPolicyRequest {
  source_policy_id: string;
  target_agent_ids?: string[];
  target_tags?: string[];
}

export interface BulkAssignPolicyResponse {
  source_policy_id: string;
  target_tags: string[];
  requested_count: number;
  matched_count: number;
  created_count: number;
  failed_count: number;
  results: BulkAssignPolicyResult[];
}

export interface BulkAssignPolicyResult {
  agent_id?: string;
  agent_name?: string;
  policy_id?: string;
  ok: boolean;
  error?: string;
}

export interface RetentionConfig {
  keep_last?: number;
  keep_daily?: number;
  keep_weekly?: number;
  keep_monthly?: number;
  keep_yearly?: number;
}

export interface PolicyInput {
  name?: string;
  agent_id: string;
  storage_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  artifact_context_name?: string;
  archive_remote_dir_template?: string;
  archive_name_template?: string;
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
  verification?: BackupVerificationSettings;
}

export interface ArtifactNamingWarning {
  code?: string;
  message: string;
  source?: string;
}

export interface ArtifactNamingMetadata {
  context_name?: string;
  site_name?: string;
  source_type?: string;
  remote_dir?: string;
  artifact_name?: string;
  artifact_path?: string;
  remote_dir_template?: string;
  name_template?: string;
  variables?: Record<string, string>;
  warnings?: ArtifactNamingWarning[];
  legacy?: boolean;
}

export interface ArtifactNamingPreviewInput {
  policy_id?: string;
  agent_id: string;
  backup_mode: BackupMode;
  archive_format?: ArchiveFormat;
  backup_dirs: string[];
  backup_sources?: BackupSource[];
  artifact_context_name?: string;
  archive_remote_dir_template?: string;
  archive_name_template?: string;
  use_recommended_defaults?: boolean;
}

export interface BackupVerificationSettings {
  enabled: boolean;
  schedule?: string;
  sample_count?: number;
  sample_restore_enabled?: boolean;
  timeout_minutes?: number;
}

export interface BackupVerificationSummary {
  status: "pending" | "running" | "success" | "failed" | "timeout" | "cancelled";
  snapshot_id?: string;
  checked_at?: string;
  task_id?: string;
  error?: string;
}

export type BackupSourceType = "path" | "docker_container" | "database";

export interface BackupSource {
  type: BackupSourceType;
  path?: string;
  docker_container?: DockerContainerBackupSource;
  database?: DatabaseBackupSource;
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

export type DatabaseEngine = "postgresql" | "mysql";
export type DatabaseExecutionMode = "host" | "docker";

export interface DatabaseBackupSource {
  engine: DatabaseEngine;
  execution_mode: DatabaseExecutionMode;
  host?: string;
  port?: number;
  username: string;
  password?: string;
  password_set?: boolean;
  database?: string;
  all_databases?: boolean;
  compress?: boolean;
  output_name?: string;
  extra_args?: string[];
  docker_container?: DockerContainerBackupSource;
  connection_name?: string;
  dump_timeout_seconds?: number;
}
