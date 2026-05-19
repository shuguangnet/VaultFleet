export interface Snapshot {
  id: string;
  time: string;
  paths: string[];
  hostname: string;
  username: string;
  tags?: string[];
}

export interface SnapshotRefreshResponse {
  message_id: string;
}

export interface RestoreRequest {
  snapshot_id: string;
  target_path: string;
}

export interface RestoreAccepted {
  message_id: string;
}
