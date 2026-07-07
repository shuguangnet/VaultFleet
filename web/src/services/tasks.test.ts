import { afterEach, describe, expect, it, vi } from "vitest";
import { getTaskLogs } from "./tasks";

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
});
