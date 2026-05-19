export interface Agent {
  id: string;
  name: string;
  status: "online" | "offline";
  last_seen: string;
  version: string;
  hostname: string;
  os: string;
  arch: string;
  created_at: string;
}

export interface CreateAgentResponse {
  id: string;
  enroll_token: string;
}
