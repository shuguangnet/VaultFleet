import { useState } from "react";
import {
  Alert,
  Button,
  Card,
  Drawer,
  Dropdown,
  Empty,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  EllipsisOutlined,
  DesktopOutlined,
  LinkOutlined,
  PlusOutlined,
  SearchOutlined,
  CodeOutlined,
  CloudUploadOutlined,
  TagsOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { Link } from "react-router-dom";
import {safeFormatDate} from "@/lib/date";
import {
  createAgent,
  deleteAgent,
  getInstallToken,
  listAgentTags,
  listAgents,
  regenerateAgentToken,
  updateAgentTags,
} from "@/services/agents";
import {
  cancelAgentRollout,
  createAgentRollout,
  listAgentRollouts,
} from "@/services/agent-rollouts";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { InstallCommand } from "@/components/install-command";
import { PageHeader } from "@/components/page-header";
import { ErrorPanel } from "@/components/error-panel";
import { StatusBadge } from "@/components/status-badge";
import { App } from "antd";
import { useAuth } from "@/contexts/auth-context";
import { permissions } from "@/services/identity";
import type { AgentRollout, AgentRolloutItem } from "@/types/agent-rollout";

type Agent = Awaited<ReturnType<typeof listAgents>>[number];

export function NodesPage() {
  const { message } = App.useApp();
  const auth = useAuth();
  const canWriteNodes = auth.hasPermission(permissions.writeNodes);
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [newNodeName, setNewNodeName] = useState("");
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [tagEditorAgent, setTagEditorAgent] = useState<Agent | null>(null);
  const [tagDraft, setTagDraft] = useState<string[]>([]);
  const [enrollToken, setEnrollToken] = useState<string | null>(null);
  const [installCommandAgent, setInstallCommandAgent] = useState<
    { id: string; token: string } | null
  >(null);
  const [rolloutDrawerOpen, setRolloutDrawerOpen] = useState(false);
  const [rolloutVersion, setRolloutVersion] = useState("");
  const [rolloutRepo, setRolloutRepo] = useState("");
  const [rolloutTags, setRolloutTags] = useState<string[]>([]);
  const [rolloutAgentIds, setRolloutAgentIds] = useState<string[]>([]);
  const [rolloutCanaryCount, setRolloutCanaryCount] = useState(1);
  const [rolloutBatchSize, setRolloutBatchSize] = useState(5);

  const {
    data: agents,
    isLoading,
    isFetching,
    error: agentsError,
    refetch: refetchAgents,
  } = useQuery({
    queryKey: ["agents", selectedTags],
    queryFn: () => listAgents(selectedTags),
    refetchInterval: 10000,
  });
  const { data: knownTags } = useQuery({
    queryKey: ["agent-tags"],
    queryFn: listAgentTags,
  });
  const { data: rollouts } = useQuery({
    queryKey: ["agent-upgrade-rollouts"],
    queryFn: listAgentRollouts,
    refetchInterval: (query) =>
      query.state.data?.some((rollout) => rollout.status === "pending" || rollout.status === "running")
        ? 5000
        : false,
  });

  const createMutation = useMutation({
    mutationFn: createAgent,
    onSuccess: (data) => {
      setEnrollToken(data.enroll_token);
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteAgent,
    onSuccess: () => {
      message.success("节点已删除");
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
    onError: (err: any) => message.error(err.message || "删除失败"),
  });

  const regenMutation = useMutation({
    mutationFn: regenerateAgentToken,
    onSuccess: (data) => {
      setEnrollToken(data.enroll_token);
    },
  });

  const updateTagsMutation = useMutation({
    mutationFn: ({ id, tags }: { id: string; tags: string[] }) =>
      updateAgentTags(id, tags),
    onSuccess: () => {
      message.success("节点标签已更新");
      setTagEditorAgent(null);
      setTagDraft([]);
      queryClient.invalidateQueries({ queryKey: ["agents"] });
      queryClient.invalidateQueries({ queryKey: ["agent-tags"] });
    },
    onError: (err: any) => message.error(err.message || "标签更新失败"),
  });
  const createRolloutMutation = useMutation({
    mutationFn: createAgentRollout,
    onSuccess: () => {
      message.success("Agent 批量升级已创建");
      closeRolloutDrawer();
      queryClient.invalidateQueries({ queryKey: ["agent-upgrade-rollouts"] });
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
    onError: (err: any) => message.error(err.message || "创建升级任务失败"),
  });
  const cancelRolloutMutation = useMutation({
    mutationFn: (id: string) => cancelAgentRollout(id, "cancelled from UI"),
    onSuccess: () => {
      message.success("升级任务已取消");
      queryClient.invalidateQueries({ queryKey: ["agent-upgrade-rollouts"] });
    },
    onError: (err: any) => message.error(err.message || "取消升级任务失败"),
  });

  const filtered = agents?.filter((a) =>
    a.name.toLowerCase().includes(search.toLowerCase())
  ) || [];
  const rolloutTargets = resolveRolloutTargets(agents ?? [], rolloutAgentIds, rolloutTags);

  const handleAddNode = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!newNodeName) {
      message.warning("请输入节点名称");
      return;
    }
    createMutation.mutate({ name: newNodeName });
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setEnrollToken(null);
    setNewNodeName("");
    setInstallCommandAgent(null);
    createMutation.reset();
  };
  const openRolloutDrawer = () => {
    setRolloutTags(selectedTags);
    setRolloutDrawerOpen(true);
  };

  const closeRolloutDrawer = () => {
    setRolloutDrawerOpen(false);
    setRolloutVersion("");
    setRolloutRepo("");
    setRolloutTags([]);
    setRolloutAgentIds([]);
    setRolloutCanaryCount(1);
    setRolloutBatchSize(5);
    createRolloutMutation.reset();
  };

  const submitRollout = () => {
    if (rolloutTags.length === 0 && rolloutAgentIds.length === 0) {
      message.warning("请选择目标标签或目标节点");
      return;
    }
    createRolloutMutation.mutate({
      target_version: rolloutVersion || undefined,
      github_repo: rolloutRepo || undefined,
      target_tags: rolloutTags,
      target_agent_ids: rolloutAgentIds,
      canary_count: rolloutCanaryCount,
      batch_size: rolloutBatchSize,
    });
  };

  const openTagEditor = (agent: Agent) => {
    setTagEditorAgent(agent);
    setTagDraft(agent.tags ?? []);
  };

  const handleShowInstallCommand = async (agentId: string) => {
    try {
      const result = await getInstallToken(agentId);
      if (!result.enrolled) {
        setInstallCommandAgent({ id: agentId, token: result.enroll_token });
        setEnrollToken(result.enroll_token);
        setDrawerOpen(true);
      } else {
        // 节点已注册 — 直接重新生成
        regenMutation.mutate(agentId, {
          onSuccess: (data) => {
            setInstallCommandAgent({ id: agentId, token: data.enroll_token });
            setDrawerOpen(true);
          },
        });
      }
    } catch {
      regenMutation.mutate(agentId, {
        onSuccess: (data) => {
          setInstallCommandAgent({ id: agentId, token: data.enroll_token });
          setDrawerOpen(true);
        },
      });
    }
  };

  const columns: ColumnsType<Agent> = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      render: (text, record) => (
        <Link to={`/nodes/${record.id}`}>
          <Space>
            <Typography.Text strong>{text}</Typography.Text>
            <LinkOutlined style={{ color: "var(--vf-text-muted)" }} />
          </Space>
        </Link>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s: string) => <StatusBadge status={s as any} />,
    },
    {
      title: "标签",
      dataIndex: "tags",
      key: "tags",
      responsive: ["md"],
      render: (tags: string[]) =>
        tags?.length ? (
          <Space size={[4, 4]} wrap>
            {tags.map((tag) => (
              <Tag key={tag}>{tag}</Tag>
            ))}
          </Space>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            -
          </Typography.Text>
        ),
    },
    {
      title: "最后在线",
      dataIndex: "last_seen",
      key: "last_seen",
      render: (v: string | null) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd HH:mm:ss", "从未在线")}
        </Typography.Text>
      ),
    },
    {
      title: "系统信息",
      key: "os",
      responsive: ["md"],
      render: (_, record) => (
        <div style={{ display: "flex", flexDirection: "column" }}>
          <Typography.Text style={{ fontSize: 12 }}>
            {record.os} / {record.arch}
          </Typography.Text>
          {record.version && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {record.version}
            </Typography.Text>
          )}
        </div>
      ),
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      responsive: ["lg"],
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      fixed: "right",
      width: 56,
      render: (_, record) => (
        <Dropdown
          menu={{
            items: [
              {
                key: "detail",
                label: (
                  <Link to={`/nodes/${record.id}`} style={{ display: "block" }}>
                    详情
                  </Link>
                ),
              },
              canWriteNodes ? {
                key: "tags",
                icon: <TagsOutlined />,
                label: "编辑标签",
                onClick: () => openTagEditor(record),
              } : null,
              canWriteNodes ? {
                key: "install",
                icon: <CodeOutlined />,
                label: "安装指令",
                onClick: () => handleShowInstallCommand(record.id),
              } : null,
              canWriteNodes ? { type: "divider" as const } : null,
              canWriteNodes ? {
                key: "delete",
                icon: <WarningOutlined style={{ color: "#ef4444" }} />,
                label: (
                  <Popconfirm
                    title="确认删除节点？"
                    description="此操作将永久删除该节点及其所有关联策略。此操作不可撤销。"
                    okText="确认删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => deleteMutation.mutate(record.id)}
                  >
                    <span style={{ color: "#ef4444" }}>删除</span>
                  </Popconfirm>
                ),
              } : null,
            ].filter(Boolean) as any,
          }}
          trigger={["click"]}
          placement="bottomRight"
        >
          <Button type="text" icon={<EllipsisOutlined />} />
        </Dropdown>
      ),
    },
  ];

  return (
    <div className="vf-page">
      <PageHeader
        title="节点管理"
        description="Agent / 状态 / 安装令牌"
        icon={<DesktopOutlined />}
        actions={
          canWriteNodes ? (
            <Space wrap>
              <Button
                icon={<CloudUploadOutlined />}
                onClick={openRolloutDrawer}
              >
                批量升级
              </Button>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => setDrawerOpen(true)}
              >
                添加节点
              </Button>
            </Space>
          ) : null
        }
      />

      <ErrorPanel
        error={agentsError}
        title="无法加载节点"
        onRetry={() => void refetchAgents()}
        retrying={isFetching}
      />

      <div className="vf-toolbar" role="search" aria-label="节点筛选">
        <Input
          className="vf-toolbar-search"
          allowClear
          placeholder="搜索节点名称"
          prefix={<SearchOutlined />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Select
          className="vf-toolbar-filter"
          mode="multiple"
          allowClear
          placeholder="按标签筛选"
          value={selectedTags}
          onChange={setSelectedTags}
          options={(knownTags ?? []).map((tag) => ({ label: tag, value: tag }))}
          suffixIcon={<TagsOutlined />}
        />
        <Typography.Text className="vf-toolbar-count">
          共 {filtered.length} 个节点
        </Typography.Text>
      </div>

      <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
        <Table<Agent>
          columns={columns}
          dataSource={filtered}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 10, showSizeChanger: true }}
          scroll={{ x: 680 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="未找到匹配的节点"
              />
            ),
          }}
        />
      </Card>

      <Card
        className="vf-table-card"
        title="Agent 升级任务"
        styles={{ body: { padding: 0 } }}
      >
        <Table<AgentRollout>
          columns={rolloutColumns(cancelRolloutMutation.mutate, cancelRolloutMutation.isPending)}
          dataSource={rollouts ?? []}
          rowKey="id"
          expandable={{
            expandedRowRender: rolloutExpandedRow,
            rowExpandable: (rollout) => (rollout.items?.length ?? 0) > 0,
          }}
          pagination={{ pageSize: 5 }}
          scroll={{ x: 860 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无升级任务" />
            ),
          }}
        />
      </Card>

      <Drawer
        title={enrollToken ? "安装指令" : "添加新节点"}
        open={drawerOpen}
        onClose={closeDrawer}
        width="min(100vw, 480px)"
        destroyOnHidden
      >
        {enrollToken ? (
          <InstallCommand enrollToken={enrollToken} />
        ) : (
          <form onSubmit={handleAddNode}>
            <Space direction="vertical" size={16} style={{ width: "100%" }}>
              <Typography.Paragraph type="secondary">
                输入节点名称以生成安装 Token。
              </Typography.Paragraph>
              <Input
                placeholder="节点名称 (如: production-db-1)"
                value={newNodeName}
                onChange={(e) => setNewNodeName(e.target.value)}
                autoFocus
              />
              <Button
                type="primary"
                htmlType="submit"
                block
                loading={createMutation.isPending}
              >
                生成安装 Token
              </Button>
            </Space>
          </form>
        )}
      </Drawer>

      <Drawer
        title="Agent 批量升级"
        open={rolloutDrawerOpen}
        onClose={closeRolloutDrawer}
        width="min(100vw, 720px)"
        destroyOnHidden
        footer={
          <Button
            type="primary"
            block
            size="large"
            loading={createRolloutMutation.isPending}
            onClick={submitRollout}
          >
            创建升级任务
          </Button>
        }
      >
        <Space direction="vertical" size={14} style={{ width: "100%" }}>
          <Alert
            type="info"
            showIcon
            title="先升级 canary 节点，确认重新上线并上报目标版本后，再按批次继续；任一节点失败会停止后续升级。"
          />
          <div>
            <Typography.Text strong>目标版本</Typography.Text>
            <Input
              value={rolloutVersion}
              onChange={(e) => setRolloutVersion(e.target.value)}
              placeholder="留空使用 Master 当前版本或 latest"
              style={{ marginTop: 4 }}
            />
          </div>
          <div>
            <Typography.Text strong>GitHub 仓库</Typography.Text>
            <Input
              value={rolloutRepo}
              onChange={(e) => setRolloutRepo(e.target.value)}
              placeholder="留空使用系统默认仓库"
              style={{ marginTop: 4 }}
            />
          </div>
          <div>
            <Typography.Text strong>目标标签</Typography.Text>
            <Select
              aria-label="升级目标标签"
              mode="multiple"
              allowClear
              value={rolloutTags}
              onChange={setRolloutTags}
              options={(knownTags ?? []).map((tag) => ({ label: tag, value: tag }))}
              style={{ width: "100%", marginTop: 4 }}
              placeholder="选择标签"
            />
          </div>
          <div>
            <Typography.Text strong>目标节点</Typography.Text>
            <Select
              aria-label="升级目标节点"
              mode="multiple"
              allowClear
              value={rolloutAgentIds}
              onChange={setRolloutAgentIds}
              options={(agents ?? []).map((agent) => ({
                label: `${agent.name} (${agent.version || "未知版本"})`,
                value: agent.id,
              }))}
              style={{ width: "100%", marginTop: 4 }}
              placeholder="可选：手动指定节点"
            />
          </div>
          <Space wrap>
            <div>
              <Typography.Text strong>Canary 数量</Typography.Text>
              <InputNumber
                aria-label="Canary 数量"
                min={1}
                max={10}
                value={rolloutCanaryCount}
                onChange={(value) => setRolloutCanaryCount(value ?? 1)}
                style={{ width: 140, marginTop: 4, display: "block" }}
              />
            </div>
            <div>
              <Typography.Text strong>批大小</Typography.Text>
              <InputNumber
                aria-label="批大小"
                min={1}
                max={50}
                value={rolloutBatchSize}
                onChange={(value) => setRolloutBatchSize(value ?? 5)}
                style={{ width: 140, marginTop: 4, display: "block" }}
              />
            </div>
          </Space>
          <div>
            <Typography.Text strong>目标预览</Typography.Text>
            <Table<Agent>
              dataSource={rolloutTargets}
              rowKey="id"
              pagination={false}
              size="small"
              style={{ marginTop: 8 }}
              columns={[
                { title: "节点", dataIndex: "name", key: "name" },
                {
                  title: "状态",
                  dataIndex: "status",
                  key: "status",
                  render: (status) => <StatusBadge status={status as any} />,
                },
                {
                  title: "版本",
                  dataIndex: "version",
                  key: "version",
                  render: (version) => version || "未知",
                },
                {
                  title: "架构",
                  dataIndex: "arch",
                  key: "arch",
                  render: (arch) => arch || "未知",
                },
              ]}
              locale={{
                emptyText: (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请选择目标标签或节点" />
                ),
              }}
            />
          </div>
        </Space>
      </Drawer>

      <Drawer
        title={tagEditorAgent ? `编辑标签：${tagEditorAgent.name}` : "编辑标签"}
        open={!!tagEditorAgent}
        onClose={() => {
          setTagEditorAgent(null);
          setTagDraft([]);
        }}
        width="min(100vw, 420px)"
        destroyOnHidden
        footer={
          <Button
            type="primary"
            block
            loading={updateTagsMutation.isPending}
            onClick={() =>
              tagEditorAgent &&
              updateTagsMutation.mutate({
                id: tagEditorAgent.id,
                tags: tagDraft,
              })
            }
          >
            保存标签
          </Button>
        }
      >
        <Space direction="vertical" size={12} style={{ width: "100%" }}>
          <Typography.Paragraph type="secondary">
            使用环境、区域、业务或 OpenStack 可用区等标签组织节点。
          </Typography.Paragraph>
          <Select
            mode="tags"
            value={tagDraft}
            onChange={setTagDraft}
            placeholder="例如 prod、web、openstack:az1"
            options={(knownTags ?? []).map((tag) => ({ label: tag, value: tag }))}
            style={{ width: "100%" }}
          />
        </Space>
      </Drawer>
    </div>
  );
}

