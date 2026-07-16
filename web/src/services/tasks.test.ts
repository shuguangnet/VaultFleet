import { afterEach, describe, expect, it, vi } from "vitest";
import { getTaskLogs, retryFailedRestore } from "./tasks";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("task service", () => {
  it("fetches task logs with incremental query params", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: { status: "empty", lines: [] } }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getTaskLogs("task-1", { after: 12, limit: 50 });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/tasks/task-1/logs?after=12&limit=50",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("requests a preflightable failed-item retry plan", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: { agent_id: "target", source_agent_id: "source", snapshot_id: "snap-1", restore_mode: "docker_container", docker_source_ids: ["db-id"] } }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(retryFailedRestore("task-1")).resolves.toMatchObject({ docker_source_ids: ["db-id"] });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/tasks/task-1/retry-failed",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
  });
});
