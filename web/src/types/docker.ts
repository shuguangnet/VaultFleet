export interface DockerMount {
  type: string;
  name?: string;
  source: string;
  destination: string;
  rw: boolean;
}

export interface DockerPort {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export interface DockerContainer {
  id: string;
  name: string;
  image: string;
  status: string;
  mounts?: DockerMount[];
  ports?: DockerPort[];
  labels?: Record<string, string>;
  env?: Record<string, string>;
  compose_project?: string;
  compose_service?: string;
  compose_file?: string;
  working_dir?: string;
}

export interface DockerDiscoverResponse {
  agent_id: string;
  containers: DockerContainer[];
  warnings?: string[];
}

export interface DockerBackupProfileRequest {
  storage_id: string;
  repo_path?: string;
  restic_password?: string;
  containers: DockerContainer[];
  exclude_patterns?: string[];
  schedule?: string;
  retention?: Record<string, number>;
  timeout_hours?: number;
  run_now?: boolean;
}

export interface DockerBackupProfileResponse {
  policy_id: string;
  backup_dirs: string[];
  backup_command?: {
    command_id: string;
    message_id: string;
  };
}

export interface DockerRestoreRequest {
  snapshot_id: string;
  target_path: string;
  include_paths?: string[];
  manifest_path?: string;
  precheck_only?: boolean;
  start_containers?: boolean;
  startup_command?: string;
  command_timeout_seconds?: number;
}
