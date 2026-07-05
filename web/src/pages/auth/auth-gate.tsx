import { Spin } from "antd";
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
      <div
        style={{
          display: "flex",
          height: "100vh",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Spin size="large" tip="加载中..." />
      </div>
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