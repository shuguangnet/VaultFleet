import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

type StatusType = "online" | "offline" | "success" | "failed" | "running" | "syncing" | "unsynced" | "pending";

interface StatusBadgeProps {
  status: StatusType;
  className?: string;
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const config: Record<StatusType, { label: string; className: string }> = {
    online: { label: "在线", className: "bg-green-500 hover:bg-green-600" },
    offline: { label: "离线", className: "bg-red-500 hover:bg-red-600" },
    success: { label: "已完成", className: "bg-green-500 hover:bg-green-600" },
    failed: { label: "失败", className: "bg-red-500 hover:bg-red-600" },
    running: { label: "运行中", className: "bg-blue-500 hover:bg-blue-600 animate-pulse" },
    syncing: { label: "同步中", className: "bg-blue-500 hover:bg-blue-600" },
    unsynced: { label: "待同步", className: "bg-amber-500 hover:bg-amber-600" },
    pending: { label: "待同步", className: "bg-amber-500 hover:bg-amber-600" },
  };

  const { label, className: statusClass } = config[status] || { label: status, className: "bg-gray-500" };

  return (
    <Badge className={cn("font-medium", statusClass, className)}>
      {label}
    </Badge>
  );
}
