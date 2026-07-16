import { TaskFilters, TaskHistory, TaskLogQuery, TaskLogResponse } from "@/types/task";
import type { RestoreRetryPlan } from "@/types/snapshot";
import { apiDelete, apiGet, apiPost } from "./http";

export const listTasks = (filters: TaskFilters = {}) => apiGet<TaskHistory[]>(`/api/tasks${toQuery(filters)}`);

export const cancelTask = (taskId: string) => apiPost(`/api/tasks/${taskId}/cancel`, {});
export const deleteTask = (taskId: string) => apiDelete(`/api/tasks/${taskId}`);
export const retryFailedRestore = (taskId: string) => apiPost<RestoreRetryPlan>(`/api/tasks/${taskId}/retry-failed`, {});

export const taskArtifactDownloadUrl = (taskId: string) => `/api/tasks/${taskId}/download`;

export const getTaskLogs = (taskId: string, query: TaskLogQuery = {}) =>
  apiGet<TaskLogResponse>(`/api/tasks/${taskId}/logs${taskLogQuery(query)}`);

function toQuery(filters: TaskFilters): string {
  const params = new URLSearchParams();
  if (filters.agent_id) params.set("agent_id", filters.agent_id);
  if (filters.type) params.set("type", filters.type);
  if (filters.status) params.set("status", filters.status);
  if (filters.limit) params.set("limit", filters.limit.toString());

  const query = params.toString();
  return query ? `?${query}` : "";
}

function taskLogQuery(query: TaskLogQuery): string {
  const params = new URLSearchParams();
  if (query.after !== undefined) params.set("after", query.after.toString());
  if (query.limit) params.set("limit", query.limit.toString());
  const text = params.toString();
  return text ? `?${text}` : "";
}
