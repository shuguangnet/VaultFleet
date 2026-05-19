import { apiPut, ApiError } from "./http";

export const changePassword = (body: { current_password: string; new_password: string }) => apiPut<{ ok: true }>("/api/system/password", body);

export const exportSystemData = async () => {
  const response = await fetch("/api/system/export", { credentials: "same-origin" });
  if (!response.ok) throw new ApiError("export failed", response.status, await response.text());
  return response.blob();
};
