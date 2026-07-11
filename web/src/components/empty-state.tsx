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
  icon,
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  return (
    <Empty
      image={icon || Empty.PRESENTED_IMAGE_SIMPLE}
      description={
        <div className="vf-empty-copy">
          <div className="vf-empty-title">{title}</div>
          <div className="vf-empty-description">
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
