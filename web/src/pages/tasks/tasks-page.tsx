import { useMemo, useState } from "react";
import {
  App,
  Button,
  Card,
  Col,
  Dropdown,
  Empty,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  CloseOutlined,
  DownloadOutlined,
  HistoryOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { backupNow, listAgents } from "@/services/agents";
import { cancelTask, listTasks, taskArtifactDownloadUrl } from "@/services/tasks";
import type { TaskHistory } from "@/types/task";
import { safeFormatDate } from "@/lib/date";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes < 1024 * 1024 * 1024 * 1024)
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
  return `${(bytes / 1024 / 1024 / 1024 / 1024).toFixed(2)} TB`;
}

export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "";
  if (bytesPerSec < 1024) return `${bytesPerSec} B/s`;
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSec / 1024 / 1024).toFixed(1)} MB/s`;
}

export function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  if (hours > 0) return `${hours}:${pad(minutes)}:${pad(seconds)}`;
  return `${minutes}:${pad(seconds)}`;
}

export function TasksPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [backupAgentId, setBackupAgentId] = useState<string | null>(null);
  const [cancelTaskId, setCancelTaskId] = useState<string | null>(null);

  const filters = useMemo(
    () => ({
      agent_id: searchParams.get("agent_id") || undefined,
      status: searchParams.get("status") || undefined,
      type: searchParams.get("type") || undefined,
      limit: 100,
    }),
    [searchParams]
  );

  const { data: tasks, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["tasks", filters],
    queryFn: () => listTasks(filters),
    refetchInterval: (query) => {
      const data = query.state.data;
      const hasActive = data?.some(
        (t) => t.status === "pending" || t.status === "running"
      );
      return hasActive ? 5000 : false;
    },
  });

  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });

  const backupMutation = useMutation({
    mutationFn: (agentId: string) => backupNow(agentId),
    onSuccess: () => {
      setBackupAgentId(null);
      message.success("备份命令已下发");
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => cancelTask(taskId),
    onSuccess: () => {
      setCancelTaskId(null);
      message.success("取消命令已发送");
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => message.error(error.message),
  });

  const handleFilterChange = (key: string, value: string) => {
    const newParams = new URLSearchParams(searchParams);
    if (value && value !== "all") {
      newParams.set(key, value);
    } else {
      newParams.delete(key);
    }
    setSearchParams(newParams);
  };

  const columns = [
    {
      title: "节点",
      dataIndex: "agent_id",
      key: "agent_id",
      render: (v: string) =>
        agents?.find((a) => a.id === v)?.name || v,
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
      title: "耗时 / 大小",
      key: "metric",
      render: (_: unknown, record: TaskHistory) =>
        renderTaskMetricContent(record, (id) => setCancelTaskId(id)),
    },
    {
      title: "完成时间",
      dataIndex: "finished_at",
      key: "finished_at",
      render: (v: string) =>
        v ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {safeFormatDate(v, "yyyy-MM-dd HH:mm:ss")}
          </Typography.Text>
        ) : (
          "-"
        ),
    },
  ];

  const expandedRowRender = (task: TaskHistory) => (
    <div style={{ padding: "16px 24px", background: "#fafafa" }}>
      <Row gutter={[16, 16]}>
        <Col xs={24} md={8}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            Message ID
          </Typography.Text>
          <div>
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              {task.message_id}
            </Typography.Text>
          </div>
        </Col>
        {task.command_id && (
          <Col xs={24} md={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Command ID
            </Typography.Text>
            <div>
              <Typography.Text code copyable style={{ fontSize: 12 }}>
                {task.command_id}
              </Typography.Text>
            </div>
          </Col>
        )}
        {task.snapshot_id && (
          <Col xs={24} md={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Snapshot ID
            </Typography.Text>
            <div>
              <Typography.Text code copyable style={{ fontSize: 12 }}>
                {task.snapshot_id}
              </Typography.Text>
            </div>
          </Col>
        )}
        <Col xs={24} md={8}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            开始时间
          </Typography.Text>
          <div style={{ fontSize: 12 }}>
            {safeFormatDate(task.started_at, "yyyy-MM-dd HH:mm:ss")}
          </div>
        </Col>
        <Col xs={24} md={8}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            结束时间
          </Typography.Text>
          <div style={{ fontSize: 12 }}>
            {safeFormatDate(task.finished_at, "yyyy-MM-dd HH:mm:ss")}
          </div>
        </Col>
        <Col xs={24} md={8}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            关联信息
          </Typography.Text>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginTop: 4 }}>
            {task.policy_id && (
              <Tag color="purple" style={{ fontSize: 11 }}>
                策略:{task.policy_id.substring(0, 8)}
              </Tag>
            )}
            {task.storage_id && (
              <Tag style={{ fontSize: 11 }}>
                存储:{task.storage_id.substring(0, 8)}
              </Tag>
            )}
            {task.backup_mode === "archive" && (
              <Tag color="orange" style={{ fontSize: 11 }}>
                压缩包:{task.archive_format || "tar.gz"}
              </Tag>
            )}
            {task.backup_mode === "snapshot" && (
              <Tag color="green" style={{ fontSize: 11 }}>
                快照备份
              </Tag>
            )}
            {!task.policy_id && !task.storage_id && !task.backup_mode && (
              <Typography.Text type="secondary" italic style={{ fontSize: 11 }}>
                无
              </Typography.Text>
            )}
          </div>
        </Col>
      </Row>

      {task.error_log && (
        <Tooltip title="错误日志">
          <pre
            style={{
              marginTop: 12,
              padding: 12,
              background: "#fff1f0",
              color: "#cf1322",
              borderRadius: 6,
              fontSize: 12,
              fontFamily: "monospace",
              whiteSpace: "pre-wrap",
              maxHeight: 300,
              overflow: "auto",
              margin: "12px 0 0",
            }}
          >
            {task.error_log}
          </pre>
        </Tooltip>
      )}

      {!task.error_log && task.status === "success" && (
        <div style={{ marginTop: 12 }}>
          <Typography.Text type="success" style={{ fontSize: 12 }}>
            ✓ 任务执行成功，未捕获到错误输出。
          </Typography.Text>
          {task.backup_mode === "archive" && task.artifact_name && (
            <div
              style={{
                marginTop: 8,
                padding: "8px 12px",
                background: "#fffbe6",
                border: "1px solid #ffe58f",
                borderRadius: 6,
                display: "flex",
                gap: 8,
                alignItems: "center",
                flexWrap: "wrap",
              }}
            >
              <Typography.Text strong style={{ fontSize: 12, color: "#d48806" }}>
                压缩包已生成
              </Typography.Text>
              <Typography.Text style={{ fontSize: 12, color: "#d48806" }}>
                {task.artifact_name}
              </Typography.Text>
              {task.artifact_size ? (
                <Typography.Text style={{ fontSize: 12, color: "#d48806" }}>
                  {formatBytes(task.artifact_size)}
                </Typography.Text>
              ) : null}
              <a href={taskArtifactDownloadUrl(task.id)}>
                <Button size="small" icon={<DownloadOutlined />}>
                  下载压缩包
                </Button>
              </a>
            </div>
          )}
        </div>
      )}
    </div>
  );

  return (
    <div className="vf-page">
      <PageHeader
        title="任务历史"
        description="备份 / 恢复 / 取消 / 下载"
        icon={<HistoryOutlined />}
        actions={
          <>
          <Dropdown
            menu={{
              items:
                agents?.length === 0
                  ? [{ key: "empty", label: "暂无节点", disabled: true }]
                  : agents?.map((a) => ({
                      key: a.id,
                      label: a.name,
                      onClick: () => setBackupAgentId(a.id),
                    })),
            }}
          >
            <Button icon={<PlayCircleOutlined />}>手动备份</Button>
          </Dropdown>
          <Button
            icon={<ReloadOutlined spin={isFetching} />}
            onClick={() => refetch()}
            disabled={isFetching}
          >
            刷新
          </Button>
          </>
        }
      />

      <Card>
        <Row gutter={[12, 12]} style={{ marginBottom: 0 }}>
          <Col xs={24} sm={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              按节点筛选
            </Typography.Text>
            <Select
              style={{ width: "100%", marginTop: 4 }}
              value={filters.agent_id || "all"}
              onChange={(v) => handleFilterChange("agent_id", v)}
              options={[
                { value: "all", label: "全部节点" },
                ...(agents?.map((a) => ({
                  value: a.id,
                  label: a.name,
                })) || []),
              ]}
            />
          </Col>
          <Col xs={24} sm={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              任务类型
            </Typography.Text>
            <Select
              style={{ width: "100%", marginTop: 4 }}
              value={filters.type || "all"}
              onChange={(v) => handleFilterChange("type", v)}
              options={[
                { value: "all", label: "全部类型" },
                { value: "backup", label: "备份" },
                { value: "restore", label: "恢复" },
              ]}
            />
          </Col>
          <Col xs={24} sm={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              状态
            </Typography.Text>
            <Select
              style={{ width: "100%", marginTop: 4 }}
              value={filters.status || "all"}
              onChange={(v) => handleFilterChange("status", v)}
              options={[
                { value: "all", label: "全部状态" },
                { value: "pending", label: "等待中" },
                { value: "running", label: "运行中" },
                { value: "success", label: "成功" },
                { value: "failed", label: "失败" },
                { value: "timeout", label: "超时" },
                { value: "cancelled", label: "已取消" },
              ]}
            />
          </Col>
        </Row>
      </Card>

      <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
        <Table<TaskHistory>
          columns={columns as any}
          dataSource={tasks}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 20, showSizeChanger: true }}
          scroll={{ x: 860 }}
          expandable={{
            expandedRowKeys: expandedId ? [expandedId] : [],
            onExpand: (exp, record) =>
              setExpandedId(exp ? record.id : null),
            expandedRowRender,
          }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无符合条件的任务"
              />
            ),
          }}
        />
      </Card>

      <ConfirmDialog
        open={!!backupAgentId}
        onOpenChange={(open) => !open && setBackupAgentId(null)}
        title="确认手动备份"
        description={`将对节点 ${
          agents?.find((a) => a.id === backupAgentId)?.name || backupAgentId
        } 发起立即备份。`}
        confirmText="立即备份"
        variant="default"
        onConfirm={() => backupAgentId && backupMutation.mutate(backupAgentId)}
        loading={backupMutation.isPending}
      />

      <ConfirmDialog
        open={!!cancelTaskId}
        onOpenChange={(open) => !open && setCancelTaskId(null)}
        title="确认取消任务"
        description="确认取消此任务？取消后已上传的数据不会丢失，下次备份会继续。"
        confirmText="确认取消"
        onConfirm={() => cancelTaskId && cancelMutation.mutate(cancelTaskId)}
        loading={cancelMutation.isPending}
      />
    </div>
  );
}

export function renderTaskMetricContent(
  task: TaskHistory,
  onCancel?: (taskId: string) => void
) {
  if (task.status === "pending" || task.status === "running") {
    return (
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <div style={{ flex: 1 }}>{renderTaskProgress(task)}</div>
        {onCancel && (
          <Button
            type="text"
            size="small"
            danger
            icon={<CloseOutlined />}
            onClick={(e) => {
              e.stopPropagation();
              onCancel(task.id);
            }}
            aria-label="取消任务"
            title="取消任务"
          />
        )}
      </div>
    );
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", minHeight: 32, justifyContent: "center" }}>
      <span>{task.duration_ms ? formatDuration(task.duration_ms) : "-"}</span>
      {task.repo_size ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {formatBytes(task.repo_size)}
        </Typography.Text>
      ) : null}
    </div>
  );
}

function formatProgressPercent(percentDone: number): number {
  return Math.round(percentDone <= 1 ? percentDone * 100 : percentDone);
}

function renderTaskProgress(task: TaskHistory) {
  const progress = task.progress;
  if (!progress) {
    return <ProgressText muted pulse text="准备中..." />;
  }
  switch (progress.phase) {
    case "init":
      return <ProgressText muted text="初始化仓库..." />;
    case "backup": {
      const percent = formatProgressPercent(progress.percent_done);
      const speed = formatSpeed(progress.bytes_per_sec);
      return (
        <div style={{ display: "flex", flexDirection: "column", minHeight: 32, justifyContent: "center", maxWidth: 240 }}>
          <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>
            {`上传中: ${formatBytes(progress.bytes_done)} / ${formatBytes(
              progress.total_bytes
            )} (${percent}%)`}
          </span>
          {speed ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {`↑${speed}`}
            </Typography.Text>
          ) : null}
        </div>
      );
    }
    case "forget":
      return <ProgressText muted text="清理旧快照..." />;
    case "stats":
      return <ProgressText muted text="统计仓库大小..." />;
    default:
      return <ProgressText muted pulse text="处理中..." />;
  }
}

function ProgressText({
  text,
  muted,
  pulse,
}: {
  text: string;
  muted?: boolean;
  pulse?: boolean;
}) {
  return (
    <div
      style={{
        display: "flex",
        minHeight: 32,
        alignItems: "center",
        color: muted ? "rgba(0,0,0,0.45)" : undefined,
        animation: pulse ? "pulse 2s infinite" : undefined,
      }}
    >
      {text}
    </div>
  );
}
