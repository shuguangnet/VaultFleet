import { TaskFilters, TaskHistory } from "@/types/task";
import { apiGet } from "./http";

export const listTasks = (filters: TaskFilters = {}) => apiGet<TaskHistory[]>(`/api/tasks${toQuery(filters)}`);

function toQuery(filters: TaskFilters): string {
  const params = new URLSearchParams();
  if (filters.agent_id) params.set("agent_id", filters.agent_id);
  if (filters.type) params.set("type", filters.type);
  if (filters.status) params.set("status", filters.status);
  if (filters.limit) params.set("limit", filters.limit.toString());

  const query = params.toString();
  return query ? `?${query}` : "";
}
