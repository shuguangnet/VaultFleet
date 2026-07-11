import { LoadingOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { checkAuth } from "@/services/auth";
import { LoginPage } from "./login-page";
import { SetupPage } from "./setup-page";
import { AppLayout } from "@/layouts/app-layout";

export function AuthGate() {
  const { data, isLoading, refetch } = useQuery({
    queryKey: ["auth-check"],
    queryFn: checkAuth,
  });

  if (isLoading) {
    return (
      <main className="vf-auth-loading" aria-busy="true" aria-live="polite">
        <span className="vf-auth-loading-mark" aria-hidden="true">
          <SafetyCertificateOutlined />
        </span>
        <strong>VaultFleet</strong>
        <span className="vf-auth-loading-status">
          <LoadingOutlined spin aria-hidden="true" />
          正在验证服务状态
        </span>
      </main>
    );
  }

  if (!data?.initialized) {
    return <SetupPage onComplete={() => refetch()} />;
  }

  if (!data?.authenticated) {
    return <LoginPage onComplete={() => refetch()} />;
  }

  return <AppLayout user={data.user!} />;
}
