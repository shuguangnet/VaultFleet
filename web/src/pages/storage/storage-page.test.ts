import { describe, expect, it } from "vitest";
import { STORAGE_TEMPLATES } from "./storage-page";

describe("storage templates", () => {
  it("includes OpenStack Swift with common rclone fields", () => {
    const swift = STORAGE_TEMPLATES.swift;

    expect(swift.name).toBe("OpenStack Swift");
    expect(swift.defaults).toEqual({
      auth_version: "3",
      domain: "default",
      tenant_domain: "default",
    });
    expect(swift.fields.map((field) => field.key)).toEqual([
      "auth",
      "user",
      "key",
      "tenant",
      "domain",
      "tenant_domain",
      "auth_version",
      "region",
      "container",
    ]);
    expect(swift.fields.find((field) => field.key === "key")?.type).toBe("password");
  });
});
