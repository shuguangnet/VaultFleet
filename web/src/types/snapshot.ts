import type { DockerBackupMetadata } from "./task";

export interface Snapshot {
  id: string;
  time: string;
  paths: string[];
  hostname: string;
  username: string;
  tags?: string[];
  docker?: DockerBackupMetadata;
}

export interface SnapshotRefreshResponse {
  message_id: string;
  command_id?: string;
  message?: string;
}

export interface RestoreRequest {
  snapshot_id: string;
  source_agent_id?: string;
  target_path?: string;
  include_paths?: string[];
  restore_mode?: "files" | "docker_container";
  docker_source_id?: string;
}

export interface RestoreAccepted {
  message_id: string;
  command_id?: string;
  message?: string;
}

export interface SnapshotFileEntry {
  path: string;
  type: "file" | "dir";
  size: number;
  mtime: string;
}

export interface SnapshotBrowseResponse {
  snapshot_id: string;
  entries: SnapshotFileEntry[];
  error?: string;
}
