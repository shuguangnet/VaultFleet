import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { SnapshotTreeBrowser } from "@/components/snapshot-tree-browser";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listSnapshots, restoreSnapshot } from "@/services/snapshots";
import { listStorage } from "@/services/storage";
import { SnapshotsPage } from "./snapshots-page";

vi.mock("@/services/agents", () => ({
  listAgents: vi.fn(),
}));

vi.mock("@/services/policies", () => ({
  listPolicies: vi.fn(),
}));

vi.mock("@/services/storage", () => ({
  listStorage: vi.fn(),
}));

vi.mock("@/services/snapshots", () => ({
  listSnapshots: vi.fn(),
  refreshSnapshots: vi.fn(),
  restoreSnapshot: vi.fn(),
}));

vi.mock("@/components/snapshot-tree-browser", () => ({
  SnapshotTreeBrowser: vi.fn(({ onSelectedPathsChange }: ComponentProps<typeof SnapshotTreeBrowser>) => (
    <button
      type="button"
      onClick={() => onSelectedPathsChange(["/data/docs", "/data/docs/readme.md"])}
    >
      选择测试路径
    </button>
  )),
}));

beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  vi.stubGlobal("ResizeObserver", ResizeObserverMock);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("SnapshotsPage", () => {
  it("passes selected snapshot paths to restore requests", async () => {
    const user = userEvent.setup();
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1",
      name: "node-1",
      status: "online",
      last_seen: "2026-05-22T00:00:00Z",
      version: "0.1.0",
      hostname: "node-1",
      os: "linux",
      arch: "amd64",
      created_at: "2026-05-22T00:00:00Z",
    }]);
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(listSnapshots).mockResolvedValue([{
      id: "snap-1",
      time: "2026-05-22T00:00:00Z",
      paths: ["/data"],
      hostname: "node-1",
      username: "root",
    }]);
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-1" });

    renderPage();

    await user.click(await screen.findByRole("button", { name: /恢复/ }));
    expect(screen.getByText("选择测试路径")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "选择测试路径" }));
    expect(screen.getByRole("button", { name: "恢复选中的 2 项" })).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    await user.click(screen.getByRole("button", { name: "恢复选中的 2 项" }));

    expect(restoreSnapshot).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1",
      target_path: "/data",
      include_paths: ["/data/docs", "/data/docs/readme.md"],
    });
  });
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AntdApp>
        <MemoryRouter initialEntries={["/snapshots?agent_id=agent-1"]}>
          <SnapshotsPage />
        </MemoryRouter>
      </AntdApp>
    </QueryClientProvider>,
  );
}
