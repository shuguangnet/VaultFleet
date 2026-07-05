import { Empty } from "antd";
import type { ReactNode } from "react";

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
  className?: string;
}

export function EmptyState({
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  return (
    <Empty
      image={Empty.PRESENTED_IMAGE_SIMPLE}
      description={
        <div>
          <div style={{ fontSize: 14, color: "rgba(0,0,0,0.88)" }}>{title}</div>
          <div style={{ fontSize: 12, color: "rgba(0,0,0,0.45)" }}>
            {description}
          </div>
        </div>
      }
      className={className}
    >
      {action as ReactNode}
    </Empty>
  );
}