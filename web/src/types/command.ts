export type CommandType = "backup_now" | "restore_req" | "selective_restore_req" | "policy_push" | "snapshot_list_req" | "update_agent";
export type CommandStatus = "pending" | "dispatched" | "running" | "succeeded" | "failed" | "timeout";

export interface AgentCommand {
  id: string;
  agent_id: string;
  type: CommandType;
  status: CommandStatus;
  message_id: string;
  result?: string;
  error_message?: string;
  attempts: number;
  policy_id?: string;
  storage_id?: string;
  deadline_at?: string;
  dispatched_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CommandFilters {
  status?: string;
  type?: string;
  limit?: number;
}
