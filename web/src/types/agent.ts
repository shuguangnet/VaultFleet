export interface Agent {
  id: string;
  name: string;
  status: "online" | "offline";
  tags?: string[];
  last_seen: string;
  version: string;
  hostname: string;
  os: string;
  arch: string;
  capabilities?: string[];
  created_at: string;
}

export interface ApiAgent {
  id: string;
  name: string;
  status: "online" | "offline";
  tags?: string[] | null;
  last_seen?: string | null;
  last_seen_at?: string | null;
  version?: string | null;
  hostname?: string | null;
  os?: string | null;
  arch?: string | null;
  capabilities?: string[] | null;
  system_info?: string | null;
  created_at: string;
}

export interface CreateAgentResponse {
  id: string;
  enroll_token: string;
}
