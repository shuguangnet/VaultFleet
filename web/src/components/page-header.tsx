import type { ReactNode } from "react";
import { Space, Typography } from "antd";

interface PageHeaderProps {
  title: string;
  description?: string;
  eyebrow?: string;
  icon?: ReactNode;
  actions?: ReactNode;
  meta?: ReactNode;
}

export function PageHeader({
  title,
  description,
  eyebrow,
  icon,
  actions,
  meta,
}: PageHeaderProps) {
  return (
    <section className="vf-page-heading">
      <div className="vf-page-heading-main">
        {icon && <span className="vf-page-heading-icon">{icon}</span>}
        <div className="vf-page-heading-copy">
          {eyebrow && (
            <Typography.Text className="vf-page-heading-eyebrow">
              {eyebrow}
            </Typography.Text>
          )}
          <Typography.Title level={4} className="vf-page-heading-title">
            {title}
          </Typography.Title>
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
