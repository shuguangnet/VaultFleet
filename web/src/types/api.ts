export interface ApiResponse<T> {
  ok: boolean;
  data?: T;
  error?: string;
}

export interface AuthUser {
  username: string;
}

export interface AuthCheck {
  authenticated: boolean;
  initialized: boolean;
  user?: AuthUser;
}

export interface AuthCredentials {
  username: string;
  password?: string;
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
