import { describe, expect, it, vi } from "vitest";
import { createDockerBackupProfile, discoverDocker, restoreDockerSnapshot } from "./docker";

describe("docker services", () => {
  it("calls docker discovery endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true, data: { containers: [] } }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await discoverDocker("agent-1");

    expect(fetchMock).toHaveBeenCalledWith("/api/agents/agent-1/docker/discover", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ all: false }),
    }));
  });

  it("calls docker backup profile endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true, data: { policy_id: "p1" } }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await createDockerBackupProfile("agent-1", { storage_id: "s1", containers: [], run_now: true });

    expect(fetchMock).toHaveBeenCalledWith("/api/agents/agent-1/docker/backup-profile", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ storage_id: "s1", containers: [], run_now: true }),
    }));
  });

  it("calls docker restore endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true, data: { message_id: "m1" } }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await restoreDockerSnapshot("agent-1", { snapshot_id: "snap-1", target_path: "/restore", start_containers: false });

    expect(fetchMock).toHaveBeenCalledWith("/api/agents/agent-1/docker/restore", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ snapshot_id: "snap-1", target_path: "/restore", start_containers: false }),
    }));
  });
});
