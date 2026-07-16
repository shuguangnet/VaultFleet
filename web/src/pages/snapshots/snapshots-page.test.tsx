import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { SnapshotTreeBrowser } from "@/components/snapshot-tree-browser";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listSnapshots, preflightRestore, restoreSnapshot } from "@/services/snapshots";
import { listStorage } from "@/services/storage";
import { SnapshotsPage } from "./snapshots-page";
import { AuthProvider } from "@/contexts/auth-context";

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
  preflightRestore: vi.fn(),
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
  cleanup();
  vi.clearAllMocks();
});

describe("SnapshotsPage", () => {
  it("passes selected snapshot paths to restore requests", async () => {
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1",
      name: "node-1",
      status: "online",
      last_seen: "2026-05-22T00:00:00Z",
      version: "0.1.0",
      hostname: "node-1",
      os: "linux",
      arch: "amd64",
      capabilities: ["restore_preflight"],
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
    vi.mocked(preflightRestore).mockResolvedValue({
      snapshot_id: "snap-1",
      status: "passed",
      checks: [{ code: "target_path_writable", severity: "info", message: "target path is writable" }],
    });
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-1" });

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /恢复/ }));
    expect(screen.getByText("选择测试路径")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "选择测试路径" }));
    expect(screen.getByRole("button", { name: "恢复选中的 2 项" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /执行恢复预检/ }));
    await waitFor(() => expect(preflightRestore).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1",
      source_agent_id: "agent-1",
      restore_mode: "files",
      target_path: "/data",
      include_paths: ["/data/docs", "/data/docs/readme.md"],
    }));
    expect(await screen.findByText("预检通过")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    fireEvent.click(screen.getByRole("button", { name: "恢复选中的 2 项" }));

    await waitFor(() => expect(restoreSnapshot).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1",
      source_agent_id: "agent-1",
      restore_mode: "files",
      target_path: "/data",
      include_paths: ["/data/docs", "/data/docs/readme.md"],
    }));
  }, 10000);

  it("restores docker containers from snapshot browser", async () => {
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1",
      name: "node-1",
      status: "online",
      last_seen: "2026-05-22T00:00:00Z",
      version: "0.5.23",
      hostname: "node-1",
      os: "linux",
      arch: "amd64",
      capabilities: ["restore_preflight", "docker_workload_backups", "docker_container_restore"],
      created_at: "2026-05-22T00:00:00Z",
    }]);
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(listSnapshots).mockResolvedValue([{
      id: "snap-1",
      time: "2026-05-22T00:00:00Z",
      paths: ["/var/lib/docker/volumes/db/_data"],
      hostname: "node-1",
      username: "root",
      docker: {
        sources: [
          {
            container_id: "container-1",
            name: "postgres",
            image: "postgres:16",
            resolved_paths: ["/var/lib/docker/volumes/db/_data"],
          },
          {
            container_id: "container-2",
            name: "api",
            image: "api:latest",
            resolved_paths: ["/srv/api"],
          },
        ],
      },
    }]);
    vi.mocked(preflightRestore).mockResolvedValue({
      snapshot_id: "snap-1",
      status: "passed",
      checks: [{ code: "docker_available", severity: "info", message: "Docker Engine is available" }],
    });
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-1" });

    renderPage();

    const restoreContainerButtons = await screen.findAllByRole("button", { name: /恢复容器/ });
    fireEvent.click(restoreContainerButtons[0]);

    await waitFor(() => {
      expect(screen.queryByText("选择测试路径")).not.toBeInTheDocument();
    });
    expect(screen.getByText(/升级目标 Agent 后可在一次任务中恢复多个容器/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /执行恢复预检/ }));
    expect(await screen.findByText("Docker Engine is available")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    const submitButtons = screen.getAllByRole("button", { name: /恢复容器/ });
    fireEvent.click(submitButtons[submitButtons.length - 1]);

    await waitFor(() => expect(restoreSnapshot).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1",
      source_agent_id: "agent-1",
      restore_mode: "docker_container",
      docker_source_id: "container-1",
    }));
  }, 10000);

  it("submits restore requests to the selected target agent", async () => {
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "source-agent",
        name: "source-node",
        status: "online",
        last_seen: "2026-05-22T00:00:00Z",
        version: "0.5.23",
        hostname: "source-node",
        os: "linux",
        arch: "amd64",
        capabilities: ["snapshot_browse", "restore_preflight", "restore_include_paths"],
        created_at: "2026-05-22T00:00:00Z",
      },
      {
        id: "target-agent",
        name: "target-node",
        status: "online",
        last_seen: "2026-05-22T00:00:00Z",
        version: "0.5.23",
        hostname: "target-node",
        os: "linux",
        arch: "amd64",
        capabilities: ["restore_preflight", "restore_include_paths"],
        created_at: "2026-05-22T00:00:00Z",
      },
    ]);
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(listSnapshots).mockResolvedValue([{
      id: "snap-1",
      time: "2026-05-22T00:00:00Z",
      paths: ["/data"],
      hostname: "source-node",
      username: "root",
    }]);
    vi.mocked(preflightRestore).mockResolvedValue({
      snapshot_id: "snap-1",
      status: "passed",
      checks: [{ code: "target_path_writable", severity: "info", message: "target path is writable" }],
    });
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-1" });

    renderPage("/snapshots?agent_id=source-agent");

    fireEvent.click(await screen.findByRole("button", { name: /恢复/ }));
    const targetSelect = screen.getAllByRole("combobox")[1];
    fireEvent.mouseDown(targetSelect);
    fireEvent.click(await screen.findByText("target-node"));

    fireEvent.click(screen.getByRole("button", { name: /执行恢复预检/ }));
    await waitFor(() => expect(preflightRestore).toHaveBeenCalledWith("target-agent", {
      snapshot_id: "snap-1",
      source_agent_id: "source-agent",
      restore_mode: "files",
      target_path: "/data",
    }));

    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    fireEvent.click(screen.getByRole("button", { name: "恢复全部" }));

    await waitFor(() => expect(restoreSnapshot).toHaveBeenCalledWith("target-agent", {
      snapshot_id: "snap-1",
      source_agent_id: "source-agent",
      restore_mode: "files",
      target_path: "/data",
    }));
  }, 10000);

  it("blocks final restore until preflight passes", async () => {
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1",
      name: "node-1",
      status: "online",
      last_seen: "2026-05-22T00:00:00Z",
      version: "0.5.26",
      hostname: "node-1",
      os: "linux",
      arch: "amd64",
      capabilities: ["restore_preflight"],
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

    fireEvent.click(await screen.findByRole("button", { name: /恢复/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    expect(screen.getByRole("button", { name: "恢复全部" })).toBeDisabled();

    expect(preflightRestore).not.toHaveBeenCalled();
    expect(restoreSnapshot).not.toHaveBeenCalled();
  }, 10000);

  it("blocks final restore when preflight fails", async () => {
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1",
      name: "node-1",
      status: "online",
      last_seen: "2026-05-22T00:00:00Z",
      version: "0.5.26",
      hostname: "node-1",
      os: "linux",
      arch: "amd64",
      capabilities: ["restore_preflight"],
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
    vi.mocked(preflightRestore).mockResolvedValue({
      snapshot_id: "snap-1",
      status: "failed",
      checks: [{ code: "target_path_writable", severity: "error", message: "target path is not writable", detail: "permission denied" }],
    });
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-1" });

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /恢复/ }));
    fireEvent.click(screen.getByRole("button", { name: /执行恢复预检/ }));

    expect(await screen.findByText("预检未通过")).toBeInTheDocument();
    expect(screen.getByText(/target path is not writable/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    expect(screen.getByRole("button", { name: "恢复全部" })).toBeDisabled();
    expect(restoreSnapshot).not.toHaveBeenCalled();
  }, 10000);

  it("selects and restores multiple Docker containers in snapshot order", async () => {
    vi.mocked(listAgents).mockResolvedValue([{
      id: "agent-1", name: "node-1", status: "online", last_seen: "2026-05-22T00:00:00Z",
      version: "0.9.0", hostname: "node-1", os: "linux", arch: "amd64",
      capabilities: ["restore_preflight", "docker_container_restore", "docker_multi_container_restore"],
      created_at: "2026-05-22T00:00:00Z",
    }]);
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(listSnapshots).mockResolvedValue([{
      id: "snap-1", time: "2026-05-22T00:00:00Z", paths: ["/shared"], hostname: "node-1", username: "root",
      docker: { sources: [
        { container_id: "db-id", name: "db", image: "postgres:16", resolved_paths: ["/shared", "/db"] },
        { container_id: "api-id", name: "api", image: "api:latest", resolved_paths: ["/shared", "/api"] },
      ] },
    }]);
    vi.mocked(preflightRestore).mockResolvedValue({
      snapshot_id: "snap-1", status: "passed",
      checks: [{ code: "docker_available", severity: "info", message: "Docker Engine is available", source_id: "db-id", source_name: "db" }],
    });
    vi.mocked(restoreSnapshot).mockResolvedValue({ message_id: "msg-batch" });

    renderPage();
    fireEvent.click((await screen.findAllByRole("button", { name: /恢复容器/ }))[0]);
    fireEvent.click(screen.getByRole("checkbox", { name: /选择 api/ }));
    expect(screen.getByText(/已选 2/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /执行恢复预检/ }));
    await waitFor(() => expect(preflightRestore).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1", source_agent_id: "agent-1", restore_mode: "docker_container", docker_source_ids: ["db-id", "api-id"],
    }));
    expect(await screen.findByText("Docker Engine is available")).toBeInTheDocument();
    expect(screen.getAllByText("db").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("checkbox", { name: /确认恢复/ }));
    fireEvent.click(screen.getByRole("button", { name: "恢复 2 个容器" }));

    await waitFor(() => expect(restoreSnapshot).toHaveBeenCalledWith("agent-1", {
      snapshot_id: "snap-1", source_agent_id: "agent-1", restore_mode: "docker_container", docker_source_ids: ["db-id", "api-id"],
    }));
  }, 10000);
});

function renderPage(initialEntry = "/snapshots?agent_id=agent-1") {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AntdApp>
        <MemoryRouter initialEntries={[initialEntry]}>
          <AuthProvider user={{ username: "admin", role: "admin", permissions: ["read:operational", "run:restore"] }}>
            <SnapshotsPage />
          </AuthProvider>
        </MemoryRouter>
      </AntdApp>
    </QueryClientProvider>,
  );
}
