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
  created_at: string;
  updated_at?: string;
}

export interface TaskFilters {
  agent_id?: string;
  type?: string;
  status?: string;
  limit?: number;
}
