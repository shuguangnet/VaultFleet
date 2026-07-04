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
  type: "backup" | "restore";
  status: "pending" | "running" | "success" | "failed" | "timeout" | "cancelled";
  snapshot_id?: string;
  command_id?: string;
  policy_id?: string;
  storage_id?: string;
  started_at?: string;
  finished_at?: string;
  repo_size?: number;
  duration_ms?: number;
  error_log?: string;
  progress?: BackupProgress;
  docker?: DockerBackupMetadata;
  created_at: string;
  updated_at?: string;
}

export interface DockerBackupMetadata {
  sources?: DockerResolvedSource[];
  warnings?: string[];
}

export interface DockerResolvedSource {
  container_id?: string;
  name?: string;
  image?: string;
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
