import { apiDelete, apiGet, apiPost, apiPut } from "./http";
import type { ApiToken, AuditEvent, UserAccount, UserRole } from "@/types/api";

export const permissions = {
  readOperational: "read:operational",
  writeNodes: "write:nodes",
  writeStorage: "write:storage",
  writePolicies: "write:policies",
  runBackup: "run:backup",
  runRestore: "run:restore",
  writeNotifications: "write:notifications",
  readSystem: "read:system",
  adminSystem: "admin:system",
  adminUsers: "admin:users",
  adminTokens: "admin:tokens",
  readAudit: "read:audit",
} as const;

export const listUsers = () => apiGet<UserAccount[]>("/api/users");
export const createUser = (body: { username: string; password: string; role: UserRole }) =>
  apiPost<UserAccount>("/api/users", body);
export const updateUser = (id: string, body: { username?: string; role?: UserRole }) =>
  apiPut<UserAccount>(`/api/users/${id}`, body);
export const disableUser = (id: string) => apiPost<UserAccount>(`/api/users/${id}/disable`);
export const enableUser = (id: string) => apiPost<UserAccount>(`/api/users/${id}/enable`);
export const resetUserPassword = (id: string, password: string) =>
  apiPost<UserAccount>(`/api/users/${id}/reset-password`, { password });
export const deleteUser = (id: string) => apiDelete(`/api/users/${id}`);

export const listApiTokens = () => apiGet<ApiToken[]>("/api/api-tokens");
export const createApiToken = (body: {
  name: string;
  role: UserRole;
  scopes: string[];
  expires_at?: string | null;
}) => apiPost<ApiToken>("/api/api-tokens", body);
export const revokeApiToken = (id: string) => apiPost<ApiToken>(`/api/api-tokens/${id}/revoke`);
export const deleteApiToken = (id: string) => apiDelete(`/api/api-tokens/${id}`);

export const listAuditEvents = (params: { action?: string; result?: string; limit?: number } = {}) => {
  const query = new URLSearchParams();
  if (params.action) query.set("action", params.action);
  if (params.result) query.set("result", params.result);
  if (params.limit) query.set("limit", String(params.limit));
  const suffix = query.toString();
  return apiGet<AuditEvent[]>(`/api/audit-events${suffix ? `?${suffix}` : ""}`);
};