function resolveRolloutTargets(agents: Agent[], explicitIDs: string[], tags: string[]) {
  const explicit = new Set(explicitIDs);
  const requiredTags = new Set(tags);
  const result: Agent[] = [];
  const seen = new Set<string>();
  for (const agent of agents) {
    const agentTags = new Set(agent.tags ?? []);
    const tagMatched = requiredTags.size > 0 && [...requiredTags].every((tag) => agentTags.has(tag));
    if ((explicit.has(agent.id) || tagMatched) && !seen.has(agent.id)) {
      result.push(agent);
      seen.add(agent.id);
    }
  }
  return result;
}

function rolloutStatusTag(status: string) {
  const color =
    status === "succeeded" ? "green" :
    status === "failed" ? "red" :
    status === "cancelled" ? "default" :
    status === "running" ? "blue" :
    "gold";
  const label: Record<string, string> = {
    pending: "等待中",
    running: "运行中",
    succeeded: "成功",
    failed: "失败",
    cancelled: "已取消",
  };
  return <Tag color={color}>{label[status] || status}</Tag>;
}

function rolloutItemStatusTag(status: string) {
  const color =
    status === "success" ? "green" :
    status === "failed" ? "red" :
    status === "running" ? "blue" :
    status === "skipped" ? "default" :
    "gold";
  const label: Record<string, string> = {
    pending: "等待",
    running: "升级中",
    success: "成功",
    failed: "失败",
    skipped: "跳过",
  };
  return <Tag color={color}>{label[status] || status}</Tag>;
}

