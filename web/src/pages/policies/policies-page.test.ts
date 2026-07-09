import { describe, expect, it } from "vitest";
import {
  buildArtifactContextOptions,
  cleanRcloneArgs,
  defaultPolicyInput,
  defaultRcloneArgs,
  normalizePolicyHook,
  submitRcloneArgs,
} from "./policies-page";

describe("policy rclone args helpers", () => {
  it("returns WebDAV transfer defaults", () => {
    expect(defaultRcloneArgs("webdav")).toEqual({
      transfers: "2",
      tpslimit: "4",
      retries: "10",
      "retries-sleep": "10s",
      "low-level-retries": "20",
      timeout: "10m0s",
    });
  });

  it("does not set defaults for non-WebDAV storage", () => {
    expect(defaultRcloneArgs("s3")).toEqual({});
    expect(defaultRcloneArgs("")).toEqual({});
  });

  it("trims rclone args and omits empty values", () => {
    expect(
      cleanRcloneArgs({
        transfers: " 2 ",
        tpslimit: "",
        retries: "   ",
        timeout: " 10m0s",
        "local-no-check-updated": " true ",
      }),
    ).toEqual({
      transfers: "2",
      timeout: "10m0s",
      "local-no-check-updated": "true",
    });
  });

  it("returns undefined when no rclone args remain after cleaning", () => {
    expect(cleanRcloneArgs({ transfers: " ", timeout: "" })).toBeUndefined();
    expect(cleanRcloneArgs({})).toBeUndefined();
  });

  it("sends an empty object to clear saved args when editing", () => {
    expect(submitRcloneArgs({ transfers: " ", timeout: "" }, true)).toEqual({});
    expect(
      submitRcloneArgs({ transfers: " ", timeout: "" }, false),
    ).toBeUndefined();
  });

  it("defaults policy task timeout to 6 hours", () => {
    expect(defaultPolicyInput().timeout_hours).toBe(6);
  });

  it("defaults recoverability verification to disabled safe settings", () => {
    expect(defaultPolicyInput().verification).toEqual({
      enabled: false,
      schedule: "0 4 * * *",
      sample_count: 10,
      sample_restore_enabled: false,
      timeout_minutes: 60,
    });
  });

  it("normalizes empty hooks to undefined", () => {
    expect(
      normalizePolicyHook({ command: "   ", timeout_seconds: 120 }),
    ).toBeUndefined();
  });

  it("trims commands and omits zero timeout values", () => {
    expect(
      normalizePolicyHook({
        command: "  docker exec db pg_dump  ",
        timeout_seconds: 0,
      }),
    ).toEqual({
      command: "docker exec db pg_dump",
    });
  });

  it("builds artifact context suggestions from selected backup sources", () => {
    expect(
      buildArtifactContextOptions(
        {
          repo_path: "vaultfleet/node-1",
          backup_dirs: ["/var/www/example.com"],
          backup_sources: [
            {
              type: "docker_container",
              docker_container: {
                name: "web",
                compose_project: "cliproxyapi",
                compose_service: "api",
                include_bind_mounts: true,
                include_volumes: true,
                include_compose_files: true,
              },
            },
            {
              type: "database",
              database: {
                engine: "mysql",
                execution_mode: "host",
                username: "root",
                database: "wordpress",
                all_databases: false,
              },
            },
          ],
        },
        "example-site",
      ).map((option) => option.value),
    ).toEqual([
      "example-site",
      "example.com",
      "cliproxyapi",
      "api",
      "web",
      "wordpress",
      "mysql-wordpress",
      "node-1",
    ]);
  });
});
