import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
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
  last_seen_at: "2026-05-21T07:48:34Z",
  system_info: "{\"hostname\":\"arm-host\",\"os\":\"linux\",\"arch\":\"arm64\"}",
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

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <MemoryRouter>
          <NodesPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

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
  });

  it("shows mutation controls for admin users", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(apiResponse([onlineAgent])));

    renderNodesWithUser({ username: "admin", role: "admin", permissions: ["write:nodes"] });

    expect(await screen.findByRole("button", { name: /添加节点/ })).toBeInTheDocument();
  });
});

function renderNodesWithUser(user: { username: string; role: "admin" | "operator" | "viewer"; permissions: string[] }) {
  return render(
    <QueryClientProvider client={newTestQueryClient()}>
      <MemoryRouter>
        <AuthProvider user={user}>
          <NodesPage />
        </AuthProvider>
      </MemoryRouter>
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
