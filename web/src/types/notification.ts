export interface NotificationConfig {
  id: string;
  name: string;
  type: "telegram" | "webhook";
  config: Record<string, string>;
  events: string[];
  created_at: string;
  updated_at: string;
}

export interface NotificationInput {
  name: string;
  type: "telegram" | "webhook";
  config: Record<string, string>;
  events: string[];
}
