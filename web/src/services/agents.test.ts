import { describe, expect, it, vi, afterEach } from "vitest";
import { getAgent, listAgents, normalizeAgent } from "./agents";

afterEach(() => vi.restoreAllMocks());

describe("agent service", () => {
  it("normalizes backend last_seen_at and system_info fields", () => {
    expect(normalizeAgent({
      id: "agent-1",
      name: "Debian-AMD64",
      status: "online",
      last_seen_at: "2026-05-21T05:20:52.803465124Z",
      system_info: "{\"hostname\":\"ser4885257919\",\"os\":\"linux\",\"arch\":\"amd64\",\"version\":\"0.1.0\",\"capabilities\":[\"docker_workload_backups\"]}",
      created_at: "2026-05-21T05:17:11Z",
    })).toEqual({
      id: "agent-1",
      name: "Debian-AMD64",
      status: "online",
      last_seen: "2026-05-21T05:20:52.803465124Z",
      version: "0.1.0",
      hostname: "ser4885257919",
      os: "linux",
      arch: "amd64",
      capabilities: ["docker_workload_backups"],
      created_at: "2026-05-21T05:17:11Z",
    });
  });

  it("falls back to empty display fields when system_info is absent or invalid", () => {
    const normalized = normalizeAgent({
      id: "agent-1",
      name: "Agent",
      status: "offline",
      last_seen_at: null,
      system_info: "not-json",
      created_at: "2026-05-21T05:17:11Z",
    });

    expect(normalized.last_seen).toBe("");
    expect(normalized.hostname).toBe("");
    expect(normalized.os).toBe("");
    expect(normalized.arch).toBe("");
    expect(normalized.version).toBe("");
    expect(normalized.capabilities).toEqual([]);
  });

  it("normalizes list and detail responses from the API", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        ok: true,
        data: [{
          id: "agent-1",
          name: "Agent",
          status: "online",
          last_seen_at: "2026-05-21T05:20:52Z",
          system_info: "{\"hostname\":\"host-a\",\"os\":\"linux\",\"arch\":\"arm64\"}",
          created_at: "2026-05-21T05:17:11Z",
        }],
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        ok: true,
        data: {
          id: "agent-2",
          name: "Agent 2",
          status: "offline",
          last_seen_at: null,
          system_info: "{}",
          created_at: "2026-05-21T05:17:11Z",
        },
      }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listAgents()).resolves.toMatchObject([{ id: "agent-1", hostname: "host-a", arch: "arm64" }]);
    await expect(getAgent("agent-2")).resolves.toMatchObject({ id: "agent-2", last_seen: "" });
  });
});
