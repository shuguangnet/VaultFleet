import { AuthCheck, AuthCredentials, AuthUser } from "@/types/api";
import { apiGet, apiPost } from "./http";

export const checkAuth = () => apiGet<AuthCheck>("/api/auth/check");
export const initAdmin = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/init", body);
export const login = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/login", body);
