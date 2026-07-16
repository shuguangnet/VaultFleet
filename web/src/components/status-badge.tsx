import { Tag } from "antd";

export type StatusType =
  | "online"
  | "offline"
  | "success"
  | "partial_success"
  | "failed"
  | "running"
  | "syncing"
  | "unsynced"
  | "pending"
  | "timeout"
  | "dispatched"
  | "succeeded"
  | "queued"
  | "cancelled";

interface StatusBadgeProps {
  status: StatusType;
  className?: string;
}

const STATUS_CONFIG: Record<
  StatusType,
  { label: string; color: string }
> = {
  online: { label: "在线", color: "success" },
  offline: { label: "离线", color: "error" },
  success: { label: "已完成", color: "success" },
  partial_success: { label: "部分成功", color: "warning" },
  failed: { label: "失败", color: "error" },
  running: { label: "运行中", color: "processing" },
  syncing: { label: "同步中", color: "processing" },
  unsynced: { label: "待同步", color: "warning" },
  pending: { label: "等待中", color: "warning" },
  timeout: { label: "超时", color: "orange" },
  dispatched: { label: "已下发", color: "blue" },
  succeeded: { label: "成功", color: "success" },
  queued: { label: "已排队", color: "default" },
  cancelled: { label: "已取消", color: "default" },
};

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config = STATUS_CONFIG[status] ?? { label: status, color: "default" };
  return (
    <Tag color={config.color} className={className} style={{ margin: 0 }}>
      {config.label}
    </Tag>
  );
}
