export interface BackupProgress {
  agent_id: string;
  phase: "init" | "backup" | "forget" | "stats";
  percent_done: number;
  total_files: number;
  files_done: number;
  total_bytes: number;
  bytes_done: number;
  bytes_per_sec: number;
  current_file: string;
}

export interface TaskHistory {
  id: string;
  message_id: string;
  agent_id: string;
  type: "backup" | "restore" | "verify";
  status: "pending" | "running" | "success" | "failed" | "timeout" | "cancelled";
  snapshot_id?: string;
  command_id?: string;
  policy_id?: string;
  storage_id?: string;
  backup_mode?: "snapshot" | "archive";
  archive_format?: "tar.gz" | "zip";
  artifact_path?: string;
  artifact_name?: string;
  artifact_size?: number;
  artifact_content_type?: string;
  started_at?: string;
  finished_at?: string;
  repo_size?: number;
  duration_ms?: number;
  error_log?: string;
  progress?: BackupProgress;
  docker?: DockerBackupMetadata;
  database?: DatabaseBackupMetadata;
  verification?: BackupVerificationResult;
  created_at: string;
  updated_at?: string;
}

export interface BackupVerificationResult {
  status: "passed" | "failed";
  snapshot_id?: string;
  checks: BackupVerificationCheck[];
  error?: string;
}

export interface BackupVerificationCheck {
  code: string;
  status: "passed" | "failed" | "skipped";
  severity: "info" | "warning" | "error";
  message: string;
  detail?: string;
  duration_ms?: number;
}

export interface DockerBackupMetadata {
  sources?: DockerResolvedSource[];
  warnings?: string[];
}

export interface DatabaseBackupMetadata {
  dumps?: DatabaseDumpMetadata[];
  warnings?: string[];
}

export interface DatabaseDumpMetadata {
  engine: "postgresql" | "mysql";
  execution_mode: "host" | "docker";
  database?: string;
  all_databases?: boolean;
  container_name?: string;
  output_path?: string;
  output_name?: string;
  size?: number;
  compressed?: boolean;
  warnings?: string[];
}

export interface DockerResolvedSource {
  selection?: unknown;
  container_id?: string;
  name?: string;
  image?: string;
  labels?: Record<string, string>;
  compose?: {
    project?: string;
    service?: string;
    working_dir?: string;
    config_files?: string[];
  };
  mounts?: Array<{
    type: string;
    name?: string;
    source?: string;
    destination: string;
    rw: boolean;
  }>;
  env?: string[];
  cmd?: string[];
  entrypoint?: string[];
  working_dir?: string;
  user?: string;
  ports?: Array<{
    container_port: string;
    protocol?: string;
    host_ip?: string;
    host_port?: string;
  }>;
  restart_policy?: string;
  network_mode?: string;
  state?: string;
  resolved_paths?: string[];
  warnings?: string[];
}

export interface TaskFilters {
  agent_id?: string;
  type?: string;
  status?: string;
  limit?: number;
}

export interface TaskLogLine {
  agent_id: string;
  message_id: string;
  task_type?: string;
  sequence: number;
  timestamp: string;
  level: "info" | "error" | string;
  phase: string;
  stream: "system" | "stdout" | "stderr" | string;
  line: string;
  truncated?: boolean;
}

export type TaskLogStatus =
  | "available"
  | "empty"
  | "missing_message_id"
  | "expired"
  | "unsupported_agent";

export interface TaskLogResponse {
  agent_id: string;
  message_id: string;
  task_id?: string;
  command_id?: string;
  status: TaskLogStatus;
  lines: TaskLogLine[];
  latest_sequence: number;
  truncated: boolean;
  dropped_lines: number;
}

export interface TaskLogQuery {
  after?: number;
  limit?: number;
}
