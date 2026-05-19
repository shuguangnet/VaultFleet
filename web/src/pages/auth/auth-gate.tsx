import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { checkAuth } from "@/services/auth";
import { LoginPage } from "./login-page";
import { SetupPage } from "./setup-page";
import { AppLayout } from "@/layouts/app-layout";
import { Skeleton } from "@/components/ui/skeleton";

export function AuthGate() {
  const { data, isLoading, refetch } = useQuery({
    queryKey: ["auth-check"],
    queryFn: checkAuth,
  });

  if (isLoading) {
    return (
      <div className="flex h-screen w-screen items-center justify-center">
        <div className="space-y-4">
          <Skeleton className="h-12 w-[250px]" />
          <Skeleton className="h-4 w-[200px]" />
        </div>
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
