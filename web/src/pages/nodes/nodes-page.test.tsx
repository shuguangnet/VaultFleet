import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { NodesPage } from "./nodes-page";
import { AuthProvider } from "@/contexts/auth-context";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: vi.fn(actual.useQuery),
  };
});

const onlineAgent = {
  id: "agent-1",
  name: "ARM64-Node",
  status: "online",
  tags: ["prod"],
  last_seen_at: "2026-05-21T07:48:34Z",
  system_info: "{\"hostname\":\"arm-host\",\"os\":\"linux\",\"arch\":\"arm64\",\"version\":\"v0.5.41\"}",
  created_at: "2026-05-21T05:17:11Z",
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("NodesPage", () => {
  it("polls agent status so the page can show offline and reconnect changes", async () => {
    const fetchMock = vi.fn().mockResolvedValue(apiResponse([onlineAgent]));
    vi.stubGlobal("fetch", fetchMock);

    renderNodesWithUser({ username: "admin", role: "admin", permissions: ["read:operational", "write:nodes"] });

    const row = await screen.findByRole("row", { name: /ARM64-Node/ });
    expect(within(row).getByText("在线")).toBeInTheDocument();

    const agentsQuery = vi.mocked(useQuery).mock.calls.find(([options]) => {
      return Array.isArray(options.queryKey) && options.queryKey[0] === "agents";
    });
    expect(agentsQuery?.[0]).toMatchObject({ refetchInterval: 10000 });
  });

  it("hides mutation controls for viewer users", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(apiResponse([onlineAgent])));

    renderNodesWithUser({ username: "viewer", role: "viewer", permissions: ["read:operational"] });

    await screen.findByRole("row", { name: /ARM64-Node/ });
    expect(screen.queryByRole("button", { name: /添加节点/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /批量升级/ })).not.toBeInTheDocument();
  });

  it("shows mutation controls for admin users", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(apiResponse([onlineAgent])));

    renderNodesWithUser({ username: "admin", role: "admin", permissions: ["write:nodes"] });

    expect(await screen.findByRole("button", { name: /添加节点/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /批量升级/ })).toBeInTheDocument();
  });

  it("creates a rollout from the selected tag", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input.toString();
      if (url.includes("/api/agents/tags")) return apiResponse(["prod"]);
      if (url.includes("/api/agent-upgrade-rollouts") && init?.method === "POST") {
        return apiResponse({
          id: "rollout-1",
          target_version: "v0.5.42",
          target_tags: ["prod"],
          target_agent_ids: [],
          canary_count: 1,
          batch_size: 5,
          status: "pending",
          counts: { pending: 1, running: 0, success: 0, failed: 0, skipped: 0 },
          created_at: "2026-07-09T00:00:00Z",
          updated_at: "2026-07-09T00:00:00Z",
        });
      }
      if (url.includes("/api/agent-upgrade-rollouts")) return apiResponse([]);
      if (url.includes("/api/agents")) return apiResponse([onlineAgent]);
      return apiResponse([]);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderNodesWithUser({ username: "admin", role: "admin", permissions: ["write:nodes", "read:operational"] });

    await screen.findByRole("row", { name: /ARM64-Node/ });
    await user.click(screen.getByRole("button", { name: /批量升级/ }));
    await user.type(screen.getByPlaceholderText("留空使用 Master 当前版本或 latest"), "v0.5.42");
    fireEvent.mouseDown(screen.getByLabelText("升级目标标签"));
    await clickAntSelectOption("prod");
    expect(await screen.findByText("目标预览")).toBeInTheDocument();
    expect(screen.getAllByText("ARM64-Node").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: /创建升级任务/ }));

    await waitFor(() => {
      const postCall = fetchMock.mock.calls.find(([url, init]) =>
        url.toString().includes("/api/agent-upgrade-rollouts") && init?.method === "POST"
      );
      expect(postCall).toBeTruthy();
      expect(JSON.parse(postCall?.[1]?.body as string)).toEqual(
        expect.objectContaining({
          target_version: "v0.5.42",
          target_tags: ["prod"],
          canary_count: 1,
          batch_size: 5,
        }),
      );
    });
  });

  it("renders failed rollout progress", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const url = input.toString();
      if (url.includes("/api/agents/tags")) return apiResponse(["prod"]);
      if (url.includes("/api/agent-upgrade-rollouts")) {
        return apiResponse([
          {
            id: "rollout-1",
            target_version: "v0.5.42",
            target_tags: ["prod"],
            target_agent_ids: [],
            canary_count: 1,
            batch_size: 5,
            status: "failed",
            failure_reason: "agent self-update is disabled",
            counts: { pending: 0, running: 0, success: 0, failed: 1, skipped: 2 },
            items: [
              {
                id: "item-1",
                rollout_id: "rollout-1",
                agent_id: "agent-1",
                agent_name: "ARM64-Node",
                phase: "canary",
                batch_index: 0,
                status: "failed",
                current_version: "v0.5.41",
                target_version: "v0.5.42",
                error: "agent self-update is disabled",
                created_at: "2026-07-09T00:00:00Z",
                updated_at: "2026-07-09T00:00:00Z",
              },
              {
                id: "item-2",
                rollout_id: "rollout-1",
                agent_id: "agent-2",
                agent_name: "Batch-Node",
                phase: "batch",
                batch_index: 0,
                status: "skipped",
                current_version: "v0.5.41",
                target_version: "v0.5.42",
                skip_reason: "rollout stopped after failed item",
                created_at: "2026-07-09T00:00:00Z",
                updated_at: "2026-07-09T00:00:00Z",
              },
            ],
            created_at: "2026-07-09T00:00:00Z",
            updated_at: "2026-07-09T00:00:00Z",
          },
        ]);
      }
      if (url.includes("/api/agents")) return apiResponse([onlineAgent]);
      return apiResponse([]);
    }));

    renderNodesWithUser({ username: "admin", role: "admin", permissions: ["write:nodes", "read:operational"] });

    expect(await screen.findByText("v0.5.42")).toBeInTheDocument();
    expect(screen.getByText("agent self-update is disabled")).toBeInTheDocument();
    expect(screen.getByText("失败 1")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /expand row/i }));
    await waitFor(() => expect(screen.getAllByText("ARM64-Node").length).toBeGreaterThan(1));
    expect(screen.getByText("Canary")).toBeInTheDocument();
    expect(screen.getByText("Batch-Node")).toBeInTheDocument();
    expect(screen.getByText("rollout stopped after failed item")).toBeInTheDocument();
  });
});

function renderNodesWithUser(user: { username: string; role: "admin" | "operator" | "viewer"; permissions: string[] }) {
  return render(
    <QueryClientProvider client={newTestQueryClient()}>
      <AntdApp>
        <MemoryRouter>
          <AuthProvider user={user}>
            <NodesPage />
          </AuthProvider>
        </MemoryRouter>
      </AntdApp>
    </QueryClientProvider>,
  );
}

function newTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
}

function apiResponse(data: unknown) {
  return new Response(JSON.stringify({ ok: true, data }), { status: 200 });
}

async function clickAntSelectOption(text: string) {
  const options = await screen.findAllByText(text);
  const option = options.find((el) => el.closest(".ant-select-item-option"));
  expect(option).toBeDefined();
  fireEvent.click(option!);
}
