import { afterEach, describe, expect, it, vi } from "vitest";
import { browseSnapshot, restoreSnapshot } from "./snapshots";

afterEach(() => vi.restoreAllMocks());

describe("snapshot service", () => {
  it("sends selective restore include paths", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: { message_id: "msg-1" } }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(restoreSnapshot("agent-1", {
      snapshot_id: "snap-1",
      target_path: "/restore",
      include_paths: ["etc/config.yml"],
    })).resolves.toEqual({ message_id: "msg-1" });

    expect(fetchMock).toHaveBeenCalledWith("/api/agents/agent-1/restore", expect.objectContaining({
      body: JSON.stringify({
        snapshot_id: "snap-1",
        target_path: "/restore",
        include_paths: ["etc/config.yml"],
      }),
      method: "POST",
    }));
  });

  it("browses a snapshot file listing", async () => {
    const response = {
      snapshot_id: "snap-1",
      entries: [{ path: "etc", type: "dir", size: 0, mtime: "2026-05-22T00:00:00Z" }],
    };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: response }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(browseSnapshot("agent-1", { snapshot_id: "snap-1" })).resolves.toEqual(response);

    expect(fetchMock).toHaveBeenCalledWith("/api/agents/agent-1/snapshot-browse", expect.objectContaining({
      body: JSON.stringify({ snapshot_id: "snap-1" }),
      method: "POST",
    }));
  });
});
