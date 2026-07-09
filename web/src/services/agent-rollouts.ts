import type { AgentRollout, AgentRolloutCreateRequest } from "@/types/agent-rollout";
import { apiGet, apiPost } from "./http";

export const listAgentRollouts = () =>
  apiGet<AgentRollout[]>("/api/agent-upgrade-rollouts");

export const getAgentRollout = (id: string) =>
  apiGet<AgentRollout>(`/api/agent-upgrade-rollouts/${id}`);

export const createAgentRollout = (body: AgentRolloutCreateRequest) =>
  apiPost<AgentRollout>("/api/agent-upgrade-rollouts", body);

export const cancelAgentRollout = (id: string, reason?: string) =>
  apiPost<{ id: string; status: string }>(`/api/agent-upgrade-rollouts/${id}/cancel`, { reason });
