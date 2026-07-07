import { afterEach, describe, expect, it, vi } from "vitest";
import { checkAuth } from "./auth";

afterEach(() => vi.restoreAllMocks());

describe("auth service", () => {
  it("normalizes authenticated auth checks from the backend username field", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            ok: true,
            data: {
              initialized: true,
              authenticated: true,
              username: "admin",
              role: "operator",
              permissions: ["read:operational"],
            },
          }),
          { status: 200 },
        ),
      ),
    );

    await expect(checkAuth()).resolves.toMatchObject({
      initialized: true,
      authenticated: true,
      user: { username: "admin", role: "operator", permissions: ["read:operational"] },
    });
  });
});
