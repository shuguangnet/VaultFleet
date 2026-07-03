import {
  DockerBackupProfileRequest,
  DockerBackupProfileResponse,
  DockerDiscoverResponse,
  DockerRestoreRequest,
} from "@/types/docker";
import { RestoreAccepted } from "@/types/snapshot";
import { apiPost } from "./http";

export const discoverDocker = (agentId: string) =>
  apiPost<DockerDiscoverResponse>(`/api/agents/${agentId}/docker/discover`, { all: false });

export const createDockerBackupProfile = (agentId: string, body: DockerBackupProfileRequest) =>
  apiPost<DockerBackupProfileResponse>(`/api/agents/${agentId}/docker/backup-profile`, body);

export const restoreDockerSnapshot = (agentId: string, body: DockerRestoreRequest) =>
  apiPost<RestoreAccepted>(`/api/agents/${agentId}/docker/restore`, body);
