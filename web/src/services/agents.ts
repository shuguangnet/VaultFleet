import { Agent, ApiAgent, CreateAgentResponse } from "@/types/agent";
import { BrowseRequest, BrowseResponse, DatabaseDiscoveryResponse, DirSizeRequest, DirSizeResponse, DockerDiscoveryResponse } from "@/types/api";
import type { DatabaseBackupSource } from "@/types/policy";
import { apiDelete, apiGet, apiPost, apiPut } from "./http";

export const listAgents = async (tagsOrContext?: unknown) => {
  const tags = Array.isArray(tagsOrContext) ? tagsOrContext : [];
  const query = tags.length
    ? `?${tags.map((tag) => `tag=${encodeURIComponent(tag)}`).join("&")}`
    : "";
  return (await apiGet<ApiAgent[]>(`/api/agents${query}`)).map(normalizeAgent);
};
export const listAgentTags = () => apiGet<string[]>("/api/agents/tags");
export const createAgent = (body: { name: string }) => apiPost<CreateAgentResponse>("/api/agents", body);
export const getAgent = async (id: string) => normalizeAgent(await apiGet<ApiAgent>(`/api/agents/${id}`));
export const deleteAgent = (id: string) => apiDelete(`/api/agents/${id}`);
export const updateAgentTags = async (id: string, tags: string[]) => normalizeAgent(await apiPut<ApiAgent>(`/api/agents/${id}/tags`, { tags }));
export const regenerateAgentToken = (id: string) => apiPost<CreateAgentResponse>(`/api/agents/${id}/regenerate-token`);
export const getInstallToken = (id: string) => apiGet<{ id: string; enroll_token: string; enrolled: boolean }>(`/api/agents/${id}/install-token`);
export const browseAgent = (id: string, body: BrowseRequest) => apiPost<BrowseResponse>(`/api/agents/${id}/browse`, body);
export const dirSizeAgent = (id: string, body: DirSizeRequest) => apiPost<DirSizeResponse>(`/api/agents/${id}/dir-size`, body);
export const discoverDockerAgent = (id: string) => apiPost<DockerDiscoveryResponse>(`/api/agents/${id}/docker/discover`);
export const discoverDatabaseAgent = (id: string, source: DatabaseBackupSource) =>
  apiPost<DatabaseDiscoveryResponse>(`/api/agents/${id}/database/discover`, { source });
export const backupNow = (id: string, body?: { policy_id?: string }) =>
  apiPost<{ command_id: string; message_id: string }>(`/api/agents/${id}/backup-now`, body);
export const updateAgent = (id: string, body: { version?: string; github_repo?: string } = {}) =>
  apiPost<{ accepted: boolean; message_id: string; version: string; github_repo?: string }>(`/api/agents/${id}/update-agent`, body);

export function normalizeAgent(agent: ApiAgent): Agent {
  const systemInfo = parseSystemInfo(agent.system_info);
  return {
    id: agent.id,
    name: agent.name,
    status: agent.status,
    tags: agent.tags ?? [],
    last_seen: agent.last_seen ?? agent.last_seen_at ?? "",
    version: agent.version ?? systemInfo.version ?? "",
    hostname: agent.hostname ?? systemInfo.hostname ?? "",
    os: agent.os ?? systemInfo.os ?? "",
    arch: agent.arch ?? systemInfo.arch ?? "",
    capabilities: agent.capabilities ?? systemInfo.capabilities ?? [],
    created_at: agent.created_at,
  };
}

function parseSystemInfo(raw: string | null | undefined): Partial<Pick<Agent, "version" | "hostname" | "os" | "arch" | "capabilities">> {
  if (!raw) {
    return {};
  }
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") {
      return {};
    }
    return {
      version: stringField(parsed, "version"),
      hostname: stringField(parsed, "hostname"),
      os: stringField(parsed, "os"),
      arch: stringField(parsed, "arch"),
      capabilities: stringArrayField(parsed, "capabilities"),
    };
  } catch {
    return {};
  }
}

function stringArrayField(value: object, key: string): string[] | undefined {
  const field = (value as Record<string, unknown>)[key];
  if (!Array.isArray(field)) {
    return undefined;
  }
  return field.filter((item): item is string => typeof item === "string");
}

function stringField(value: object, key: string): string | undefined {
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" ? field : undefined;
}
