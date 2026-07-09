export type AgentRolloutStatus = "pending" | "running" | "succeeded" | "failed" | "cancelled";
export type AgentRolloutItemStatus = "pending" | "running" | "success" | "failed" | "skipped";
export type AgentRolloutPhase = "canary" | "batch" | "";

export interface AgentRolloutCreateRequest {
  target_version?: string;
  github_repo?: string;
  target_tags?: string[];
  target_agent_ids?: string[];
  canary_count?: number;
  batch_size?: number;
}

export interface AgentRolloutItem {
  id: string;
  rollout_id: string;
  agent_id: string;
  agent_name?: string;
  phase?: AgentRolloutPhase;
  batch_index: number;
  status: AgentRolloutItemStatus;
  current_version?: string;
  target_version: string;
  architecture?: string;
  message_id?: string;
  error?: string;
  skip_reason?: string;
  last_seen_version?: string;
  started_at?: string;
  completed_at?: string;
  deadline_at?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentRollout {
  id: string;
  target_version: string;
  github_repo?: string;
  target_tags: string[];
  target_agent_ids: string[];
  canary_count: number;
  batch_size: number;
  status: AgentRolloutStatus;
  failure_reason?: string;
  counts: Record<AgentRolloutItemStatus, number>;
  items?: AgentRolloutItem[];
  created_by_type?: string;
  created_by_id?: string;
  created_by_name?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}
