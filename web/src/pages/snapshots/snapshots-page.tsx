import { useState } from "react";
import {
  Alert,
  App,
  Button,
  Checkbox,
  Drawer,
  Empty,
  Form,
  Input,
  Result,
  Row,
  Col,
  Segmented,
  Select,
  Space,
  Table,
  Typography,
} from "antd";
import {
  CameraOutlined,
  CheckCircleOutlined,
  InfoCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  UndoOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import {
  listSnapshots,
  refreshSnapshots,
  restoreSnapshot,
} from "@/services/snapshots";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listStorage } from "@/services/storage";
import type { RestoreRequest, Snapshot } from "@/types/snapshot";
import type { DockerResolvedSource } from "@/types/task";
import { safeFormatDate } from "@/lib/date";
import { ErrorPanel } from "@/components/error-panel";
import { PageHeader } from "@/components/page-header";
import { SnapshotTreeBrowser } from "@/components/snapshot-tree-browser";

const EVENT_OPTIONS = [
  { id: "backup_failed", label: "备份失败" },
  { id: "agent_offline", label: "节点离线" },
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

export function SnapshotsPage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const agentId = searchParams.get("agent_id") || "";

  const [selectedSnapshot, setSelectedSnapshot] = useState<Snapshot | null>(
    null
  );
  const [targetPath, setTargetPath] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [restoreSuccessId, setRestoreSuccessId] = useState<string | null>(
    null
  );
  const [includePaths, setIncludePaths] = useState<string[]>([]);
  const [restoreMode, setRestoreMode] = useState<"files" | "docker_container">("files");
  const [selectedDockerSourceId, setSelectedDockerSourceId] = useState("");

  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });
  const { data: policies } = useQuery({
    queryKey: ["policies"],
    queryFn: () => listPolicies(),
  });
  const { data: storageList } = useQuery({
    queryKey: ["storage"],
    queryFn: listStorage,
  });
  const { data: snapshots, isLoading, isFetching } = useQuery({
    queryKey: ["snapshots", agentId],
    queryFn: () => listSnapshots(agentId),
    enabled: !!agentId,
  });

  const refreshMutation = useMutation({
    mutationFn: () => refreshSnapshots(agentId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["snapshots", agentId] });
      if (data.message === "snapshot refresh queued") {
        message.info("快照刷新已排队，Agent 离线，上线后将自动执行");
      } else {
        message.success("快照列表刷新成功");
      }
    },
    onError: (error: any) => message.error(error.message),
  });

  const restoreMutation = useMutation({
    mutationFn: (data: RestoreRequest) => restoreSnapshot(agentId, data),
    onSuccess: (data) => {
      setRestoreSuccessId(data.message_id);
      const msg =
        data.message === "restore queued" ? "恢复命令已排队" : "恢复任务已开始";
      message.success(msg);
    },
    onError: (error: any) => message.error(error.message),
  });

  const handleAgentChange = (val: string) => {
    const newParams = new URLSearchParams(searchParams);
    if (val && val !== "all") newParams.set("agent_id", val);
    else newParams.delete("agent_id");
    setSearchParams(newParams);
  };

  const currentAgent = agents?.find((a) => a.id === agentId);
  const selectedDockerSources = selectedSnapshot?.docker?.sources ?? [];
  const agentSupportsDockerRestore = !!currentAgent?.capabilities?.includes("docker_container_restore");
  const canRestoreSelectedDocker = agentSupportsDockerRestore && selectedDockerSources.length > 0;
  const restoreConfirmed = confirmed && (
    restoreMode === "docker_container"
      ? canRestoreSelectedDocker && !!selectedDockerSourceId
      : !!targetPath
  );

  const handleOpenRestore = (s: Snapshot, mode: "files" | "docker_container" = "files") => {
    const sources = s.docker?.sources ?? [];
    setSelectedSnapshot(s);
    setRestoreMode(mode);
    setTargetPath(mode === "files" ? s.paths[0] || "" : "");
    setSelectedDockerSourceId(mode === "docker_container" && sources[0] ? dockerSourceKey(sources[0]) : "");
    setConfirmed(false);
    setRestoreSuccessId(null);
    setIncludePaths([]);
    restoreMutation.reset();
  };

  const handleRestore = () => {
    if (!restoreConfirmed || !selectedSnapshot) return;
    if (restoreMode === "docker_container") {
      restoreMutation.mutate({
        snapshot_id: selectedSnapshot.id,
        restore_mode: "docker_container",
        docker_source_id: selectedDockerSourceId,
      });
      return;
    }
    restoreMutation.mutate({
      snapshot_id: selectedSnapshot.id,
      restore_mode: "files",
      target_path: targetPath,
      ...(includePaths.length > 0 ? { include_paths: includePaths } : {}),
    });
  };

  const columns: ColumnsType<Snapshot> = [
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      render: (v: string) => (
        <Typography.Text code style={{ fontSize: 12 }}>
          {v.substring(0, 8)}
        </Typography.Text>
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
      title: "包含路径",
      dataIndex: "paths",
      key: "paths",
      render: (v: string[]) => (
        <Typography.Text ellipsis style={{ fontSize: 12, maxWidth: 300 }}>
          {v.join(", ")}
        </Typography.Text>
      ),
    },
    {
      title: "主机 / 用户",
      key: "host",
      render: (_, record) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {record.hostname} / {record.username}
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
              icon={<UndoOutlined />}
              onClick={() => handleOpenRestore(record, "files")}
            >
              恢复
            </Button>
            {hasDocker && (
              <Button
                type="link"
                size="small"
                icon={<PlayCircleOutlined />}
                onClick={() => handleOpenRestore(record, "docker_container")}
              >
                恢复容器
              </Button>
            )}
          </Space>
        );
      },
    },
  ];

  const policyColumns: ColumnsType<any> = [
    {
      title: "节点",
      dataIndex: "agent_id",
      key: "agent_id",
      render: (v: string) =>
        agents?.find((a) => a.id === v)?.name || v,
    },
    {
      title: "仓库路径",
      dataIndex: "repo_path",
      key: "repo_path",
      render: (v: string) => (
        <Typography.Text code style={{ fontSize: 12 }}>
          {v}
        </Typography.Text>
      ),
    },
    {
      title: "存储",
      dataIndex: "storage_id",
      key: "storage_id",
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {storageList?.find((s) => s.id === v)?.name || v.substring(0, 8)}
        </Typography.Text>
      ),
    },
  ];

  return (
    <div className="vf-page">
      <PageHeader
        title="快照浏览"
        description="节点快照 / 浏览 / 恢复"
        icon={<CameraOutlined />}
        actions={
          <>
          <Select
            className="vf-mobile-full"
            style={{ width: 200 }}
            placeholder="选择节点查看快照"
            value={agentId || undefined}
            onChange={handleAgentChange}
            options={agents?.map((a) => ({
              value: a.id,
              label: a.name,
            }))}
          />
          <Button
            icon={<ReloadOutlined spin={isFetching || refreshMutation.isPending} />}
            disabled={!agentId || isFetching || refreshMutation.isPending}
            onClick={() => refreshMutation.mutate()}
            title="请求 Agent 刷新快照列表"
          />
          </>
        }
      />

      {!agentId ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <Empty
            image={
              <CameraOutlined
                style={{ fontSize: 56, color: "rgba(0,0,0,0.25)" }}
              />
            }
            description={
              <Typography.Text type="secondary">
                请选择一个节点以查看其备份快照
              </Typography.Text>
            }
            style={{ padding: "48px 0" }}
          />
          {policies && policies.length > 0 && (
            <div>
              <Space>
                <InfoCircleOutlined />
                <Typography.Title level={5} style={{ margin: 0 }}>
                  跨节点恢复
                </Typography.Title>
              </Space>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                如需将数据恢复到新节点，请在新节点上创建策略时使用相同的
                <Typography.Text strong>存储</Typography.Text>和
                <Typography.Text strong>仓库子路径</Typography.Text>。
                策略同步后，原有快照将自动出现在新节点下。
              </Typography.Paragraph>
              <Table
                columns={policyColumns}
                dataSource={policies}
                rowKey="id"
                pagination={false}
                scroll={{ x: 620 }}
                size="small"
              />
            </div>
          )}
        </div>
      ) : (
        <Table
          columns={columns}
          dataSource={snapshots || []}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 760 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="该节点暂无快照"
              />
            ),
          }}
        />
      )}

      <Drawer
        title="恢复数据"
        open={!!selectedSnapshot}
        onClose={() => setSelectedSnapshot(null)}
        width="min(100vw, 480px)"
        destroyOnClose
      >
        {restoreSuccessId ? (
          <Result
            status="success"
            title="恢复任务已提交"
            subTitle={`Message ID: ${restoreSuccessId}`}
            extra={[
              <Button
                type="primary"
                key="tasks"
                onClick={() => navigate(`/tasks?agent_id=${agentId}`)}
              >
                查看任务进度
              </Button>,
              <Button key="close" onClick={() => setSelectedSnapshot(null)}>
                关闭
              </Button>,
            ]}
          />
        ) : (
          <Form layout="vertical">
            <ErrorPanel error={restoreMutation.error as any} />
            <Form.Item label="快照 ID">
              <Typography.Text code style={{ fontSize: 12 }}>
                {selectedSnapshot?.id}
              </Typography.Text>
            </Form.Item>
            <Form.Item label="快照时间">
              <Typography.Text style={{ fontSize: 13 }}>
                {selectedSnapshot &&
                  safeFormatDate(
                    selectedSnapshot.time,
                    "yyyy-MM-dd HH:mm:ss"
                  )}
              </Typography.Text>
            </Form.Item>

            <Form.Item label="恢复模式">
              <Segmented
                value={restoreMode}
                onChange={(value) => {
                  const mode = value as "files" | "docker_container";
                  setRestoreMode(mode);
                  if (mode === "docker_container" && selectedDockerSources[0]) {
                    setTargetPath("");
                    setIncludePaths([]);
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
              <>
                <SnapshotTreeBrowser
                  agentId={agentId}
                  snapshotId={selectedSnapshot?.id || ""}
                  isAgentOnline={currentAgent?.status === "online"}
                  selectedPaths={includePaths}
                  onSelectedPathsChange={setIncludePaths}
                />

                <Form.Item label="目标路径" style={{ marginTop: 16 }}>
                  <Input
                    value={targetPath}
                    onChange={(e) => setTargetPath(e.target.value)}
                    placeholder="如: /tmp/restore-data"
                  />
                </Form.Item>
              </>
            )}

            <Alert
              type="warning"
              message={
                <Checkbox
                  checked={confirmed}
                  onChange={(e) => setConfirmed(e.target.checked)}
                >
                  <Typography.Text style={{ fontSize: 12 }}>
                    确认恢复：我了解此操作将在 Agent 节点上执行，且目标路径必须是可写的。
                  </Typography.Text>
                </Checkbox>
              }
              showIcon={false}
            />

            <Button
              type="primary"
              block
              size="large"
              style={{ marginTop: 16 }}
              disabled={!restoreConfirmed || restoreMutation.isPending}
              onClick={handleRestore}
              loading={restoreMutation.isPending}
            >
              {restoreMode === "docker_container"
                ? "恢复容器"
                : includePaths.length > 0
                ? `恢复选中的 ${includePaths.length} 项`
                : "恢复全部"}
            </Button>
          </Form>
        )}
      </Drawer>
    </div>
  );
}