function rolloutExpandedRow(rollout: AgentRollout) {
  return (
    <Table<AgentRolloutItem>
      rowKey="id"
      dataSource={rollout.items ?? []}
      pagination={false}
      size="small"
      columns={[
        {
          title: "节点",
          key: "agent",
          render: (_, item) => item.agent_name || item.agent_id,
        },
        {
          title: "阶段",
          key: "phase",
          render: (_, item) => item.phase === "canary" ? "Canary" : `批次 ${item.batch_index + 1}`,
        },
        {
          title: "状态",
          dataIndex: "status",
          key: "status",
          render: rolloutItemStatusTag,
        },
        {
          title: "当前版本",
          dataIndex: "current_version",
          key: "current_version",
          render: (value) => value || "未知",
        },
        {
          title: "目标版本",
          dataIndex: "target_version",
          key: "target_version",
        },
        {
          title: "最后上报",
          dataIndex: "last_seen_version",
          key: "last_seen_version",
          render: (value) => value || "-",
        },
        {
          title: "原因",
          key: "reason",
          render: (_, item) => item.error || item.skip_reason || "-",
        },
      ]}
    />
  );
}

function rolloutColumns(
  onCancel: (id: string) => void,
  cancelling: boolean,
): ColumnsType<AgentRollout> {
  return [
    {
      title: "目标版本",
      dataIndex: "target_version",
      key: "target_version",
      render: (version) => <Typography.Text code>{version}</Typography.Text>,
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: rolloutStatusTag,
    },
    {
      title: "目标",
      key: "targets",
      render: (_, rollout) => (
        <Space size={[4, 4]} wrap>
          {(rollout.target_tags ?? []).map((tag) => <Tag key={tag}>{tag}</Tag>)}
          {(rollout.target_agent_ids ?? []).length > 0 && (
            <Tag>{rollout.target_agent_ids.length} 个节点</Tag>
          )}
        </Space>
      ),
    },
    {
      title: "进度",
      key: "counts",
      render: (_, rollout) => {
        const counts = rollout.counts ?? {};
        return (
          <Space size={[4, 4]} wrap>
            <Tag color="green">成功 {counts.success ?? 0}</Tag>
            <Tag color="blue">运行 {counts.running ?? 0}</Tag>
            <Tag color="gold">等待 {counts.pending ?? 0}</Tag>
            <Tag color="red">失败 {counts.failed ?? 0}</Tag>
            <Tag>跳过 {counts.skipped ?? 0}</Tag>
          </Space>
        );
      },
    },
    {
      title: "失败原因",
      dataIndex: "failure_reason",
      key: "failure_reason",
      render: (reason) => reason ? (
        <Typography.Text type="danger" style={{ fontSize: 12 }}>{reason}</Typography.Text>
      ) : "-",
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      responsive: ["lg"],
      render: (value) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(value, "yyyy-MM-dd HH:mm")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, rollout) =>
        rollout.status === "pending" || rollout.status === "running" ? (
          <Popconfirm
            title="取消升级任务？"
            description="取消后未开始的节点会标记为跳过。"
            okText="确认取消"
            cancelText="继续保留"
            onConfirm={() => onCancel(rollout.id)}
          >
            <Button size="small" danger loading={cancelling}>取消</Button>
          </Popconfirm>
        ) : null,
    },
  ];
}
