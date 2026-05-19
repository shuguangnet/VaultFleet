import { NotificationConfig, NotificationInput } from "@/types/notification";
import { apiDelete, apiGet, apiPost, apiPut } from "./http";

export const listNotifications = () => apiGet<NotificationConfig[]>("/api/notifications");
export const createNotification = (body: NotificationInput) => apiPost<NotificationConfig>("/api/notifications", body);
export const getNotification = (id: string) => apiGet<NotificationConfig>(`/api/notifications/${id}`);
export const updateNotification = (id: string, body: Partial<NotificationInput>) => apiPut<NotificationConfig>(`/api/notifications/${id}`, body);
export const deleteNotification = (id: string) => apiDelete(`/api/notifications/${id}`);
export const testNotification = (id: string) => apiPost<{ ok: true }>(`/api/notifications/${id}/test`);
