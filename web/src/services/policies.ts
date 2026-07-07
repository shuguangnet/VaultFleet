import { BackupPolicy, PolicyInput } from "@/types/policy";
import { apiDelete, apiGet, apiPost, apiPut } from "./http";

export const listPolicies = (agentId?: string) =>
  apiGet<BackupPolicy[]>(agentId ? `/api/policies?agent_id=${encodeURIComponent(agentId)}` : "/api/policies");
export const createPolicy = (body: PolicyInput) => apiPost<BackupPolicy>("/api/policies", body);
export const getPolicy = (id: string) => apiGet<BackupPolicy>(`/api/policies/${id}`);
export const updatePolicy = (id: string, body: Partial<PolicyInput>) => apiPut<BackupPolicy>(`/api/policies/${id}`, body);
export const deletePolicy = (id: string) => apiDelete(`/api/policies/${id}`);
export const verifyPolicyNow = (id: string) => apiPost<{ command_id: string; message_id: string }>(`/api/policies/${id}/verify-now`, {});
