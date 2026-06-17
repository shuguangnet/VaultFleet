import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { listAgents, backupNow } from "@/services/agents";
import { createPolicy, deletePolicy, listPolicies, updatePolicy } from "@/services/policies";
import { listStorage } from "@/services/storage";
import { PoliciesPage } from "./policies-page";

vi.mock("@/services/agents", () => ({
  backupNow: vi.fn(),
  listAgents: vi.fn(),
}));

vi.mock("@/services/policies", () => ({
  createPolicy: vi.fn(),
  deletePolicy: vi.fn(),
  listPolicies: vi.fn(),
  updatePolicy: vi.fn(),
}));

vi.mock("@/services/storage", () => ({
  listStorage: vi.fn(),
}));

vi.mock("@/components/directory-browser", () => ({
  DirectoryBrowser: () => null,
}));

beforeAll(() => {
  if (!Element.prototype.hasPointerCapture) {
    Element.prototype.hasPointerCapture = vi.fn(() => false);
  }
  if (!Element.prototype.setPointerCapture) {
    Element.prototype.setPointerCapture = vi.fn();
  }
  if (!Element.prototype.releasePointerCapture) {
    Element.prototype.releasePointerCapture = vi.fn();
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = vi.fn();
  }
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("PoliciesPage rclone form state", () => {
  it("resets WebDAV rclone defaults after successful create", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-webdav",
        name: "WebDAV Store",
        rclone_type: "webdav",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "",
      storage_id: "storage-webdav",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/",
      backup_dirs: [],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <PoliciesPage />
      </QueryClientProvider>,
    );

    await user.click(await screen.findByRole("button", { name: "添加策略" }));
    await user.click(screen.getAllByRole("combobox")[1]);
    const webdavOption = (await screen.findAllByText("WebDAV Store")).find((el) => el.tagName !== "OPTION");
    expect(webdavOption).toBeDefined();
    await user.click(webdavOption!);

    expect(screen.getByLabelText("并发传输数")).toHaveValue("2");

    await user.click(screen.getByRole("button", { name: "提交策略" }));
    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("button", { name: "添加策略" }));

    expect(screen.queryByLabelText("并发传输数")).not.toBeInTheDocument();
  });

  it("submits the configured timeout hours", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: ["/data"],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 12,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <PoliciesPage />
      </QueryClientProvider>,
    );

    await user.click(await screen.findByRole("button", { name: "添加策略" }));
    await user.click(screen.getAllByRole("combobox")[0]);
    await user.click(await screen.findByRole("option", { name: "node-1" }));
    await user.click(screen.getAllByRole("combobox")[1]);
    await user.click(await screen.findByRole("option", { name: "S3 Store" }));
    await user.type(screen.getByRole("textbox", { name: "备份目录" }), "/data");
    await user.clear(screen.getByLabelText("任务超时（小时）"));
    await user.type(screen.getByLabelText("任务超时（小时）"), "12");
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0]).toEqual(expect.objectContaining({ timeout_hours: 12 }));
  });
});

function newTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: {
        retry: false,
      },
      queries: {
        retry: false,
      },
    },
  });
}
