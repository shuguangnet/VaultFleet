import React, { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  Modal,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  CheckCircleOutlined,
  CloudUploadOutlined,
  CopyOutlined,
  FolderOpenOutlined,
  InfoCircleOutlined,
  PlayCircleOutlined,
  RedoOutlined,
  ReloadOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  backupNow,
  getAgent,
  updateAgent,
} from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listTasks } from "@/services/tasks";
import {
  listSnapshots,
  restoreSnapshot,
} from "@/services/snapshots";
import { listAgentCommands } from "@/services/commands";
import { StatusBadge } from "@/components/status-badge";
import { DirectoryBrowser } from "@/components/directory-browser";
import { safeFormatDate } from "@/lib/date";
import { copyToClipboard } from "@/lib/utils";
import { formatBytes } from "@/pages/tasks/tasks-page";
import type { RestoreRequest, Snapshot } from "@/types/snapshot";
import type { DockerResolvedSource } from "@/types/task";

const COMMAND_TYPE_LABELS: Record<string, string> = {
  backup_now: "手动备份",
  restore_req: "恢复",
  selective_restore_req: "恢复",
  policy_push: "策略下发",
  snapshot_list_req: "快照刷新",
  update_agent: "Agent 更新",
};

export function NodeDetailPage() {
  const { message } = App.useApp();
  const { agentId } = useParams<{ agentId: string }>();
  const queryClient = useQueryClient();
  const [selectedSnapshot, setSelectedSnapshot] = useState<Snapshot | null>(null);
  const [targetPath, setTargetPath] = useState("");
  const [restoreMode, setRestoreMode] = useState<"files" | "docker_container">("files");
  const [selectedDockerSourceId, setSelectedDockerSourceId] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [expandedTaskId, setExpandedTaskId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState("overview");

  const { data: agent, isLoading: agentLoading } = useQuery({
    queryKey: ["agent", agentId],
    queryFn: () => getAgent(agentId!),
    enabled: !!agentId,
  });
  const { data: policies } = useQuery({
    queryKey: ["policies", { agent_id: agentId }],
    queryFn: () => listPolicies(agentId),
    enabled: !!agentId,
  });
  const { data: snapshots } = useQuery({
    queryKey: ["snapshots", agentId],
    queryFn: () => listSnapshots(agentId!),
    enabled: !!agentId,
  });
  const { data: tasks } = useQuery({
    queryKey: ["tasks", { agent_id: agentId }],
    queryFn: () => listTasks({ agent_id: agentId }),
    enabled: !!agentId,
  });
  const {
    data: commands,
    isFetching: commandsFetching,
    refetch: refetchCommands,
  } = useQuery({
    queryKey: ["commands", agentId],
    queryFn: () => listAgentCommands(agentId!),
    enabled: !!agentId,
    refetchInterval: (query) => {
      const data = query.state.data;
      const hasActive = data?.some(
        (c) =>
          c.status === "pending" ||
          c.status === "dispatched" ||
          c.status === "running"
      );
      return hasActive ? 5000 : false;
    },
  });

  const backupMutation = useMutation({
    mutationFn: () => backupNow(agentId!),
    onSuccess: (data) => {
      if (agent?.status === "online") {
        message.success(`备份命令已下发 (Message ID: ${data.message_id})`);
      } else {
        message.info("备份命令已排队，Agent 上线后将自动执行");
      }
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
      queryClient.invalidateQueries({ queryKey: ["commands"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const updateMutation = useMutation({
    mutationFn: () => updateAgent(agentId!),
    onSuccess: (data) => {
      message.success(`Agent 更新请求已确认 (目标版本: ${data.version})`);
      queryClient.invalidateQueries({ queryKey: ["agent", agentId] });
      queryClient.invalidateQueries({ queryKey: ["commands"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const restoreMutation = useMutation({
    mutationFn: (body: RestoreRequest) => restoreSnapshot(agentId!, body),
    onSuccess: (data) => {
      const msg =
        data.message === "restore queued" ? "恢复命令已排队" : "恢复任务已开始";
      message.success(`${msg} (Message ID: ${data.message_id})`);
      setSelectedSnapshot(null);
      setRestoreMode("files");
      setSelectedDockerSourceId("");
      setConfirmed(false);
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  if (agentLoading) {
    return (
      <div style={{ padding: 40, textAlign: "center" }}>
        <Spin size="large" />
      </div>
    );
  }
  if (!agent) {
    return <Alert type="warning" message="节点未找到" showIcon />;
  }

  const policyColumns: ColumnsType<any> = [
    {
      title: "调度",
      dataIndex: "schedule",
      key: "schedule",
      render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text>,
    },
    {
      title: "备份路径",
      dataIndex: "backup_dirs",
      key: "backup_dirs",
      render: (v: string[]) => (
        <Typography.Text ellipsis style={{ fontSize: 12, maxWidth: 240 }}>
          {v.join(", ")}
        </Typography.Text>
      ),
    },
    {
      title: "状态",
      dataIndex: "synced",
      key: "synced",
      render: (v: boolean) => <StatusBadge status={v ? "success" : "unsynced"} />,
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Link to={`/policies?id=${record.id}`}>
          <Button type="link" size="small">详情</Button>
        </Link>
      ),
    },
  ];

  const dockerSourceKey = (source: DockerResolvedSource) => {
    if (source.container_id) return source.container_id;
    if (source.name) return source.name;
    const compose = source.compose;
    if (compose?.project && compose?.service) {
      return `${compose.project}/${compose.service}`;
    }
    return "";
  };

  const dockerSourceLabel = (source: DockerResolvedSource) => {
    const name = source.name || source.container_id?.substring(0, 12) || "容器";
    const image = source.image ? ` (${source.image})` : "";
    const compose = source.compose?.project && source.compose?.service
      ? ` - ${source.compose.project}/${source.compose.service}`
      : "";
    return `${name}${image}${compose}`;
  };

  const openRestoreModal = (snapshot: Snapshot, mode: "files" | "docker_container") => {
    const sources = snapshot.docker?.sources ?? [];
    setSelectedSnapshot(snapshot);
    setRestoreMode(mode);
    setTargetPath(mode === "files" ? snapshot.paths[0] || "" : "");
    setSelectedDockerSourceId(mode === "docker_container" && sources[0] ? dockerSourceKey(sources[0]) : "");
    setConfirmed(false);
  };

  const selectedDockerSources = selectedSnapshot?.docker?.sources ?? [];
  const agentSupportsDockerRestore = !!agent.capabilities?.includes("docker_container_restore");
  const canRestoreSelectedDocker = agentSupportsDockerRestore && selectedDockerSources.length > 0;
  const restoreConfirmed = confirmed && (
    restoreMode === "docker_container"
      ? canRestoreSelectedDocker && !!selectedDockerSourceId
      : !!targetPath
  );

  const snapshotColumns: ColumnsType<Snapshot> = [
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      render: (v: string) => (
        <Typography.Text code style={{ fontSize: 12 }}>{v.substring(0, 8)}</Typography.Text>
      ),
    },
    {
      title: "时间",
      dataIndex: "time",
      key: "time",
      render: (v: string) => (
        <Typography.Text style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd HH:mm:ss")}
        </Typography.Text>
      ),
    },
    {
      title: "路径",
      dataIndex: "paths",
      key: "paths",
      render: (v: string[]) => (
        <Typography.Text ellipsis style={{ fontSize: 12, maxWidth: 300 }}>
          {v.join(", ")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => {
        const hasDocker = agentSupportsDockerRestore && (record.docker?.sources?.length ?? 0) > 0;
        return (
          <Space size={4}>
            <Button
              type="link"
              size="small"
              icon={<RedoOutlined />}
              onClick={() => openRestoreModal(record, "files")}
            >
              恢复
            </Button>
            {hasDocker && (
              <Button
                type="link"
                size="small"
                icon={<PlayCircleOutlined />}
                onClick={() => openRestoreModal(record, "docker_container")}
              >
                恢复容器
              </Button>
            )}
          </Space>
        );
      },
    },
  ];

  const taskColumns: ColumnsType<any> = [
    {
      key: "expand",
      width: 40,
      render: (_, record) => (
        <Button
          type="text"
          size="small"
          icon={expandedTaskId === record.id ? <InfoCircleOutlined /> : <InfoCircleOutlined />}
          onClick={() =>
            setExpandedTaskId(expandedTaskId === record.id ? null : record.id)
          }
        />
      ),
    },
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      render: (t: string) => (t === "backup" ? "备份" : "恢复"),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s: string) => <StatusBadge status={s as any} />,
    },
    {
      title: "时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd HH:mm:ss")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Button
          type="link"
          size="small"
          onClick={() =>
            setExpandedTaskId(expandedTaskId === record.id ? null : record.id)
          }
        >
          详情
        </Button>
      ),
    },
  ];

  const commandColumns: ColumnsType<any> = [
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      render: (v: string) => COMMAND_TYPE_LABELS[v] || v,
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s: string) => <StatusBadge status={s as any} />,
    },
    { title: "尝试", dataIndex: "attempts", key: "attempts" },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "MM-dd HH:mm:ss")}
        </Typography.Text>
      ),
    },
    {
      title: "完成时间",
      dataIndex: "completed_at",
      key: "completed_at",
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "MM-dd HH:mm:ss")}
        </Typography.Text>
      ),
    },
    {
      title: "错误信息",
      dataIndex: "error_message",
      key: "error_message",
      render: (v: string) =>
        v ? (
          <Tooltip title={v}>
            <Typography.Text type="danger" ellipsis style={{ fontSize: 12, maxWidth: 160 }}>
              {v}
            </Typography.Text>
          </Tooltip>
        ) : (
          "-"
        ),
    },
  ];

  const expandedRowRender = (record: any) => (
    <div style={{ padding: "8px 24px", background: "#fafafa" }}>
      <Row gutter={24}>
        <Col xs={24} md={12}>
          <Descriptions column={1} size="small" colon labelStyle={{ width: 120 }}>
            <Descriptions.Item label="Message ID">
              <Typography.Text code>{record.message_id}</Typography.Text>
            </Descriptions.Item>
            {record.command_id && (
              <Descriptions.Item label="Command ID">
                <Typography.Text code>{record.command_id}</Typography.Text>
              </Descriptions.Item>
            )}
            {record.snapshot_id && (
              <Descriptions.Item label="Snapshot ID">
                <Typography.Text code>{record.snapshot_id}</Typography.Text>
              </Descriptions.Item>
            )}
            <Descriptions.Item label="开始时间">
              {safeFormatDate(record.started_at, "yyyy-MM-dd HH:mm:ss")}
            </Descriptions.Item>
            <Descriptions.Item label="结束时间">
              {safeFormatDate(record.finished_at, "yyyy-MM-dd HH:mm:ss")}
            </Descriptions.Item>
          </Descriptions>
        </Col>
        <Col xs={24} md={12}>
          <Descriptions column={1} size="small" colon labelStyle={{ width: 120 }}>
            {record.duration_ms !== undefined && (
              <Descriptions.Item label="耗时">
                {(record.duration_ms / 1000).toFixed(2)}s
              </Descriptions.Item>
            )}
            {record.repo_size !== undefined && (
              <Descriptions.Item label="仓库大小">
                {formatBytes(record.repo_size)}
              </Descriptions.Item>
            )}
          </Descriptions>
          {record.error_log && (
            <Alert
              type="error"
              style={{ marginTop: 8 }}
              message={
                <pre
                  style={{
                    margin: 0,
                    whiteSpace: "pre-wrap",
                    fontSize: 11,
                    fontFamily: "monospace",
                    maxHeight: 200,
                    overflow: "auto",
                  }}
                >
                  {record.error_log}
                </pre>
              }
            />
          )}
        </Col>
      </Row>
    </div>
  );

  return (
    <div className="vf-page">
      <div
        className="vf-page-header"
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          gap: 12,
          flexWrap: "wrap",
        }}
      >
        <Space direction="vertical" size={4}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            {agent.name}
          </Typography.Title>
          <Space>
            <StatusBadge status={agent.status as any} />
            <Typography.Text type="secondary">ID: {agent.id}</Typography.Text>
          </Space>
        </Space>
        <Space className="vf-page-actions">
          <Button
            icon={<CloudUploadOutlined />}
            disabled={agent.status !== "online" || updateMutation.isPending}
            onClick={() => updateMutation.mutate()}
            loading={updateMutation.isPending}
          >
            更新 Agent
          </Button>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            onClick={() => backupMutation.mutate()}
            loading={backupMutation.isPending}
            disabled={backupMutation.isPending}
          >
            立即备份
          </Button>
        </Space>
      </div>

      <Card styles={{ body: { padding: 16 } }}>
        <Tabs
          activeKey={activeTab}
          onChange={setActiveTab}
          items={[
            {
              key: "overview",
              label: "概览",
              children: (
                <Row gutter={16}>
                  <Col xs={24} md={12} lg={8}>
                    <Card type="inner" title="系统信息" size="small">
                      <Descriptions column={1} size="small">
                        <Descriptions.Item label="操作系统">{agent.os}</Descriptions.Item>
                        <Descriptions.Item label="架构">{agent.arch}</Descriptions.Item>
                        <Descriptions.Item label="主机名">{agent.hostname}</Descriptions.Item>
                        <Descriptions.Item label="Agent 版本">
                          {agent.version ? `v${agent.version}` : "未知"}
                        </Descriptions.Item>
                      </Descriptions>
                    </Card>
                  </Col>
                  <Col xs={24} md={12} lg={8}>
                    <Card type="inner" title="连接状态" size="small">
                      <Descriptions column={1} size="small">
                        <Descriptions.Item label="当前状态">
                          <StatusBadge status={agent.status as any} />
                        </Descriptions.Item>
                        <Descriptions.Item label="最后在线">
                          {safeFormatDate(agent.last_seen, "yyyy-MM-dd HH:mm:ss", "从未")}
                        </Descriptions.Item>
                        <Descriptions.Item label="创建时间">
                          {safeFormatDate(agent.created_at, "yyyy-MM-dd HH:mm:ss")}
                        </Descriptions.Item>
                      </Descriptions>
                    </Card>
                  </Col>
                </Row>
              ),
            },
            {
              key: "policy",
              label: "策略",
              children: (
                <Table
                  columns={policyColumns}
                  dataSource={policies || []}
                  rowKey="id"
                  pagination={false}
                  scroll={{ x: 620 }}
                  size="middle"
                  locale={{
                    emptyText: (
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description="无关联策略"
                      />
                    ),
                  }}
                />
              ),
            },
            {
              key: "snapshots",
              label: "快照",
              children: (
                <Table
                  columns={snapshotColumns}
                  dataSource={snapshots || []}
                  rowKey="id"
                  pagination={{ pageSize: 10 }}
                  scroll={{ x: 720 }}
                  size="middle"
                  locale={{
                    emptyText: (
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description="暂无快照"
                      />
                    ),
                  }}
                />
              ),
            },
            {
              key: "tasks",
              label: "任务",
              children: (
                <Table
                  columns={taskColumns}
                  dataSource={tasks || []}
                  rowKey="id"
                  pagination={{ pageSize: 10 }}
                  scroll={{ x: 720 }}
                  expandable={{
                    expandedRowKeys: expandedTaskId ? [expandedTaskId] : [],
                    onExpand: (exp, record) =>
                      setExpandedTaskId(exp ? record.id : null),
                    expandedRowRender,
                  }}
                  size="middle"
                  locale={{
                    emptyText: (
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description="暂无任务"
                      />
                    ),
                  }}
                />
              ),
            },
            {
              key: "commands",
              label: "命令",
              children: (
                <Card
                  size="small"
                  styles={{ body: { padding: 0 } }}
                  title="命令队列"
                  extra={
                    <Button
                      icon={<ReloadOutlined />}
                      onClick={() => refetchCommands()}
                      loading={commandsFetching}
                      size="small"
                      type="text"
                    />
                  }
                >
                  <Table
                    columns={commandColumns}
                    dataSource={commands || []}
                    rowKey="id"
                    pagination={{ pageSize: 10 }}
                    scroll={{ x: 860 }}
                    size="small"
                    locale={{
                      emptyText: (
                        <Empty
                          image={Empty.PRESENTED_IMAGE_SIMPLE}
                          description="暂无命令"
                        />
                      ),
                    }}
                  />
                </Card>
              ),
            },
            {
              key: "browser",
              label: "文件浏览",
              children: (
                <Card size="small" title="文件浏览器">
                  {agent.status === "online" ? (
                    <DirectoryBrowser
                      agentId={agent.id}
                      onSelect={(path) => {
                        copyToClipboard(path);
                        message.success(`路径已复制: ${path}`);
                      }}
                    />
                  ) : (
                    <Empty
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                      description="节点离线，无法使用文件浏览器"
                    />
                  )}
                </Card>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={!!selectedSnapshot}
        title="恢复快照"
        onCancel={() => setSelectedSnapshot(null)}
        onOk={() => {
          const body: RestoreRequest = { snapshot_id: selectedSnapshot!.id };
          if (restoreMode === "docker_container") {
            body.restore_mode = "docker_container";
            body.docker_source_id = selectedDockerSourceId;
          } else {
            body.restore_mode = "files";
            body.target_path = targetPath;
          }
          restoreMutation.mutate(body);
        }}
        okText="确认恢复"
        cancelText="取消"
        okButtonProps={{ disabled: !restoreConfirmed, loading: restoreMutation.isPending }}
        destroyOnClose
      >
        <Form layout="vertical" style={{ marginTop: 12 }}>
          <Form.Item label="恢复模式">
            <Segmented
              value={restoreMode}
              onChange={(value) => {
                const mode = value as "files" | "docker_container";
                setRestoreMode(mode);
                if (mode === "docker_container" && selectedDockerSources[0]) {
                  setTargetPath("");
                  setSelectedDockerSourceId(dockerSourceKey(selectedDockerSources[0]));
                } else {
                  setTargetPath(selectedSnapshot?.paths[0] || "");
                  setSelectedDockerSourceId("");
                }
              }}
              options={[
                { label: "文件", value: "files" },
                {
                  label: "容器",
                  value: "docker_container",
                  disabled: !canRestoreSelectedDocker,
                },
              ]}
            />
          </Form.Item>
          {restoreMode === "docker_container" ? (
            <Form.Item label="Docker 容器">
              <Select
                value={selectedDockerSourceId}
                onChange={setSelectedDockerSourceId}
                options={selectedDockerSources.map((source) => ({
                  value: dockerSourceKey(source),
                  label: dockerSourceLabel(source),
                }))}
              />
            </Form.Item>
          ) : (
            <Form.Item label="恢复目标路径">
              <Input
                value={targetPath}
                onChange={(e) => setTargetPath(e.target.value)}
                placeholder="/path/to/restore"
              />
            </Form.Item>
          )}
          <div
            style={{
              background: "#fffbe6",
              border: "1px solid #ffe58f",
              borderRadius: 6,
              padding: 12,
              display: "flex",
              gap: 8,
            }}
          >
            <Checkbox
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            >
              <Typography.Text style={{ fontSize: 12 }}>
                我理解恢复操作可能会覆盖目标路径下的现有文件
              </Typography.Text>
            </Checkbox>
          </div>
        </Form>
      </Modal>
    </div>
  );
}
