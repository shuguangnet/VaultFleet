import { StorageConfig, StorageInput } from "@/types/storage";
import { apiDelete, apiGet, apiPost, apiPut } from "./http";

export const listStorage = () => apiGet<StorageConfig[]>("/api/storage");
export const createStorage = (body: StorageInput) => apiPost<StorageConfig>("/api/storage", body);
export const getStorage = (id: string) => apiGet<StorageConfig>(`/api/storage/${id}`);
export const updateStorage = (id: string, body: Partial<StorageInput>) => apiPut<StorageConfig>(`/api/storage/${id}`, body);
export const deleteStorage = (id: string) => apiDelete(`/api/storage/${id}`);
