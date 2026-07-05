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
  Select,
  Space,
  Table,
  Typography,
} from "antd";
import {
  CameraOutlined,
  CheckCircleOutlined,
  InfoCircleOutlined,
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
import type { Snapshot } from "@/types/snapshot";
import { safeFormatDate } from "@/lib/date";
import { ErrorPanel } from "@/components/error-panel";
import { SnapshotTreeBrowser } from "@/components/snapshot-tree-browser";

const EVENT_OPTIONS = [
  { id: "backup_failed", label: "备份失败" },
  { id: "agent_offline", label: "节点离线" },
];

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
    mutationFn: (data: {
      snapshot_id: string;
      target_path: string;
      include_paths?: string[];
    }) => restoreSnapshot(agentId, data),
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

  const handleOpenRestore = (s: Snapshot) => {
    setSelectedSnapshot(s);
    setTargetPath(s.paths[0] || "");
    setConfirmed(false);
    setRestoreSuccessId(null);
    setIncludePaths([]);
    restoreMutation.reset();
  };

  const handleRestore = () => {
    if (!confirmed) return;
    restoreMutation.mutate({
      snapshot_id: selectedSnapshot!.id,
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
      render: (_, record) => (
        <Button
          type="link"
          size="small"
          icon={<UndoOutlined />}
          onClick={() => handleOpenRestore(record)}
        >
          恢复
        </Button>
      ),
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
      <div
        className="vf-page-header"
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          快照浏览
        </Typography.Title>
        <Space className="vf-page-actions">
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
        </Space>
      </div>

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

            <SnapshotTreeBrowser
              agentId={agentId}
              snapshotId={selectedSnapshot?.id || ""}
              isAgentOnline={
                agents?.find((a) => a.id === agentId)?.status === "online"
              }
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
              disabled={!confirmed || restoreMutation.isPending}
              onClick={handleRestore}
              loading={restoreMutation.isPending}
            >
              {includePaths.length > 0
                ? `恢复选中的 ${includePaths.length} 项`
                : "恢复全部"}
            </Button>
          </Form>
        )}
      </Drawer>
    </div>
  );
}
