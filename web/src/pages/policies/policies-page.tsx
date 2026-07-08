import { useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Col,
  Drawer,
  Dropdown,
  Empty,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  AlertOutlined,
  DatabaseOutlined,
  DeleteOutlined,
  DeploymentUnitOutlined,
  EditOutlined,
  EllipsisOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  backupNow,
  discoverDockerAgent,
  listAgentTags,
  listAgents,
} from "@/services/agents";
import { listStorage } from "@/services/storage";
import {
  createPolicy,
  deletePolicy,
  bulkAssignPolicy,
  listPolicies,
  updatePolicy,
  verifyPolicyNow,
} from "@/services/policies";
import {
  describeCron,
} from "@/lib/cron";
import {
  safeFormatDate,
} from "@/lib/date";
import type {
  BackupPolicy,
  BackupSource,
  BulkAssignPolicyResponse,
  DatabaseBackupSource,
  DockerContainerBackupSource,
  PolicyHook,
  PolicyInput,
  RetentionConfig,
} from "@/types/policy";
import type { DockerContainer } from "@/types/api";
import { ErrorPanel } from "@/components/error-panel";
import { DirectoryBrowser } from "@/components/directory-browser";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { useAuth } from "@/contexts/auth-context";
import { permissions } from "@/services/identity";

const RETENTION_PRESETS: Record<
  string,
  {
    label: string;
    description: string;
    values: RetentionConfig;
  }
> = {
  basic: {
    label: "基础",
    description: "约 3 个月深度，适合非关键数据",
    values: { keep_last: 7, keep_daily: 7, keep_weekly: 4, keep_monthly: 3 },
  },
  standard: {
    label: "标准",
    description: "约半年深度，适合大多数场景",
    values: { keep_last: 10, keep_daily: 7, keep_weekly: 4, keep_monthly: 6 },
  },
  archive: {
    label: "长期归档",
    description: "约 1 年深度，适合重要业务数据",
    values: { keep_last: 10, keep_daily: 14, keep_weekly: 8, keep_monthly: 12 },
  },
  custom: {
    label: "自定义",
    description: "手动设置各维度保留数量",
    values: { keep_last: 7, keep_daily: 7, keep_weekly: 4, keep_monthly: 6 },
  },
};

const RCLONE_ARG_FIELDS = [
  { key: "transfers", label: "并发传输数", description: "同时上传的文件数", defaultValue: "4", webdavValue: "2" },
  { key: "tpslimit", label: "每秒请求数", description: "限制每秒 HTTP 请求数，0 = 不限", defaultValue: "0", webdavValue: "4" },
  { key: "retries", label: "重试次数", description: "失败后重试次数", defaultValue: "3", webdavValue: "10" },
  { key: "retries-sleep", label: "重试间隔", description: "重试之间的等待时间（如 10s）", defaultValue: "0", webdavValue: "10s" },
  { key: "low-level-retries", label: "底层重试", description: "底层 IO 错误重试次数", defaultValue: "10", webdavValue: "20" },
  { key: "timeout", label: "请求超时", description: "单次 HTTP 请求超时（如 5m0s）", defaultValue: "5m0s", webdavValue: "10m0s" },
];

export function defaultPolicyInput(): PolicyInput {
  return {
    agent_id: "",
    storage_id: "",
    backup_mode: "snapshot",
    archive_format: "tar.gz",
    repo_path: "",
    restic_password: "",
    backup_dirs: [],
    exclude_patterns: ["/tmp", "/proc", "/sys", "/dev"],
    pre_backup_hook: { command: "", timeout_seconds: 300 },
    post_backup_hook: { command: "", timeout_seconds: 300 },
    schedule: "0 2 * * *",
    retention: { keep_last: 10, keep_daily: 7, keep_weekly: 4, keep_monthly: 6 },
    rclone_args: {},
    timeout_hours: 6,
    backup_sources: [],
    verification: {
      enabled: false,
      schedule: "0 4 * * *",
      sample_count: 10,
      sample_restore_enabled: false,
      timeout_minutes: 60,
    },
  };
}

export function normalizePolicyHook(hook?: PolicyHook): PolicyHook | undefined {
  if (!hook) return undefined;
  const command = hook.command.trim();
  if (!command) return undefined;
  return {
    command,
    timeout_seconds:
      hook.timeout_seconds && hook.timeout_seconds > 0
        ? hook.timeout_seconds
        : undefined,
  };
}

export function defaultRcloneArgs(storageType: string): Record<string, string> {
  if (storageType.toLowerCase() !== "webdav") return {};
  return Object.fromEntries(
    RCLONE_ARG_FIELDS.map((f) => [f.key, f.webdavValue])
  );
}

export function cleanRcloneArgs(args?: Record<string, string>) {
  const cleaned = Object.fromEntries(
    Object.entries(args ?? {})
      .map(([k, v]) => [k, v.trim()] as const)
      .filter(([, v]) => v.length > 0)
  );
  return Object.keys(cleaned).length > 0 ? cleaned : undefined;
}

function rcloneBooleanArgEnabled(args: Record<string, string> | undefined, key: string): boolean {
  const value = args?.[key]?.trim().toLowerCase();
  return value === "true" || value === "1" || value === "yes" || value === "on";
}

function setRcloneBooleanArg(args: Record<string, string> | undefined, key: string, enabled: boolean): Record<string, string> {
  const next = { ...(args ?? {}) };
  if (enabled) {
    next[key] = "true";
  } else {
    delete next[key];
  }
  return next;
}

export function submitRcloneArgs(
  args: Record<string, string> | undefined,
  clearWhenEmpty: boolean
) {
  const cleaned = cleanRcloneArgs(args);
  if (cleaned || !clearWhenEmpty) return cleaned;
  return {};
}

function dockerSourcesFromPolicy(policy: BackupPolicy): BackupSource[] {
  return (policy.backup_sources ?? []).filter(
    (s) => s.type === "docker_container"
  );
}

function databaseSourcesFromPolicy(policy: BackupPolicy): BackupSource[] {
  return (policy.backup_sources ?? []).filter((s) => s.type === "database");
}

function verificationLabel(policy: BackupPolicy) {
  const latest = policy.latest_verification;
  if (!policy.verification?.enabled) return { text: "未启用", status: "default" as const };
  if (!latest) return { text: "从未验证", status: "warning" as const };
  if (latest.status === "success") return { text: "通过", status: "success" as const };
  if (latest.status === "running" || latest.status === "pending") return { text: "运行中", status: "processing" as const };
  return { text: "失败", status: "error" as const };
}

function buildBackupSources(input: PolicyInput): BackupSource[] {
  const pathSources = input.backup_dirs
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => ({ type: "path" as const, path: p }));
  const dockerSources = (input.backup_sources ?? []).filter(
    (s) => s.type === "docker_container"
  );
  const databaseSources = (input.backup_sources ?? []).filter(
    (s) => s.type === "database"
  );
  return [...pathSources, ...dockerSources, ...databaseSources];
}

