import { createContext, useContext, type ReactNode } from "react";
import type { AuthUser } from "@/types/api";

interface AuthContextValue {
  user: AuthUser;
  hasPermission: (permission: string) => boolean;
  isAdmin: boolean;
  isOperator: boolean;
  isViewer: boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ user, children }: { user: AuthUser; children: ReactNode }) {
  const permissionSet = new Set(user.permissions ?? []);
  const role = user.role ?? "admin";
  const value: AuthContextValue = {
    user,
    hasPermission: (permission) => permissionSet.has(permission),
    isAdmin: role === "admin",
    isOperator: role === "operator",
    isViewer: role === "viewer",
  };
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) {
    const permissions = [
      "read:operational",
      "write:nodes",
      "write:storage",
      "write:policies",
      "run:backup",
      "run:restore",
      "write:notifications",
      "read:system",
      "admin:system",
      "admin:users",
      "admin:tokens",
      "read:audit",
    ];
    return {
      user: { username: "", role: "admin", permissions },
      hasPermission: (permission: string) => permissions.includes(permission),
      isAdmin: true,
      isOperator: false,
      isViewer: false,
    } satisfies AuthContextValue;
  }
  return value;
}
