export interface ApiResponse<T> {
  ok: boolean;
  data?: T;
  error?: string;
}

export interface AuthUser {
  id?: string;
  username: string;
  role?: UserRole;
  permissions?: string[];
}

export interface AuthCheck {
  authenticated: boolean;
  initialized: boolean;
  username?: string;
  role?: UserRole;
  permissions?: string[];
  user?: AuthUser;
}

export interface AuthCredentials {
  username: string;
  password?: string;
}

export type UserRole = "admin" | "operator" | "viewer";

export interface UserAccount {
  id: string;
  username: string;
  role: UserRole;
  disabled_at?: string | null;
  last_login_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ApiToken {
  id: string;
  name: string;
  token_prefix: string;
  owner_user_id: string;
  role: UserRole;
  scopes: string[];
  expires_at?: string | null;
  revoked_at?: string | null;
  last_used_at?: string | null;
  created_at: string;
  updated_at: string;
  token?: string;
}

export interface AuditEvent {
  id: string;
  actor_type: string;
  actor_id?: string;
  actor_name?: string;
  actor_role?: string;
  token_id?: string;
  action: string;
  target_type?: string;
  target_id?: string;
  result: string;
  message?: string;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
}

export interface BrowseRequest {
  path: string;
  depth?: number;
}

export interface BrowseEntry {
  path: string;
  type: "file" | "dir";
  size: number;
}

export interface BrowseResponse {
  path: string;
  entries: BrowseEntry[];
}

export interface DirSizeRequest {
  path: string;
}

export interface DirSizeResponse {
  path: string;
  size: number;
  error?: string;
}

export interface DockerDiscoveryResponse {
  available: boolean;
  error?: string;
  containers: DockerContainer[];
}

export interface DatabaseDiscoveryResponse {
  available: boolean;
  error?: string;
  databases: string[];
}

export interface DockerContainer {
  id: string;
  names: string[];
  image: string;
  state: string;
  labels?: Record<string, string>;
  compose?: DockerComposeInfo;
  mounts: DockerMount[];
  selectable: boolean;
  warnings?: string[];
}

export interface DockerComposeInfo {
  project?: string;
  service?: string;
  working_dir?: string;
  config_files?: string[];
}

export interface DockerMount {
  type: string;
  name?: string;
  source?: string;
  destination: string;
  rw: boolean;
}