function dockerSourceKey(s: DockerContainerBackupSource): string {
  if (s.container_id) return `id:${s.container_id}`;
  if (s.compose_project && s.compose_service)
    return `compose:${s.compose_project}:${s.compose_service}`;
  return `name:${s.name ?? ""}`;
}

function containerKey(c: DockerContainer): string {
  const compose = c.compose ?? {};
  if (c.id) return `id:${c.id}`;
  if (compose.project && compose.service)
    return `compose:${compose.project}:${compose.service}`;
  return `name:${c.names?.[0] ?? ""}`;
}

function sourceFromContainer(c: DockerContainer): BackupSource {
  const compose = c.compose ?? {};
  return {
    type: "docker_container",
    docker_container: {
      container_id: c.id,
      name: c.names?.[0] ?? "",
      image: c.image,
      labels: c.labels ?? {},
      compose_project: compose.project,
      compose_service: compose.service,
      compose_working_dir: compose.working_dir,
      compose_config_files: compose.config_files ?? [],
      include_bind_mounts: true,
      include_volumes: true,
      include_compose_files: true,
    },
  };
}

function defaultDatabaseSource(): BackupSource {
  return {
    type: "database",
    database: {
      engine: "postgresql",
      execution_mode: "host",
      host: "127.0.0.1",
      port: 5432,
      username: "",
      database: "",
      all_databases: false,
      compress: true,
      dump_timeout_seconds: 600,
    },
  };
}

function databaseSourceLabel(source: DatabaseBackupSource): string {
  const engine = source.engine === "mysql" ? "MySQL" : "PostgreSQL";
  const scope = source.all_databases ? "全部数据库" : source.database || "未选择数据库";
  return `${engine} / ${scope}`;
}

function detectRetentionPreset(r: RetentionConfig): string {
  for (const [key, preset] of Object.entries(RETENTION_PRESETS)) {
    if (key === "custom") continue;
    const v = preset.values;
    if (
      (r.keep_last ?? 0) === v.keep_last &&
      (r.keep_daily ?? 0) === v.keep_daily &&
      (r.keep_weekly ?? 0) === v.keep_weekly &&
      (r.keep_monthly ?? 0) === v.keep_monthly
    )
      return key;
  }
  return "custom";
}

