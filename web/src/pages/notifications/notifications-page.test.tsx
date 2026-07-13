import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { NotificationsPage } from "./notifications-page";
import { listNotifications } from "@/services/notifications";
import { AuthProvider } from "@/contexts/auth-context";

vi.mock("@/services/notifications", () => ({
  createNotification: vi.fn(),
  deleteNotification: vi.fn(),
  listNotifications: vi.fn(),
  testNotification: vi.fn(),
  testNotificationConfig: vi.fn(),
  testNotificationDraft: vi.fn(),
  updateNotification: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("NotificationsPage", () => {
  it("shows backup and verification success events as opt-in choices", async () => {
    const user = userEvent.setup();
    vi.mocked(listNotifications).mockResolvedValue([]);

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <AuthProvider user={{ username: "admin", role: "admin", permissions: ["read:operational", "write:notifications"] }}>
            <NotificationsPage />
          </AuthProvider>
        </AntdApp>
      </QueryClientProvider>,
    );

    await user.click(await screen.findByRole("button", { name: /添加通知/ }));

    expect(screen.getAllByText("备份成功").length).toBeGreaterThan(0);
    expect(screen.getAllByText("备份失败").length).toBeGreaterThan(0);
    expect(screen.getAllByText("验证成功").length).toBeGreaterThan(0);
    expect(screen.getAllByText("验证失败").length).toBeGreaterThan(0);
    expect(screen.getAllByText("节点离线").length).toBeGreaterThan(0);
    expect(screen.getByRole("switch", { name: /备份成功/ })).not.toBeChecked();
    expect(screen.getByRole("switch", { name: /验证成功/ })).not.toBeChecked();
  });
});

function newTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
}
