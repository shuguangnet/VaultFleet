import { RestoreAccepted, RestoreRequest, Snapshot, SnapshotRefreshResponse } from "@/types/snapshot";
import { apiGet, apiPost } from "./http";

export const listSnapshots = (agentId: string) => apiGet<Snapshot[]>(`/api/agents/${agentId}/snapshots`);
export const refreshSnapshots = (agentId: string) => apiPost<SnapshotRefreshResponse>(`/api/agents/${agentId}/snapshots/refresh`);
export const restoreSnapshot = (agentId: string, body: RestoreRequest) => apiPost<RestoreAccepted>(`/api/agents/${agentId}/restore`, body);