export function PoliciesPage() {
  const { message } = App.useApp();
  const auth = useAuth();
  const canWritePolicies = auth.hasPermission(permissions.writePolicies);
  const canRunBackup = auth.hasPermission(permissions.runBackup);
  const queryClient = useQueryClient();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [confirmBackupAgentId, setConfirmBackupAgentId] = useState<
    string | null
  >(null);
  const [bulkSourcePolicy, setBulkSourcePolicy] = useState<BackupPolicy | null>(
    null,
  );
  const [bulkAgentIds, setBulkAgentIds] = useState<string[]>([]);
  const [bulkTags, setBulkTags] = useState<string[]>([]);
  const [bulkResult, setBulkResult] =
    useState<BulkAssignPolicyResponse | null>(null);
  const [retentionPreset, setRetentionPreset] = useState("standard");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const preBackupTimeout = Form.useWatch(
    ["pre_backup_hook", "timeout_seconds"],
    { preserve: true }
  );

  const [formData, setFormData] = useState<PolicyInput>(() =>
    defaultPolicyInput()
  );

  const { data: policies, isLoading } = useQuery({
    queryKey: ["policies"],
    queryFn: () => listPolicies(),
  });
  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });
  const { data: knownTags } = useQuery({
    queryKey: ["agent-tags"],
    queryFn: listAgentTags,
  });
  const { data: storageList } = useQuery({
    queryKey: ["storage"],
    queryFn: listStorage,
  });

  const resetPolicyFormState = () => {
    setEditingId(null);
    setFormData(defaultPolicyInput());
    setRetentionPreset("standard");
    setAdvancedOpen(false);
  };

  const createMutation = useMutation({
    mutationFn: createPolicy,
    onSuccess: () => {
      resetPolicyFormState();
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["policies"] });
      message.success("策略已创建");
    },
    onError: (error: any) =>
      message.error("创建策略失败: " + error.message),
  });

  const updateMutation = useMutation({
    mutationFn: (data: PolicyInput) => updatePolicy(editingId!, data),
    onSuccess: () => {
      resetPolicyFormState();
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["policies"] });
      message.success("策略已更新");
    },
    onError: (error: any) =>
      message.error("更新策略失败: " + error.message),
  });

  const deleteMutation = useMutation({
    mutationFn: deletePolicy,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["policies"] });
      message.success("策略已删除");
    },
    onError: (error: any) =>
      message.error("删除策略失败: " + error.message),
  });

  const backupMutation = useMutation({
    mutationFn: (agentId: string) => backupNow(agentId),
    onSuccess: (data) => {
      setConfirmBackupAgentId(null);
      const agent = agents?.find((a) => a.id === confirmBackupAgentId);
      if (agent?.status === "online") {
        message.success(`备份命令已下发 (Message ID: ${data.message_id})`);
      } else {
        message.info("备份命令已排队，节点上线后将自动执行");
      }
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const verifyMutation = useMutation({
    mutationFn: (policyId: string) => verifyPolicyNow(policyId),
    onSuccess: (data) => {
      message.success(`验证任务已创建 (Message ID: ${data.message_id})`);
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const bulkAssignMutation = useMutation({
    mutationFn: bulkAssignPolicy,
    onSuccess: (result) => {
      setBulkResult(result);
      queryClient.invalidateQueries({ queryKey: ["policies"] });
      message.success(`已创建 ${result.created_count} 条策略`);
    },
    onError: (error: any) => message.error(error.message || "批量下发失败"),
  });

  const handleEdit = (policy: BackupPolicy) => {
    setEditingId(policy.id);
    const repoSuffix = policy.repo_path.startsWith("vaultfleet/")
      ? policy.repo_path.slice("vaultfleet/".length)
      : policy.repo_path;
    setFormData({
      agent_id: policy.agent_id,
      storage_id: policy.storage_id,
      backup_mode: policy.backup_mode ?? "snapshot",
      archive_format: policy.archive_format ?? "tar.gz",
      repo_path: repoSuffix,
      backup_dirs: policy.backup_dirs,
      exclude_patterns: policy.exclude_patterns,
      pre_backup_hook: policy.pre_backup_hook ?? {
        command: "",
        timeout_seconds: 300,
      },
      post_backup_hook: policy.post_backup_hook ?? {
        command: "",
        timeout_seconds: 300,
      },
      schedule: policy.schedule,
      retention: policy.retention,
      rclone_args: policy.rclone_args || {},
      timeout_hours: policy.timeout_hours || 6,
      backup_sources: [
        ...dockerSourcesFromPolicy(policy),
        ...databaseSourcesFromPolicy(policy),
      ],
      verification: policy.verification ?? {
        enabled: false,
        schedule: "0 4 * * *",
        sample_count: 10,
        sample_restore_enabled: false,
        timeout_minutes: 60,
      },
    });
    setRetentionPreset(detectRetentionPreset(policy.retention));
    setAdvancedOpen(!!cleanRcloneArgs(policy.rclone_args));
    setDrawerOpen(true);
  };

  const openBulkAssign = (policy: BackupPolicy) => {
    setBulkSourcePolicy(policy);
    setBulkAgentIds([]);
    setBulkTags([]);
    setBulkResult(null);
    bulkAssignMutation.reset();
  };

  const closeBulkAssign = () => {
    setBulkSourcePolicy(null);
    setBulkAgentIds([]);
    setBulkTags([]);
    setBulkResult(null);
    bulkAssignMutation.reset();
  };

  const submitBulkAssign = () => {
    if (!bulkSourcePolicy) return;
    if (bulkAgentIds.length === 0 && bulkTags.length === 0) {
      message.warning("请选择目标节点或标签");
      return;
    }
    bulkAssignMutation.mutate({
      source_policy_id: bulkSourcePolicy.id,
      target_agent_ids: bulkAgentIds,
      target_tags: bulkTags,
    });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const submitData = {
      ...formData,
      repo_path: "vaultfleet/" + formData.repo_path,
      backup_sources: buildBackupSources(formData),
      rclone_args: submitRcloneArgs(formData.rclone_args, !!editingId),
      pre_backup_hook: normalizePolicyHook(formData.pre_backup_hook),
      post_backup_hook: normalizePolicyHook(formData.post_backup_hook),
      timeout_hours: formData.timeout_hours || 6,
      verification:
        formData.backup_mode === "snapshot"
          ? formData.verification
          : { ...(formData.verification ?? { enabled: false }), enabled: false },
    };
    if (editingId) updateMutation.mutate(submitData);
    else createMutation.mutate(submitData);
  };

  const selectedAgent = agents?.find((a) => a.id === formData.agent_id);
  const isAgentOnline = selectedAgent?.status === "online";
  const dockerCapable = !!selectedAgent?.capabilities?.includes(
    "docker_workload_backups"
  );
  const databaseCapable = !!selectedAgent?.capabilities?.includes(
    "database_backups"
  );
  const selectedDockerSources = (formData.backup_sources ?? []).filter(
    (s) => s.type === "docker_container"
  );
  const selectedDatabaseSources = (formData.backup_sources ?? []).filter(
    (s) => s.type === "database"
  );
  const selectedDockerKeys = new Set(
    selectedDockerSources
      .map((s) => s.docker_container)
      .filter((s): s is DockerContainerBackupSource => !!s)
      .map(dockerSourceKey)
  );

  const dockerDiscoveryQuery = useQuery({
    queryKey: ["agent-docker", formData.agent_id],
    queryFn: () => discoverDockerAgent(formData.agent_id),
    enabled: drawerOpen && !!formData.agent_id && isAgentOnline && dockerCapable,
  });

  const setDatabaseSources = (databaseSources: BackupSource[]) => {
    setFormData({
      ...formData,
      backup_sources: [...selectedDockerSources, ...databaseSources],
    });
  };

  const updateDatabaseSource = (
    index: number,
    updater: (source: DatabaseBackupSource) => DatabaseBackupSource,
  ) => {
    const next = selectedDatabaseSources.map((source, i) => {
      if (i !== index || !source.database) return source;
      return { type: "database" as const, database: updater(source.database) };
    });
    setDatabaseSources(next);
  };

  const columns: ColumnsType<BackupPolicy> = [
    {
      title: "节点",
      dataIndex: "agent_id",
      key: "agent_id",
      render: (v: string) =>
        agents?.find((a) => a.id === v)?.name || v,
    },
    {
      title: "调度",
      dataIndex: "schedule",
      key: "schedule",
      render: (v: string) => (
        <div>
          <Typography.Text code style={{ fontSize: 12 }}>
            {v}
          </Typography.Text>
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              {describeCron(v)}
            </Typography.Text>
          </div>
        </div>
      ),
    },
    {
      title: "同步状态",
      dataIndex: "synced",
      key: "synced",
      render: (v: boolean) => <StatusBadge status={v ? "success" : "unsynced"} />,
    },
    {
      title: "验证状态",
      key: "verification",
      render: (_, record) => {
        const state = verificationLabel(record);
        const color =
          state.status === "success"
            ? "green"
            : state.status === "error"
              ? "red"
              : state.status === "processing"
                ? "blue"
                : state.status === "warning"
                  ? "gold"
                  : "default";
        return (
          <div>
            <Tag color={color}>{state.text}</Tag>
            {record.latest_verification?.checked_at && (
              <div>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  {safeFormatDate(record.latest_verification.checked_at, "yyyy-MM-dd HH:mm")}
                </Typography.Text>
              </div>
            )}
          </div>
        );
      },
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      responsive: ["md"],
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd HH:mm")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Dropdown
          menu={{
            items: [
              canWritePolicies ? {
                key: "edit",
                icon: <EditOutlined />,
                label: "编辑",
                onClick: () => handleEdit(record),
              } : null,
              canWritePolicies ? {
                key: "bulk-assign",
                icon: <DeploymentUnitOutlined />,
                label: "批量下发",
                onClick: () => openBulkAssign(record),
              } : null,
              canRunBackup ? {
                key: "backup",
                icon: <PlayCircleOutlined />,
                label: "立即备份",
                onClick: () => setConfirmBackupAgentId(record.agent_id),
              } : null,
              canRunBackup ? {
                key: "verify",
                icon: <SafetyCertificateOutlined />,
                label: "立即验证",
                disabled:
                  record.backup_mode === "archive" ||
                  !agents
                    ?.find((a) => a.id === record.agent_id)
                    ?.capabilities?.includes("backup_verification"),
                onClick: () => verifyMutation.mutate(record.id),
              } : null,
              canWritePolicies ? { type: "divider" as const } : null,
              canWritePolicies ? {
                key: "delete",
                icon: <DeleteOutlined />,
                label: (
                  <Popconfirm
                    title="确认删除备份策略？"
                    description="此操作将停止该节点的自动备份任务。存储中的备份数据不会被删除。"
                    okText="确认删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => deleteMutation.mutate(record.id)}
                  >
                    <span style={{ color: "#c53030" }}>删除</span>
                  </Popconfirm>
                ),
              } : null,
            ].filter(Boolean) as any,
          }}
          trigger={["click"]}
        >
          <Button type="text" icon={<EllipsisOutlined />} />
        </Dropdown>
      ),
    },
  ];

  const openAdd = () => {
    resetPolicyFormState();
    setDrawerOpen(true);
  };

  return (
    <div className="vf-page">
      <PageHeader
        title="备份策略"
        description="节点 / 存储 / 调度 / 保留"
        icon={<SafetyCertificateOutlined />}
        actions={
          canWritePolicies ? <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
            添加策略
          </Button> : null
        }
      />

      <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
        <Table<BackupPolicy>
          columns={columns}
          dataSource={policies || []}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 720 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无备份策略"
              />
            ),
          }}
        />
      </Card>

      <Drawer
        title={editingId ? "编辑策略" : "添加新策略"}
        open={drawerOpen}
        onClose={() => {
          setDrawerOpen(false);
          resetPolicyFormState();
          createMutation.reset();
          updateMutation.reset();
        }}
        width="min(100vw, 640px)"
        destroyOnClose
        footer={
          <div
            className="vf-drawer-footer"
            style={{
              padding: "10px 16px",
              background: "#fff",
              borderTop: "1px solid #f0f0f0",
            }}
          >
            <Button
              type="primary"
              block
              size="large"
              loading={createMutation.isPending || updateMutation.isPending}
              onClick={handleSubmit as any}
            >
              提交策略
            </Button>
          </div>
        }
      >
        <form onSubmit={handleSubmit} aria-label="备份策略表单" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <ErrorPanel
            error={(createMutation.error || updateMutation.error) as any}
          />
          <Row gutter={[12, 12]}>
            <Col xs={24} sm={12}>
              <Typography.Text strong>选择节点</Typography.Text>
              <Select
                style={{ width: "100%", marginTop: 4 }}
                value={formData.agent_id || undefined}
                placeholder="请选择节点"
                disabled={!!editingId}
                onChange={(val) => {
                  const agent = agents?.find((a) => a.id === val);
                  const updates: Partial<PolicyInput> = {
                    agent_id: val,
                    backup_sources: [],
                  };
                  if (!editingId && agent) updates.repo_path = agent.name;
                  setFormData({ ...formData, ...updates });
                }}
                virtual={false}
                options={agents?.map((a) => ({
                  value: a.id,
                  label: a.name,
                }))}
              />
            </Col>
            <Col xs={24} sm={12}>
              <Typography.Text strong>选择存储</Typography.Text>
              <Select
                style={{ width: "100%", marginTop: 4 }}
                value={formData.storage_id || undefined}
                placeholder="请选择存储"
                disabled={!!editingId}
                onChange={(val) => {
                  const storage = storageList?.find((s) => s.id === val);
                  const updates: Partial<PolicyInput> = { storage_id: val };
                  if (
                    !editingId &&
                    !cleanRcloneArgs(formData.rclone_args) &&
                    storage
                  ) {
                    const defs = defaultRcloneArgs(storage.rclone_type);
                    updates.rclone_args = defs;
                    if (Object.keys(defs).length > 0) setAdvancedOpen(true);
                  }
                  setFormData({ ...formData, ...updates });
                }}
                options={storageList?.map((s) => ({
                  value: s.id,
                  label: s.name,
                }))}
              />
            </Col>
          </Row>

          <div>
            <Typography.Text strong>仓库子路径</Typography.Text>
            <Input
              addonBefore="vaultfleet/"
              style={{ marginTop: 4 }}
              value={formData.repo_path}
              onChange={(e) =>
                setFormData({ ...formData, repo_path: e.target.value })
              }
              placeholder={selectedAgent?.name || "my-server"}
              disabled={!!editingId}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              备份仓库的唯一标识。更换节点后使用相同路径即可访问原有备份数据。
            </Typography.Text>
          </div>

          {!editingId && (
            <div>
              <Typography.Text strong>Restic 密码（可选）</Typography.Text>
              <Input.Password
                style={{ marginTop: 4 }}
                value={formData.restic_password}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    restic_password: e.target.value,
                  })
                }
                placeholder="留空则不加密"
                disabled={formData.backup_mode === "archive"}
              />
              {formData.backup_mode === "archive" && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  压缩包备份直接生成归档文件，不使用 restic 仓库密码。
                </Typography.Text>
              )}
            </div>
          )}

          <div
            style={{
              border: "1px solid #f0f0f0",
              borderRadius: 6,
              padding: 12,
            }}
          >
            <Typography.Text strong>备份模式</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
              可选择标准快照仓库备份，或直接生成可下载压缩包。
            </Typography.Paragraph>
            <Row gutter={[8, 8]}>
              <Col xs={24} sm={12}>
                <button
                  type="button"
                  onClick={() =>
                    setFormData({ ...formData, backup_mode: "snapshot" })
                  }
                  style={{
                    width: "100%",
                    textAlign: "left",
                    padding: 10,
                    borderRadius: 6,
                    border: `1px solid ${
                      formData.backup_mode === "snapshot"
                        ? "#1f4f8f"
                        : "#f0f0f0"
                    }`,
                    background:
                      formData.backup_mode === "snapshot"
                        ? "rgba(22,104,220,0.05)"
                        : "transparent",
                    cursor: "pointer",
                  }}
                >
                  <div style={{ fontSize: 13, fontWeight: 500 }}>快照仓库</div>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    适合长期增量备份与恢复浏览。
                  </Typography.Text>
                </button>
              </Col>
              <Col xs={24} sm={12}>
                <button
                  type="button"
                  onClick={() =>
                    setFormData({ ...formData, backup_mode: "archive" })
                  }
                  style={{
                    width: "100%",
                    textAlign: "left",
                    padding: 10,
                    borderRadius: 6,
                    border: `1px solid ${
                      formData.backup_mode === "archive"
                        ? "#1f4f8f"
                        : "#f0f0f0"
                    }`,
                    background:
                      formData.backup_mode === "archive"
                        ? "rgba(22,104,220,0.05)"
                        : "transparent",
                    cursor: "pointer",
                  }}
                >
                  <div style={{ fontSize: 13, fontWeight: 500 }}>压缩包归档</div>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    每次备份生成一个可直接下载的压缩文件。
                  </Typography.Text>
                </button>
              </Col>
            </Row>
            {formData.backup_mode === "archive" && (
              <div style={{ marginTop: 8 }}>
                <Typography.Text strong>压缩格式</Typography.Text>
                <Select
                  style={{ width: "100%", marginTop: 4 }}
                  value={formData.archive_format || "tar.gz"}
                  onChange={(val) =>
                    setFormData({
                      ...formData,
                      archive_format: val as "tar.gz" | "zip",
                    })
                  }
                  options={[
                    { value: "tar.gz", label: "tar.gz" },
                    { value: "zip", label: "zip" },
                  ]}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  生成后的压缩包会出现在备份历史中，可直接下载。
                </Typography.Text>
              </div>
            )}
	          </div>

          <div
            style={{
              borderTop: "1px solid #f0f0f0",
              paddingTop: 12,
            }}
          >
            <Checkbox
              checked={!!formData.verification?.enabled}
              disabled={formData.backup_mode === "archive"}
              onChange={(e) =>
                setFormData({
                  ...formData,
                  verification: {
                    ...(formData.verification ?? {}),
                    enabled: e.target.checked,
                    schedule: formData.verification?.schedule || "0 4 * * *",
                    sample_count: formData.verification?.sample_count ?? 10,
                    timeout_minutes: formData.verification?.timeout_minutes ?? 60,
                  },
                })
              }
            >
              <Typography.Text strong>启用可恢复性验证</Typography.Text>
            </Checkbox>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, margin: "4px 0 8px" }}>
              定期对最新 restic 快照执行 check、ls 和抽样检查。
            </Typography.Paragraph>
            {formData.verification?.enabled && formData.backup_mode === "snapshot" && (
              <Row gutter={[8, 8]}>
                <Col xs={24} sm={12}>
                  <label htmlFor="policy-verification-schedule" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>验证调度</label>
                  <Input
                    id="policy-verification-schedule"
                    value={formData.verification.schedule}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        verification: {
                          ...(formData.verification ?? { enabled: true }),
                          schedule: e.target.value,
                        },
                      })
                    }
                    placeholder="0 4 * * *"
                  />
                </Col>
                <Col xs={12} sm={6}>
                  <label htmlFor="policy-verification-samples" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>抽样数量</label>
                  <InputNumber
                    id="policy-verification-samples"
                    style={{ width: "100%" }}
                    min={0}
                    value={formData.verification.sample_count ?? 10}
                    onChange={(v) =>
                      setFormData({
                        ...formData,
                        verification: {
                          ...(formData.verification ?? { enabled: true }),
                          sample_count: (v as number) ?? 0,
                        },
                      })
                    }
                  />
                </Col>
                <Col xs={12} sm={6}>
                  <label htmlFor="policy-verification-timeout" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>超时分钟</label>
                  <InputNumber
                    id="policy-verification-timeout"
                    style={{ width: "100%" }}
                    min={1}
                    value={formData.verification.timeout_minutes ?? 60}
                    onChange={(v) =>
                      setFormData({
                        ...formData,
                        verification: {
                          ...(formData.verification ?? { enabled: true }),
                          timeout_minutes: (v as number) ?? 60,
                        },
                      })
                    }
                  />
                </Col>
                <Col span={24}>
                  <Checkbox
                    checked={!!formData.verification.sample_restore_enabled}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        verification: {
                          ...(formData.verification ?? { enabled: true }),
                          sample_restore_enabled: e.target.checked,
                        },
                      })
                    }
                  >
                    执行小文件临时恢复测试
                  </Checkbox>
                </Col>
              </Row>
            )}
          </div>

          <div>
            <label htmlFor="policy-backup-dirs" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>备份目录</label>
            <Input.TextArea
              id="policy-backup-dirs"
              value={formData.backup_dirs.join("\n")}
              onChange={(e) =>
                setFormData({
                  ...formData,
                  backup_dirs: e.target.value.split("\n").filter(Boolean),
                })
              }
              placeholder="每行一个路径，如: /etc"
              rows={3}
            />
            {formData.agent_id && (
              <div style={{ marginTop: 8 }}>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  通过文件浏览器添加：
                </Typography.Text>
                {isAgentOnline ? (
                  <DirectoryBrowser
                    agentId={formData.agent_id}
                    selectedPaths={formData.backup_dirs}
                    onSelect={(path) => {
                      if (!formData.backup_dirs.includes(path)) {
                        setFormData({
                          ...formData,
                          backup_dirs: [...formData.backup_dirs, path],
                        });
                      }
                    }}
                    onDeselect={(path) =>
                      setFormData({
                        ...formData,
                        backup_dirs: formData.backup_dirs.filter(
                          (d) => d !== path
                        ),
                      })
                    }
                  />
                ) : (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    节点离线，无法使用文件浏览器。
                  </Typography.Text>
                )}
              </div>
            )}
          </div>

          {formData.agent_id && (
            <div
              style={{
                border: "1px solid #f0f0f0",
                borderRadius: 6,
                padding: 12,
              }}
            >
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "center",
                }}
              >
                <Space>
                  <DatabaseOutlined />
                  <div>
                    <div style={{ fontSize: 13, fontWeight: 500 }}>
                      Docker 容器
                    </div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      已选择 {selectedDockerSources.length} 个容器
                    </Typography.Text>
                  </div>
                </Space>
                <Button
                  size="small"
                  icon={<ReloadOutlined spin={dockerDiscoveryQuery.isFetching} />}
                  onClick={() => dockerDiscoveryQuery.refetch()}
                  disabled={
                    !isAgentOnline ||
                    !dockerCapable ||
                    dockerDiscoveryQuery.isFetching
                  }
                >
                  刷新
                </Button>
              </div>

              {!isAgentOnline && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  节点离线，无法发现 Docker 容器。
                </Typography.Text>
              )}
              {isAgentOnline && !dockerCapable && (
                <div style={{ fontSize: 12, color: "rgba(0,0,0,0.45)" }}>
                  当前 Agent 未上报 Docker 备份能力。
                </div>
              )}
              {isAgentOnline &&
                dockerCapable &&
                (dockerDiscoveryQuery.error || dockerDiscoveryQuery.data?.error) && (
                  <Alert
                    type="error"
                    showIcon
                    style={{ marginTop: 8 }}
                    message={
                      (dockerDiscoveryQuery.error as Error)?.message ||
                      dockerDiscoveryQuery.data?.error
                    }
                  />
                )}
              {isAgentOnline &&
                dockerCapable &&
                dockerDiscoveryQuery.data?.available && (
                  <div style={{ maxHeight: 300, overflowY: "auto", marginTop: 8 }}>
                    {dockerDiscoveryQuery.data.containers.length === 0 ? (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        未发现 Docker 容器。
                      </Typography.Text>
                    ) : (
                      dockerDiscoveryQuery.data.containers.map((c) => {
                        const key = containerKey(c);
                        const checked = selectedDockerKeys.has(key);
                        const compose = c.compose;
                        return (
                          <label
                            key={c.id}
                            style={{
                              display: "flex",
                              gap: 12,
                              alignItems: "flex-start",
                              padding: 10,
                              marginBottom: 6,
                              border: "1px solid #f0f0f0",
                              borderRadius: 6,
                              opacity: c.selectable ? 1 : 0.5,
                              cursor: c.selectable ? "pointer" : "default",
                            }}
                          >
                            <Checkbox
                              disabled={!c.selectable}
                              checked={checked}
                              onChange={(e) => {
                                const val = e.target.checked;
                                const newDockerSources = selectedDockerSources.filter(
                                  (s) => {
                                    const d = s.docker_container;
                                    return (
                                      d && dockerSourceKey(d) !== key
                                    );
                                  }
                                );
                                if (val)
                                  newDockerSources.push(sourceFromContainer(c));
                                setFormData({
                                  ...formData,
                                  backup_sources: [
                                    ...newDockerSources,
                                    ...selectedDatabaseSources,
                                  ],
                                });
                              }}
                            />
                            <div style={{ flex: 1, minWidth: 0 }}>
                              <div
                                style={{
                                  display: "flex",
                                  flexWrap: "wrap",
                                  gap: 6,
                                  alignItems: "center",
                                }}
                              >
                                <Typography.Text strong style={{ fontSize: 13 }}>
                                  {c.names?.[0] || c.id.slice(0, 12)}
                                </Typography.Text>
                                <Tag>{c.state}</Tag>
                              </div>
                              <span style={{ fontSize: 12, color: "rgba(0,0,0,0.45)" }}>
                                {c.image}
                              </span>
                              {(compose?.project || compose?.service) && (
                                <Typography.Text
                                  type="secondary"
                                  style={{ fontSize: 12, display: "block" }}
                                >
                                  {compose.project || "-"} / {compose.service || "-"}
                                </Typography.Text>
                              )}
                              {(c.warnings ?? []).length > 0 && (
                                <div style={{ color: "#fa8c16", fontSize: 11, marginTop: 4 }}>
                                  {c.warnings?.join("；")}
                                </div>
                              )}
                            </div>
                          </label>
                        );
                      })
                    )}
                  </div>
                )}
            </div>
          )}

          <div
            style={{
              border: "1px solid #f0f0f0",
              borderRadius: 6,
              padding: 12,
            }}
          >
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                gap: 8,
                alignItems: "center",
              }}
            >
              <div>
                <Typography.Text strong>数据库备份源</Typography.Text>
                <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
                  支持 PostgreSQL / MySQL 逻辑 dump，生成的 SQL 文件会进入快照或压缩包；不会自动恢复数据库。
                </Typography.Paragraph>
              </div>
              <Button
                icon={<PlusOutlined />}
                disabled={!databaseCapable}
                onClick={() => setDatabaseSources([...selectedDatabaseSources, defaultDatabaseSource()])}
              >
                添加数据库
              </Button>
            </div>
            {!databaseCapable && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                当前 Agent 未上报数据库备份能力。
              </Typography.Text>
            )}
            {selectedDatabaseSources.length > 0 && (
              <div style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 12 }}>
                {selectedDatabaseSources.map((source, index) => {
                  const db = source.database;
                  if (!db) return null;
                  const idPrefix = `policy-db-${index}`;
                  return (
                    <div
                      key={index}
                      style={{ border: "1px solid #f0f0f0", borderRadius: 6, padding: 12 }}
                    >
                      <div style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
                        <Typography.Text strong>{databaseSourceLabel(db)}</Typography.Text>
                        <Button
                          danger
                          size="small"
                          icon={<DeleteOutlined />}
                          onClick={() =>
                            setDatabaseSources(
                              selectedDatabaseSources.filter((_, i) => i !== index),
                            )
                          }
                        />
                      </div>
                      <Row gutter={[8, 8]} style={{ marginTop: 8 }}>
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-engine`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>数据库类型</label>
                          <Select
                            id={`${idPrefix}-engine`}
                            style={{ width: "100%" }}
                            value={db.engine}
                            onChange={(value) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                engine: value,
                                port: value === "mysql" ? 3306 : 5432,
                              }))
                            }
                            options={[
                              { value: "postgresql", label: "PostgreSQL" },
                              { value: "mysql", label: "MySQL" },
                            ]}
                          />
                        </Col>
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-mode`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>执行位置</label>
                          <Select
                            id={`${idPrefix}-mode`}
                            style={{ width: "100%" }}
                            value={db.execution_mode}
                            onChange={(value) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                execution_mode: value,
                              }))
                            }
                            options={[
                              { value: "host", label: "Agent 主机" },
                              { value: "docker", label: "Docker 容器" },
                            ]}
                          />
                        </Col>
                        {db.execution_mode === "docker" ? (
                          <Col xs={24}>
                            <label htmlFor={`${idPrefix}-container`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>数据库容器</label>
                            <Select
                              id={`${idPrefix}-container`}
                              style={{ width: "100%" }}
                              value={db.docker_container ? dockerSourceKey(db.docker_container) : undefined}
                              placeholder={dockerCapable ? "选择容器" : "当前 Agent 不支持 Docker"}
                              disabled={!dockerCapable}
                              onChange={(value) => {
                                const container = dockerDiscoveryQuery.data?.containers.find(
                                  (item) => containerKey(item) === value,
                                );
                                if (!container) return;
                                updateDatabaseSource(index, (current) => ({
                                  ...current,
                                  docker_container: sourceFromContainer(container).docker_container,
                                }));
                              }}
                              options={(dockerDiscoveryQuery.data?.containers ?? []).map((item) => ({
                                value: containerKey(item),
                                label: `${item.names?.[0] || item.id.slice(0, 12)} (${item.image})`,
                                disabled: !item.selectable,
                              }))}
                            />
                          </Col>
                        ) : (
                          <>
                            <Col xs={24} sm={16}>
                              <label htmlFor={`${idPrefix}-host`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>主机</label>
                              <Input
                                id={`${idPrefix}-host`}
                                value={db.host}
                                onChange={(e) =>
                                  updateDatabaseSource(index, (current) => ({
                                    ...current,
                                    host: e.target.value,
                                  }))
                                }
                              />
                            </Col>
                            <Col xs={24} sm={8}>
                              <label htmlFor={`${idPrefix}-port`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>端口</label>
                              <InputNumber
                                id={`${idPrefix}-port`}
                                style={{ width: "100%" }}
                                min={0}
                                max={65535}
                                value={db.port}
                                onChange={(value) =>
                                  updateDatabaseSource(index, (current) => ({
                                    ...current,
                                    port: (value as number) ?? undefined,
                                  }))
                                }
                              />
                            </Col>
                          </>
                        )}
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-username`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>数据库用户</label>
                          <Input
                            id={`${idPrefix}-username`}
                            value={db.username}
                            onChange={(e) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                username: e.target.value,
                              }))
                            }
                          />
                        </Col>
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-password`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>数据库密码</label>
                          <Input.Password
                            id={`${idPrefix}-password`}
                            placeholder={db.password_set ? "已保存，留空则不变" : "可留空"}
                            value={db.password ?? ""}
                            onChange={(e) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                password: e.target.value,
                                password_set: current.password_set || e.target.value.length > 0,
                              }))
                            }
                          />
                        </Col>
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-database`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>数据库名</label>
                          <Input
                            id={`${idPrefix}-database`}
                            disabled={!!db.all_databases}
                            value={db.database}
                            onChange={(e) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                database: e.target.value,
                              }))
                            }
                          />
                        </Col>
                        <Col xs={24} sm={12}>
                          <label htmlFor={`${idPrefix}-output`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>输出文件名</label>
                          <Input
                            id={`${idPrefix}-output`}
                            placeholder="自动生成"
                            value={db.output_name}
                            onChange={(e) =>
                              updateDatabaseSource(index, (current) => ({
                                ...current,
                                output_name: e.target.value,
                              }))
                            }
                          />
                        </Col>
                        <Col xs={24}>
                          <Space wrap>
                            <Checkbox
                              checked={!!db.all_databases}
                              onChange={(e) =>
                                updateDatabaseSource(index, (current) => ({
                                  ...current,
                                  all_databases: e.target.checked,
                                  database: e.target.checked ? "" : current.database,
                                }))
                              }
                            >
                              备份全部数据库
                            </Checkbox>
                            <Checkbox
                              checked={db.compress !== false}
                              onChange={(e) =>
                                updateDatabaseSource(index, (current) => ({
                                  ...current,
                                  compress: e.target.checked,
                                }))
                              }
                            >
                              gzip 压缩 dump
                            </Checkbox>
                          </Space>
                        </Col>
                      </Row>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          <Alert
            type="warning"
            showIcon
            icon={<AlertOutlined />}
            message="Docker 工作负载建议"
            description={
              <div style={{ fontSize: 12 }}>
                <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                  Docker 场景建议备份容器挂载数据、bind mount
                  路径、`docker-compose.yml`、`.env`
                  等编排文件；此功能不备份镜像层，也不会自动重建容器。
                </Typography.Paragraph>
                <Row gutter={[8, 8]}>
                  <Col xs={24} sm={12}>
                    <div style={{ padding: 8, border: "1px solid #f0f0f0", borderRadius: 4, background: "#fafafa" }}>
                      <Typography.Text strong style={{ fontSize: 12 }}>
                        推荐路径示例
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        /srv/app/data
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        /srv/app/docker-compose.yml
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        /srv/app/.env
                      </Typography.Text>
                    </div>
                  </Col>
                  <Col xs={24} sm={12}>
                    <div style={{ padding: 8, border: "1px solid #f0f0f0", borderRadius: 4, background: "#fafafa" }}>
                      <Typography.Text strong style={{ fontSize: 12 }}>
                        一致性示例
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        docker exec db pg_dump ...
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        docker compose stop app
                      </Typography.Text>
                      <Typography.Text code style={{ fontSize: 11, display: "block" }}>
                        docker compose start app
                      </Typography.Text>
                    </div>
                  </Col>
                </Row>
              </div>
            }
          />

          <div
            style={{
              border: "1px solid #f0f0f0",
              borderRadius: 6,
              padding: 12,
            }}
          >
            <Typography.Text strong>备份钩子（可选）</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
              备份前后可执行主机命令，用于 Docker 数据导出、短暂停服务或恢复运行。命令执行失败会导致任务失败。
            </Typography.Paragraph>
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              <div>
                <label htmlFor="policy-pre-hook-command" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>备份前命令</label>
                <Input.TextArea
                  id="policy-pre-hook-command"
                  value={formData.pre_backup_hook?.command ?? ""}
                  onChange={(e) =>
                    setFormData({
                      ...formData,
                      pre_backup_hook: {
                        command: e.target.value,
                        timeout_seconds:
                          formData.pre_backup_hook?.timeout_seconds ?? 300,
                      },
                    })
                  }
                  placeholder="如: docker exec db pg_dump -U app app > /srv/app/backup/db.sql"
                  rows={2}
                />
              </div>
              <div>
                <label htmlFor="policy-pre-hook-timeout" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>备份前命令超时（秒）</label>
                <InputNumber
                  id="policy-pre-hook-timeout"
                  style={{ width: "100%" }}
                  min={0}
                  max={3600}
                  value={formData.pre_backup_hook?.timeout_seconds ?? ""}
                  onChange={(v) =>
                    setFormData({
                      ...formData,
                      pre_backup_hook: {
                        command: formData.pre_backup_hook?.command ?? "",
                        timeout_seconds: (v as number) ?? undefined,
                      },
                    })
                  }
                />
              </div>
              <div>
                <label htmlFor="policy-post-hook-command" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>备份后命令</label>
                <Input.TextArea
                  id="policy-post-hook-command"
                  value={formData.post_backup_hook?.command ?? ""}
                  onChange={(e) =>
                    setFormData({
                      ...formData,
                      post_backup_hook: {
                        command: e.target.value,
                        timeout_seconds:
                          formData.post_backup_hook?.timeout_seconds ?? 300,
                      },
                    })
                  }
                  placeholder="如: docker compose start app"
                  rows={2}
                />
              </div>
              <div>
                <label htmlFor="policy-post-hook-timeout" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>备份后命令超时（秒）</label>
                <InputNumber
                  id="policy-post-hook-timeout"
                  style={{ width: "100%" }}
                  min={0}
                  max={3600}
                  value={formData.post_backup_hook?.timeout_seconds ?? ""}
                  onChange={(v) =>
                    setFormData({
                      ...formData,
                      post_backup_hook: {
                        command: formData.post_backup_hook?.command ?? "",
                        timeout_seconds: (v as number) ?? undefined,
                      },
                    })
                  }
                />
              </div>
            </div>
          </div>

          <div>
            <Typography.Text strong>Cron 调度</Typography.Text>
            <Input
              style={{ marginTop: 4 }}
              value={formData.schedule}
              onChange={(e) =>
                setFormData({ ...formData, schedule: e.target.value })
              }
              placeholder="0 2 * * *"
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {describeCron(formData.schedule)} — 标准 Cron 表达式（分 时 日 月 周）。
            </Typography.Text>
          </div>

          <div>
            <label htmlFor="policy-timeout-hours" style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>任务超时（小时）</label>
            <InputNumber
              id="policy-timeout-hours"
              style={{ width: "100%" }}
              min={1}
              max={72}
              value={formData.timeout_hours ?? 6}
              onChange={(v) =>
                setFormData({
                  ...formData,
                  timeout_hours: (v as number) ?? 6,
                })
              }
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              备份任务超过此时间未完成将自动标记为超时，默认 6 小时
            </Typography.Text>
          </div>

          <div
            style={{
              borderTop: "1px solid #f0f0f0",
              paddingTop: 12,
            }}
          >
            <Typography.Text strong>保留策略 (Retention)</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
              每次备份后自动清理旧快照，释放存储空间。
            </Typography.Paragraph>
            <Row gutter={[8, 8]}>
              {Object.entries(RETENTION_PRESETS).map(([key, preset]) => (
                <Col xs={24} sm={12} key={key}>
                  <button
                    type="button"
                    onClick={() => {
                      setRetentionPreset(key);
                      if (key !== "custom") {
                        setFormData({
                          ...formData,
                          retention: { ...preset.values },
                        });
                      }
                    }}
                    style={{
                      width: "100%",
                      textAlign: "left",
                      padding: 10,
                      borderRadius: 6,
                      border: `1px solid ${
                        retentionPreset === key ? "#1f4f8f" : "#f0f0f0"
                      }`,
                      background:
                        retentionPreset === key
                          ? "rgba(22,104,220,0.05)"
                          : "transparent",
                      cursor: "pointer",
                    }}
                  >
                    <div style={{ fontSize: 13, fontWeight: 500 }}>{preset.label}</div>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      {preset.description}
                    </Typography.Text>
                  </button>
                </Col>
              ))}
            </Row>
            {retentionPreset === "custom" && (
              <Row gutter={[8, 8]} style={{ marginTop: 8 }}>
                {[
                  { key: "keep_last", label: "保留最近副本" },
                  { key: "keep_daily", label: "保留每日副本" },
                  { key: "keep_weekly", label: "保留每周副本" },
                  { key: "keep_monthly", label: "保留每月副本" },
                ].map((f) => (
                  <Col xs={24} sm={12} key={f.key}>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {f.label}
                    </Typography.Text>
                    <InputNumber
                      style={{ width: "100%" }}
                      min={0}
                      value={(formData.retention as any)[f.key] ?? 0}
                      onChange={(v) =>
                        setFormData({
                          ...formData,
                          retention: {
                            ...formData.retention,
                            [f.key]: (v as number) ?? 0,
                          },
                        })
                      }
                    />
                  </Col>
                ))}
              </Row>
            )}
            {retentionPreset !== "custom" && (
              <div
                style={{
                  marginTop: 8,
                  padding: "6px 10px",
                  background: "#fafafa",
                  borderRadius: 4,
                  fontSize: 12,
                  color: "rgba(0,0,0,0.45)",
                }}
              >
                最近 {formData.retention.keep_last ?? 0} 个 · 每日{" "}
                {formData.retention.keep_daily ?? 0} 份 · 每周{" "}
                {formData.retention.keep_weekly ?? 0} 份 · 每月{" "}
                {formData.retention.keep_monthly ?? 0} 份
              </div>
            )}
          </div>

          <div
            style={{
              borderTop: "1px solid #f0f0f0",
              paddingTop: 12,
            }}
          >
            <button
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                width: "100%",
                background: "transparent",
                border: "none",
                cursor: "pointer",
                padding: 0,
              }}
            >
              <Space>
                <SettingOutlined />
                <Typography.Text strong>高级传输参数</Typography.Text>
              </Space>
              <ReloadOutlined
                spin={false}
                style={{
                  transform: advancedOpen ? "rotate(180deg)" : "none",
                  transition: "transform 0.2s",
                }}
              />
            </button>
            {advancedOpen && (
              <Row gutter={[12, 12]} style={{ marginTop: 8 }}>
                {RCLONE_ARG_FIELDS.map((f) => (
                  <Col xs={24} sm={12} key={f.key}>
                    <label htmlFor={`rclone-${f.key}`} style={{ display: "block", fontWeight: 500, marginBottom: 4 }}>{f.label}</label>
                    <Input
                      id={`rclone-${f.key}`}
                      value={formData.rclone_args?.[f.key] ?? ""}
                      onChange={(e) =>
                        setFormData({
                          ...formData,
                          rclone_args: {
                            ...(formData.rclone_args ?? {}),
                            [f.key]: e.target.value,
                          },
                        })
                      }
                      placeholder={`默认 ${f.defaultValue} / WebDAV ${f.webdavValue}`}
                    />
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      {f.description}
                    </Typography.Text>
                  </Col>
                ))}
                <Col xs={24}>
                  <Checkbox
                    checked={rcloneBooleanArgEnabled(formData.rclone_args, "local-no-check-updated")}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        rclone_args: setRcloneBooleanArg(
                          formData.rclone_args,
                          "local-no-check-updated",
                          e.target.checked,
                        ),
                      })
                    }
                  >
                    允许备份正在变化的本地文件
                  </Checkbox>
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      仅建议用于日志文件；开启后 rclone 不再因为上传期间文件大小变化而失败。
                    </Typography.Text>
                  </div>
                </Col>
              </Row>
            )}
          </div>
          {/* 隐藏的 submit 用于支持 Enter 键 */}
          <button type="submit" style={{ display: "none" }} />
        </form>
      </Drawer>

      <Drawer
        title="批量下发策略"
        open={!!bulkSourcePolicy}
        onClose={closeBulkAssign}
        width="min(100vw, 520px)"
        destroyOnClose
        footer={
          <Button
            type="primary"
            block
            loading={bulkAssignMutation.isPending}
            onClick={submitBulkAssign}
          >
            执行批量下发
          </Button>
        }
      >
        <Space direction="vertical" size={16} style={{ width: "100%" }}>
          {bulkSourcePolicy && (
            <Alert
              type="info"
              showIcon
              message="从现有策略克隆"
              description={`源节点：${
                agents?.find((a) => a.id === bulkSourcePolicy.agent_id)?.name ||
                bulkSourcePolicy.agent_id
              }，调度：${bulkSourcePolicy.schedule}`}
            />
          )}

          <div>
            <Typography.Text strong>目标节点</Typography.Text>
            <Select
              mode="multiple"
              allowClear
              value={bulkAgentIds}
              onChange={setBulkAgentIds}
              placeholder="选择一个或多个节点"
              style={{ width: "100%", marginTop: 4 }}
              options={(agents ?? [])
                .filter((agent) => agent.id !== bulkSourcePolicy?.agent_id)
                .map((agent) => ({
                  value: agent.id,
                  label: agent.name,
                }))}
            />
          </div>

          <div>
            <Typography.Text strong>目标标签</Typography.Text>
            <Select
              mode="tags"
              allowClear
              value={bulkTags}
              onChange={setBulkTags}
              placeholder="匹配同时拥有这些标签的节点"
              style={{ width: "100%", marginTop: 4 }}
              options={(knownTags ?? []).map((tag) => ({
                value: tag,
                label: tag,
              }))}
            />
          </div>

          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            每个目标节点会创建一条独立策略，仓库路径自动使用 vaultfleet/目标节点 ID。
          </Typography.Text>

          {bulkResult && (
            <div>
              <Alert
                type={bulkResult.failed_count ? "warning" : "success"}
                showIcon
                message={`成功 ${bulkResult.created_count}，失败 ${bulkResult.failed_count}`}
                style={{ marginBottom: 12 }}
              />
              <Space direction="vertical" size={8} style={{ width: "100%" }}>
                {bulkResult.results.map((result, index) => (
                  <div
                    key={`${result.agent_id || index}-${result.policy_id || result.error}`}
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      gap: 12,
                      padding: "8px 10px",
                      border: "1px solid #f0f0f0",
                      borderRadius: 6,
                    }}
                  >
                    <Typography.Text>
                      {result.agent_name || result.agent_id || "未知节点"}
                    </Typography.Text>
                    {result.ok ? (
                      <Tag color="green">已创建</Tag>
                    ) : (
                      <Typography.Text type="danger" style={{ fontSize: 12 }}>
                        {result.error}
                      </Typography.Text>
                    )}
                  </div>
                ))}
              </Space>
            </div>
          )}
        </Space>
      </Drawer>

      <ConfirmDialog
        open={!!confirmBackupAgentId}
        onOpenChange={(open) => !open && setConfirmBackupAgentId(null)}
        title="确认立即备份"
        description={`将对节点 ${
          agents?.find((a) => a.id === confirmBackupAgentId)?.name ||
          confirmBackupAgentId
        } 发起立即备份请求。`}
        confirmText="立即备份"
        variant="default"
        onConfirm={() =>
          confirmBackupAgentId && backupMutation.mutate(confirmBackupAgentId)
        }
        loading={backupMutation.isPending}
      />
    </div>
  );
}
