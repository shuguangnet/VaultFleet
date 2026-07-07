import { AgentCommand, CommandFilters } from "@/types/command";
import type { TaskLogQuery, TaskLogResponse } from "@/types/task";
import { apiGet } from "./http";

export const getCommand = (id: string) => apiGet<AgentCommand>(`/api/commands/${id}`);

export const getCommandLogs = (id: string, query: TaskLogQuery = {}) =>
  apiGet<TaskLogResponse>(`/api/commands/${id}/logs${taskLogQuery(query)}`);

export const listAgentCommands = (agentId: string, filters: CommandFilters = {}) =>
  apiGet<AgentCommand[]>(`/api/agents/${agentId}/commands${toQuery(filters)}`);

function toQuery(filters: CommandFilters): string {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.type) params.set("type", filters.type);
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
