import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { browseSnapshot } from "@/services/snapshots";
import { SnapshotTreeBrowser } from "./snapshot-tree-browser";

vi.mock("@/services/snapshots", () => ({
  browseSnapshot: vi.fn(),
}));

afterEach(() => {
  vi.clearAllMocks();
  cleanup();
});

describe("SnapshotTreeBrowser", () => {
  it("disables browsing while the agent is offline", async () => {
    const user = userEvent.setup();

    renderBrowser({ isAgentOnline: false });

    const browseButton = screen.getByRole("button", { name: /需要节点在线才能浏览/ });
    expect(browseButton).toBeDisabled();

    await user.click(browseButton);

    expect(browseSnapshot).not.toHaveBeenCalled();
  });

  it("loads snapshot entries and selecting a directory reports the directory and descendants", async () => {
    const onSelectedPathsChange = vi.fn();
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [
        { path: "/data", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs/readme.md", type: "file", size: 2048, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/photo.jpg", type: "file", size: 1024, mtime: "2026-05-22T00:00:00Z" },
      ],
    });

    renderBrowser({ onSelectedPathsChange });

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));

    expect(browseSnapshot).toHaveBeenCalledWith("agent-1", { snapshot_id: "snap-1" });
    expect(await screen.findByText("data")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "展开 /data" }));

    expect(screen.getByText("docs")).toBeInTheDocument();
    expect(screen.getByText("photo.jpg")).toBeInTheDocument();

    const dataRow = screen.getByText("data").closest("[data-snapshot-tree-row]");
    expect(dataRow).not.toBeNull();

    await user.click(within(dataRow as HTMLElement).getByRole("checkbox", { name: "选择 /data" }));

    expect(onSelectedPathsChange).toHaveBeenLastCalledWith([
      "/data",
      "/data/docs",
      "/data/docs/readme.md",
      "/data/photo.jpg",
    ]);
  });

  it("clears selected paths from the expanded footer", async () => {
    const onSelectedPathsChange = vi.fn();
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [],
    });

    renderBrowser({
      selectedPaths: ["/data", "/data/file.txt"],
      onSelectedPathsChange,
    });

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));

    expect(await screen.findByText("快照为空")).toBeInTheDocument();
    expect(screen.getByText("已选中 2 项")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "清除选择" }));

    expect(onSelectedPathsChange).toHaveBeenLastCalledWith([]);
  });

  it("checks a parent directory when all descendants are selected even if the parent path is not", async () => {
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [
        { path: "/data", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs/readme.md", type: "file", size: 2048, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/photo.jpg", type: "file", size: 1024, mtime: "2026-05-22T00:00:00Z" },
      ],
    });

    renderBrowser({
      selectedPaths: ["/data/docs", "/data/docs/readme.md", "/data/photo.jpg"],
    });

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));
    expect(await screen.findByText("data")).toBeInTheDocument();

    const dataRow = screen.getByText("data").closest("[data-snapshot-tree-row]");
    expect(dataRow).not.toBeNull();
    expect(within(dataRow as HTMLElement).getByRole("checkbox", { name: "选择 /data" }))
      .toHaveAttribute("aria-checked", "true");
  });

  it("disables refresh after expansion when the agent goes offline", async () => {
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [],
    });

    const { rerender } = renderBrowser();

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));
    expect(await screen.findByText("快照为空")).toBeInTheDocument();
    expect(browseSnapshot).toHaveBeenCalledTimes(1);

    rerenderBrowser(rerender, { isAgentOnline: false });

    const refreshButton = screen.getByRole("button", { name: "刷新快照内容" });
    expect(refreshButton).toBeDisabled();

    await user.click(refreshButton);

    expect(browseSnapshot).toHaveBeenCalledTimes(1);
  });

  it("removes selected ancestor directories when a descendant is unchecked", async () => {
    const onSelectedPathsChange = vi.fn();
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [
        { path: "/data", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/docs/readme.md", type: "file", size: 2048, mtime: "2026-05-22T00:00:00Z" },
        { path: "/data/photo.jpg", type: "file", size: 1024, mtime: "2026-05-22T00:00:00Z" },
      ],
    });

    renderBrowser({
      selectedPaths: ["/data", "/data/docs", "/data/docs/readme.md", "/data/photo.jpg"],
      onSelectedPathsChange,
    });

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));
    expect(await screen.findByText("data")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "展开 /data" }));
    const photoRow = screen.getByText("photo.jpg").closest("[data-snapshot-tree-row]");
    expect(photoRow).not.toBeNull();

    await user.click(within(photoRow as HTMLElement).getByRole("checkbox", { name: "选择 /data/photo.jpg" }));

    expect(onSelectedPathsChange).toHaveBeenLastCalledWith([
      "/data/docs",
      "/data/docs/readme.md",
    ]);
  });

  it("resets the expanded tree when the snapshot changes", async () => {
    const user = userEvent.setup();
    vi.mocked(browseSnapshot).mockResolvedValue({
      snapshot_id: "snap-1",
      entries: [{ path: "/data", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" }],
    });

    const { rerender } = renderBrowser();

    await user.click(screen.getByRole("button", { name: /浏览快照内容/ }));
    expect(await screen.findByText("data")).toBeInTheDocument();

    rerenderBrowser(rerender, { snapshotId: "snap-2" });

    await waitFor(() => {
      expect(screen.queryByText("data")).not.toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: /浏览快照内容/ })).toBeInTheDocument();
  });
});

function renderBrowser(
  props: Partial<ComponentProps<typeof SnapshotTreeBrowser>> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SnapshotTreeBrowser
        agentId="agent-1"
        snapshotId="snap-1"
        isAgentOnline
        selectedPaths={[]}
        onSelectedPathsChange={vi.fn()}
        {...props}
      />
    </QueryClientProvider>,
  );
}

function rerenderBrowser(
  rerender: ReturnType<typeof render>["rerender"],
  props: Partial<ComponentProps<typeof SnapshotTreeBrowser>> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  rerender(
    <QueryClientProvider client={queryClient}>
      <SnapshotTreeBrowser
        agentId="agent-1"
        snapshotId="snap-1"
        isAgentOnline
        selectedPaths={[]}
        onSelectedPathsChange={vi.fn()}
        {...props}
      />
    </QueryClientProvider>,
  );
}
