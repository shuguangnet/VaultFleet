import { AuthCheck, AuthCredentials, AuthUser } from "@/types/api";
import { apiGet, apiPost } from "./http";

export const checkAuth = async (): Promise<AuthCheck> => {
  const data = await apiGet<AuthCheck>("/api/auth/check");
  if (!data.user && data.username) {
    return {
      ...data,
      user: { username: data.username, role: data.role, permissions: data.permissions },
    };
  }
  return data;
};
export const initAdmin = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/init", body);
export const login = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/login", body);
export const logout = () => apiPost<void>("/api/auth/logout");
