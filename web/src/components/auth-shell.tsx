import type { ReactNode } from "react";
import { SafetyCertificateOutlined } from "@ant-design/icons";

interface AuthShellProps {
  title: string;
  description: string;
  children: ReactNode;
  width?: "default" | "wide";
}

export function AuthShell({
  title,
  description,
  children,
  width = "default",
}: AuthShellProps) {
  return (
    <main className="vf-auth-shell">
      <section
        className={`vf-auth-panel${width === "wide" ? " vf-auth-panel-wide" : ""}`}
        aria-labelledby="auth-title"
      >
        <header className="vf-auth-brand">
          <span className="vf-auth-brand-mark" aria-hidden="true">
            <SafetyCertificateOutlined />
          </span>
          <span className="vf-auth-brand-copy">
            <span className="vf-auth-brand-name">VaultFleet</span>
            <span className="vf-auth-brand-subtitle">企业云备份控制台</span>
          </span>
        </header>

        <div className="vf-auth-heading">
          <h1 id="auth-title">{title}</h1>
          <p>{description}</p>
        </div>

        {children}

        <footer className="vf-auth-trust">
          <SafetyCertificateOutlined aria-hidden="true" />
          <span>凭据仅用于当前 VaultFleet 服务</span>
        </footer>
      </section>
    </main>
  );
}
