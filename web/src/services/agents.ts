import { Agent, CreateAgentResponse } from "@/types/agent";
import { BrowseRequest, BrowseResponse } from "@/types/api";
import { apiDelete, apiGet, apiPost } from "./http";

export const listAgents = () => apiGet<Agent[]>("/api/agents");
export const createAgent = (body: { name: string }) => apiPost<CreateAgentResponse>("/api/agents", body);
export const getAgent = (id: string) => apiGet<Agent>(`/api/agents/${id}`);
export const deleteAgent = (id: string) => apiDelete(`/api/agents/${id}`);
export const regenerateAgentToken = (id: string) => apiPost<CreateAgentResponse>(`/api/agents/${id}/regenerate-token`);
export const browseAgent = (id: string, body: BrowseRequest) => apiPost<BrowseResponse>(`/api/agents/${id}/browse`, body);
export const backupNow = (id: string) => apiPost<{ message_id: string }>(`/api/agents/${id}/backup-now`);
