export interface StorageConfig {
  id: string;
  name: string;
  rclone_type: string;
  rclone_config: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface StorageInput {
  name: string;
  rclone_type: string;
  rclone_config: Record<string, string>;
}
