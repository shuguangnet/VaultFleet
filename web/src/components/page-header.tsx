import type { ReactNode } from "react";
import { Space, Typography } from "antd";

interface PageHeaderProps {
  title: string;
  description?: string;
  icon?: ReactNode;
  actions?: ReactNode;
  meta?: ReactNode;
}

export function PageHeader({
  title,
  description,
  icon,
  actions,
  meta,
}: PageHeaderProps) {
  return (
    <section className="vf-page-heading">
      <div className="vf-page-heading-main">
        <div className="vf-page-heading-copy">
          <div className="vf-page-heading-title-row">
            {icon && <span className="vf-page-heading-icon">{icon}</span>}
            <Typography.Title level={4} className="vf-page-heading-title">
              {title}
            </Typography.Title>
          </div>
          {description && (
            <Typography.Text className="vf-page-heading-description">
              {description}
            </Typography.Text>
          )}
          {meta && <div className="vf-page-heading-meta">{meta}</div>}
        </div>
      </div>
      {actions && (
        <Space className="vf-page-heading-actions" size={8} wrap>
          {actions}
        </Space>
      )}
    </section>
  );
}
