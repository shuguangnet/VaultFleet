import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { listAgents, backupNow, discoverDatabaseAgent, discoverDockerAgent, listAgentTags } from "@/services/agents";
import {
  bulkAssignPolicy,
  createPolicy,
  copyPolicy,
  deletePolicy,
  listPolicies,
  previewArtifactNaming,
  updatePolicy,
} from "@/services/policies";
import { listStorage } from "@/services/storage";
import { PoliciesPage } from "./policies-page";

vi.mock("@/services/agents", () => ({
  backupNow: vi.fn(),
  discoverDatabaseAgent: vi.fn(),
  discoverDockerAgent: vi.fn(),
  listAgentTags: vi.fn(),
  listAgents: vi.fn(),
}));

vi.mock("@/services/policies", () => ({
  bulkAssignPolicy: vi.fn(),
  createPolicy: vi.fn(),
  copyPolicy: vi.fn(),
  deletePolicy: vi.fn(),
  listPolicies: vi.fn(),
  previewArtifactNaming: vi.fn(),
  updatePolicy: vi.fn(),
}));

vi.mock("@/services/storage", () => ({
  listStorage: vi.fn(),
}));

vi.mock("@/components/directory-browser", () => ({
  DirectoryBrowser: () => null,
}));

beforeAll(() => {
  if (!Element.prototype.hasPointerCapture) {
    Element.prototype.hasPointerCapture = vi.fn(() => false);
  }
  if (!Element.prototype.setPointerCapture) {
    Element.prototype.setPointerCapture = vi.fn();
  }
  if (!Element.prototype.releasePointerCapture) {
    Element.prototype.releasePointerCapture = vi.fn();
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = vi.fn();
  }
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

beforeEach(() => {
  vi.mocked(listAgentTags).mockResolvedValue([]);
  vi.mocked(copyPolicy).mockResolvedValue({} as never);
  vi.mocked(discoverDatabaseAgent).mockResolvedValue({
    available: true,
    databases: [],
  });
  vi.mocked(bulkAssignPolicy).mockResolvedValue({
    source_policy_id: "policy-1",
    target_tags: [],
    requested_count: 0,
    matched_count: 0,
    created_count: 0,
    failed_count: 0,
    results: [],
  });
  vi.mocked(previewArtifactNaming).mockResolvedValue({
    context_name: "node-1",
    source_type: "path",
  });
});

describe("PoliciesPage rclone form state", () => {
  it("switches schedule modes and edits custom retention", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(createPolicy).mockResolvedValue({} as never);
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp><PoliciesPage /></AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    expect(screen.getByLabelText("执行时间")).toHaveValue("02:00");

    const frequency = screen.getByLabelText("执行频率");
    await user.click(frequency);
    const weeklyOptions = await screen.findAllByText("每周指定日期");
    const weeklyOption = weeklyOptions.map((node) => node.closest(".ant-select-item-option")).find(Boolean);
    expect(weeklyOption).toBeTruthy();
    await user.click(weeklyOption!);
    expect(screen.getByRole("group", { name: "执行星期" })).toBeInTheDocument();
    expect(screen.getByText(/每周一 02:00/)).toBeInTheDocument();

    await clickButtonByText(user, "自定义");
    await user.click(screen.getByLabelText("保留最近 N 次"));
    await user.keyboard("{Control>}a{/Control}7");
    expect(screen.getByLabelText("保留最近 N 次")).toHaveValue("7");
  });

  it("resets WebDAV rclone defaults after successful create", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-webdav",
        name: "WebDAV Store",
        rclone_type: "webdav",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "",
      storage_id: "storage-webdav",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/",
      backup_dirs: [],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({
      command_id: "cmd-1",
      message_id: "msg-1",
    });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[1]);
    const webdavOption = (await screen.findAllByText("WebDAV Store")).find(
      (el) => el.tagName !== "OPTION",
    );
    expect(webdavOption).toBeDefined();
    await user.click(webdavOption!);

    expect(screen.getByLabelText("并发传输数")).toHaveValue("2");

    await clickButtonByText(user, "提交策略");
    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));

    await clickButtonByText(user, "添加策略");

    expect(screen.queryByLabelText("并发传输数")).not.toBeInTheDocument();
  });

  it("submits the configured timeout hours", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: ["/data"],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 12,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({
      command_id: "cmd-1",
      message_id: "msg-1",
    });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");
    await user.click(screen.getAllByRole("combobox")[1]);
    await clickOptionByName("S3 Store");
    await user.type(screen.getByRole("textbox", { name: "备份目录" }), "/data");
    await user.clear(screen.getByLabelText("任务超时（小时）"));
    await user.type(screen.getByLabelText("任务超时（小时）"), "12");
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0]).toEqual(
      expect.objectContaining({ timeout_hours: 12 }),
    );
  });

  it("submits configured backup hooks and shows docker guidance", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: ["/srv/app/data"],
      exclude_patterns: [],
      pre_backup_hook: {
        command: "docker exec db pg_dump",
        timeout_seconds: 180,
      },
      post_backup_hook: {
        command: "docker compose start app",
        timeout_seconds: 30,
      },
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({
      command_id: "cmd-1",
      message_id: "msg-1",
    });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    expect(
      screen.getByText(/不备份镜像层，也不会自动重建容器/),
    ).toBeInTheDocument();

    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");
    await user.click(screen.getAllByRole("combobox")[1]);
    await clickOptionByName("S3 Store");
    await user.type(
      screen.getByRole("textbox", { name: "备份目录" }),
      "/srv/app/data",
    );
    await user.type(
      screen.getByLabelText("备份前命令"),
      "docker exec db pg_dump",
    );
    await user.clear(screen.getByLabelText("备份前命令超时（秒）"));
    await user.type(screen.getByLabelText("备份前命令超时（秒）"), "180");
    await user.type(
      screen.getByLabelText("备份后命令"),
      "docker compose start app",
    );
    await user.clear(screen.getByLabelText("备份后命令超时（秒）"));
    await user.type(screen.getByLabelText("备份后命令超时（秒）"), "30");
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0]).toEqual(
      expect.objectContaining({
        pre_backup_hook: {
          command: "docker exec db pg_dump",
          timeout_seconds: 180,
        },
        post_backup_hook: {
          command: "docker compose start app",
          timeout_seconds: 30,
        },
      }),
    );
  }, 10000);

  it("discovers and submits Docker container backup sources", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        capabilities: ["docker_workload_backups"],
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(discoverDockerAgent).mockResolvedValue({
      available: true,
      containers: [
        {
          id: "container-1",
          names: ["db"],
          image: "postgres:16",
          state: "running",
          compose: { project: "app", service: "db" },
          mounts: [{ type: "volume", name: "db-data", source: "/var/lib/docker/volumes/db-data/_data", destination: "/var/lib/postgresql/data", rw: true }],
          selectable: true,
        },
      ],
    });
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: [],
      backup_sources: [],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");
    await user.click(screen.getAllByRole("combobox")[1]);
    await clickOptionByName("S3 Store");
    expect(await screen.findByText("postgres:16")).toBeInTheDocument();
    const dockerCheckboxes = screen.getAllByRole("checkbox");
    await user.click(dockerCheckboxes[dockerCheckboxes.length - 1]);
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0].backup_sources).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          type: "docker_container",
          docker_container: expect.objectContaining({
            container_id: "container-1",
            include_bind_mounts: true,
            include_volumes: true,
          }),
        }),
      ]),
    );
  });

  it("adds and submits a PostgreSQL database backup source", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        capabilities: ["database_backups"],
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: [],
      backup_sources: [],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");
    await user.click(screen.getAllByRole("combobox")[1]);
    await clickOptionByName("S3 Store");
    await clickButtonByText(user, "添加数据库");
    await user.type(screen.getByLabelText("数据库用户"), "postgres");
    await user.type(screen.getByLabelText("数据库密码"), "secret");
    await user.type(screen.getByLabelText("数据库名"), "app");
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0].backup_sources).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          type: "database",
          database: expect.objectContaining({
            engine: "postgresql",
            execution_mode: "host",
            username: "postgres",
            password: "secret",
            database: "app",
            compress: true,
          }),
        }),
      ]),
    );
  });

  it("loads database names and submits the selected database", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        capabilities: ["database_backups"],
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([
      {
        id: "storage-1",
        name: "S3 Store",
        rclone_type: "s3",
        rclone_config: {},
        created_at: "2026-05-25T00:00:00Z",
        updated_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(discoverDatabaseAgent).mockResolvedValue({
      available: true,
      databases: ["app", "analytics"],
    });
    vi.mocked(createPolicy).mockResolvedValue({
      id: "policy-1",
      agent_id: "agent-1",
      storage_id: "storage-1",
      backup_mode: "snapshot",
      repo_path: "vaultfleet/node-1",
      backup_dirs: [],
      backup_sources: [],
      exclude_patterns: [],
      schedule: "0 2 * * *",
      retention: {},
      timeout_hours: 6,
      synced: false,
      created_at: "2026-05-25T00:00:00Z",
      updated_at: "2026-05-25T00:00:00Z",
    });
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");
    await user.click(screen.getAllByRole("combobox")[1]);
    await clickOptionByName("S3 Store");
    await clickButtonByText(user, "添加数据库");
    await user.type(screen.getByLabelText("数据库用户"), "postgres");
    await user.type(screen.getByLabelText("数据库密码"), "secret");
    await clickButtonByText(user, "加载");

    await waitFor(() => expect(discoverDatabaseAgent).toHaveBeenCalledTimes(1));
    expect(discoverDatabaseAgent).toHaveBeenCalledWith(
      "agent-1",
      expect.objectContaining({
        engine: "postgresql",
        execution_mode: "host",
        username: "postgres",
        password: "secret",
        all_databases: true,
        database: "",
      }),
    );

    await user.type(screen.getByLabelText("数据库名"), "analytics");
    fireEvent.submit(screen.getByRole("form", { name: "备份策略表单" }));

    await waitFor(() => expect(createPolicy).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createPolicy).mock.calls[0][0].backup_sources).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          type: "database",
          database: expect.objectContaining({
            database: "analytics",
            all_databases: false,
          }),
        }),
      ]),
    );
  });

  it("does not discover Docker containers for unsupported agents", async () => {
    const user = userEvent.setup();
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listAgents).mockResolvedValue([
      {
        id: "agent-1",
        name: "node-1",
        status: "online",
        last_seen: "",
        version: "",
        hostname: "",
        os: "",
        arch: "",
        capabilities: [],
        created_at: "2026-05-25T00:00:00Z",
      },
    ]);
    vi.mocked(listStorage).mockResolvedValue([]);
    vi.mocked(createPolicy).mockResolvedValue({} as never);
    vi.mocked(updatePolicy).mockResolvedValue({} as never);
    vi.mocked(deletePolicy).mockResolvedValue({} as never);
    vi.mocked(backupNow).mockResolvedValue({ command_id: "cmd-1", message_id: "msg-1" });

    render(
      <QueryClientProvider client={newTestQueryClient()}>
        <AntdApp>
          <PoliciesPage />
        </AntdApp>
      </QueryClientProvider>,
    );

    await clickButtonByText(user, "添加策略");
    await user.click(screen.getAllByRole("combobox")[0]);
    await clickOptionByName("node-1");

    expect(await screen.findByText("当前 Agent 未上报 Docker 备份能力。")).toBeInTheDocument();
    expect(discoverDockerAgent).not.toHaveBeenCalled();
  });
});

function newTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: {
        retry: false,
      },
      queries: {
        retry: false,
      },
    },
  });
}

async function clickButtonByText(
  user: ReturnType<typeof userEvent.setup>,
  text: string,
) {
  const label = await screen.findByText(text);
  const button = label.closest("button");
  expect(button).toBeTruthy();
  await user.click(button!);
}

async function clickOptionByName(name: string) {
  const option = await screen.findByRole("option", { name });
  fireEvent.click(option);
}
