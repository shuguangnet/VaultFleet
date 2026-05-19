export interface TaskHistory {
  id: number;
  message_id: string;
  agent_id: string;
  type: "backup" | "restore";
  status: "running" | "success" | "failed";
  snapshot_id?: string;
  error_log?: string;
  created_at: string;
  finished_at?: string;
  repo_size?: number;
  duration_ms?: number;
}

export interface TaskFilters {
  agent_id?: string;
  type?: string;
  status?: string;
  limit?: number;
}
