import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { StoragePage } from "./storage-page";
import { listProviders, listStorage } from "@/services/storage";

vi.mock("@/services/storage", () => ({
  createStorage: vi.fn(),
  deleteStorage: vi.fn(),
  listProviders: vi.fn(),
  listStorage: vi.fn(),
  testSavedStorage: vi.fn(),
  testUnsavedStorage: vi.fn(),
  updateStorage: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("StoragePage", () => {
  it("opens an existing S3 storage configuration for editing", async () => {
    const user = userEvent.setup();
    vi.mocked(listProviders).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "Production OSS",
        rclone_type: "s3",
        rclone_config: {
          provider: "Alibaba",
          endpoint: "oss-cn-shanghai.aliyuncs.com",
          bucket: "backup-bucket",
        },
        created_at: "2026-07-12T00:00:00Z",
        updated_at: "2026-07-12T00:00:00Z",
      },
    ]);

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <StoragePage />
        </AntdApp>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Production OSS")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "ellipsis" }));
    await user.click(await screen.findByText("编辑"));

    expect(await screen.findByText("编辑存储")).toBeInTheDocument();
    expect(screen.getByDisplayValue("Production OSS")).toBeInTheDocument();
    expect(screen.getByDisplayValue("oss-cn-shanghai.aliyuncs.com")).toBeInTheDocument();
  });
});

function newTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
}
