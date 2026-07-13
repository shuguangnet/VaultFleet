import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  App,
  Button,
  Card,
  Col,
  Drawer,
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
  CopyOutlined,
  DownloadOutlined,
  DeleteOutlined,
  FileTextOutlined,
  HistoryOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { backupNow, listAgents } from "@/services/agents";
import { cancelTask, deleteTask, getTaskLogs, listTasks, taskArtifactDownloadUrl } from "@/services/tasks";
import type { BackupProgress, TaskHistory, TaskLogLine, TaskLogStatus } from "@/types/task";
import { safeFormatDate } from "@/lib/date";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { ErrorPanel } from "@/components/error-panel";
import { StatusBadge } from "@/components/status-badge";
import { useAuth } from "@/contexts/auth-context";
import { permissions } from "@/services/identity";

import { formatBytes, formatSpeed, formatDuration } from "@/lib/format";
export { formatBytes, formatSpeed, formatDuration };

export function renderTaskManifestSummary(task: TaskHistory) {
  if (task.type !== "backup") {
    return null;
  }

  const manifest = task.manifest;
  const naming = task.artifact_naming || manifest?.artifact_naming;
  const artifact = manifest?.artifact || (
    task.artifact_name
      ? {
          name: task.artifact_name,
          path: task.artifact_path,
          format: task.archive_format,
          content_type: task.artifact_content_type,
          size: task.artifact_size,
        }
      : undefined
  );
  const warnings = manifest?.warnings || [];

  return (
    <div className="vf-task-manifest">
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        <Space wrap>
          <Typography.Text strong style={{ fontSize: 12 }}>
            备份内容清单
          </Typography.Text>
          {manifest ? (
            <>
              <Tag color="blue">v{manifest.version}</Tag>
              {manifest.agent?.hostname && <Tag>{manifest.agent.hostname}</Tag>}
              {manifest.backup_mode && <Tag>{manifest.backup_mode}</Tag>}
            </>
          ) : (
            <Tag color="default">旧备份无清单</Tag>
          )}
        </Space>

        {naming || manifest?.context_name || manifest?.source_type ? (
          <ManifestSection title="标识">
            {naming?.context_name || manifest?.context_name ? (
              <Tag color="blue">{naming?.context_name || manifest?.context_name}</Tag>
            ) : null}
            {naming?.source_type || manifest?.source_type ? (
              <Tag>{naming?.source_type || manifest?.source_type}</Tag>
            ) : null}
            {naming?.artifact_path ? <Tag color="orange">{naming.artifact_path}</Tag> : null}
            {naming?.legacy ? <Tag>legacy</Tag> : null}
          </ManifestSection>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            未保存备份产物命名信息。
          </Typography.Text>
        )}

        {manifest ? (
          <>
            {manifest.sources?.paths?.length ? (
              <ManifestSection title="路径">
                {manifest.sources.paths.slice(0, 6).map((source) => (
                  <Tag key={`${source.kind || "path"}-${source.path}`} style={{ maxWidth: "100%", whiteSpace: "normal" }}>
                    {source.kind || "path"}: {source.path}
                  </Tag>
                ))}
              </ManifestSection>
            ) : null}

            {manifest.sources?.docker?.length ? (
              <ManifestSection title="Docker">
                {manifest.sources.docker.map((source) => (
                  <Tag key={source.container_id || source.name || source.image}>
                    {source.compose_project || source.name || source.container_id || "container"}
                    {source.compose_service ? ` / ${source.compose_service}` : ""}
                    {source.image ? ` · ${source.image}` : ""}
                  </Tag>
                ))}
              </ManifestSection>
            ) : null}

            {manifest.sources?.databases?.length ? (
              <ManifestSection title="数据库">
                {manifest.sources.databases.map((dump) => (
                  <Tag key={`${dump.engine}-${dump.database || "all"}-${dump.output_name}`}>
                    {dump.engine || "database"}:{dump.all_databases ? "全部数据库" : dump.database || "-"}
                    {dump.output_name ? ` · ${dump.output_name}` : ""}
                    {dump.size ? ` · ${formatBytes(dump.size)}` : ""}
                  </Tag>
                ))}
              </ManifestSection>
            ) : null}

            {manifest.exclude_patterns?.length ? (
              <ManifestSection title="排除规则">
                {manifest.exclude_patterns.map((pattern) => (
                  <Tag key={pattern}>{pattern}</Tag>
                ))}
              </ManifestSection>
            ) : null}
          </>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            此任务由旧版本创建，任务历史没有保存 VAULTFLEET-MANIFEST.json。
            {task.docker?.sources?.length || task.database?.dumps?.length
              ? " 下方显示可用的 Docker / 数据库元数据。"
              : ""}
          </Typography.Text>
        )}

        {!manifest && task.docker?.sources?.length ? (
          <ManifestSection title="Docker">
            {task.docker.sources.map((source) => (
              <Tag key={source.container_id || source.name || source.image}>
                {source.name || source.container_id || "container"}
                {source.image ? ` · ${source.image}` : ""}
              </Tag>
            ))}
          </ManifestSection>
        ) : null}

        {!manifest && task.database?.dumps?.length ? (
          <ManifestSection title="数据库">
            {task.database.dumps.map((dump) => (
              <Tag key={`${dump.engine}-${dump.database || "all"}-${dump.output_name}`}>
                {dump.engine}:{dump.all_databases ? "全部数据库" : dump.database || "-"}
                {dump.output_name ? ` · ${dump.output_name}` : ""}
              </Tag>
            ))}
          </ManifestSection>
        ) : null}

        {artifact ? (
          <ManifestSection title="产物">
            {artifact.name && <Tag color="orange">{artifact.name}</Tag>}
            {artifact.path && <Tag>{artifact.path}</Tag>}
            {artifact.format && <Tag>{artifact.format}</Tag>}
            {artifact.content_type && <Tag>{artifact.content_type}</Tag>}
            {artifact.size ? <Tag>{formatBytes(artifact.size)}</Tag> : null}
          </ManifestSection>
        ) : null}

        {warnings.length ? (
          <ManifestSection title="告警">
            {warnings.map((warning, index) => (
              <Tag color="gold" key={`${warning.code || "warning"}-${index}`}>
                {warning.source ? `${warning.source}: ` : ""}
                {warning.message}
              </Tag>
            ))}
          </ManifestSection>
        ) : null}
      </Space>
    </div>
  );
}

function ManifestSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <Typography.Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 4 }}>
        {title}
      </Typography.Text>
      <Space wrap size={[4, 4]}>
        {children}
      </Space>
    </div>
  );
}

export function TasksPage() {
  const { message } = App.useApp();
  const auth = useAuth();
  const canRunBackup = auth.hasPermission(permissions.runBackup);
  const canRunRestore = auth.hasPermission(permissions.runRestore);
  const canViewLogs = auth.hasPermission(permissions.readOperational);
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [backupAgentId, setBackupAgentId] = useState<string | null>(null);
  const [cancelTaskId, setCancelTaskId] = useState<string | null>(null);
  const [deleteTaskId, setDeleteTaskId] = useState<string | null>(null);
  const [logTask, setLogTask] = useState<TaskHistory | null>(null);

  const filters = useMemo(
    () => ({
      agent_id: searchParams.get("agent_id") || undefined,
      status: searchParams.get("status") || undefined,
      type: searchParams.get("type") || undefined,
      limit: 100,
    }),
    [searchParams]
  );

  const { data: tasks, isLoading, refetch, isFetching, error: tasksError } = useQuery({
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

  const deleteMutation = useMutation({
    mutationFn: (taskId: string) => deleteTask(taskId),
    onSuccess: () => {
      setDeleteTaskId(null);
      message.success("任务历史已删除");
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => message.error("删除任务历史失败: " + error.message),
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
      title: "任务",
      key: "policy_name",
      render: (_: unknown, record: TaskHistory) => (
        <div>
          <Typography.Text strong>
            {record.policy_name || record.manifest?.policy?.name || taskTypeLabel(record.type)}
          </Typography.Text>
          {record.policy_id && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                {record.policy_id.slice(0, 8)}
              </Typography.Text>
            </div>
          )}
        </div>
      ),
    },
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      render: (t: string) => (t === "backup" ? "备份" : t === "verify" ? "验证" : "恢复"),
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
        renderTaskMetricContent(record, canRunBackup ? (id) => setCancelTaskId(id) : undefined),
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
    {
      title: "操作",
      key: "actions",
      fixed: "right",
      width: 88,
      render: (_: unknown, record: TaskHistory) => (
        <Space size={4}>
          {canViewLogs && taskSupportsLogs(record) ? (
            <Button
              size="small"
              icon={<FileTextOutlined />}
              onClick={(e) => {
                e.stopPropagation();
                setLogTask(record);
              }}
            >
              日志
            </Button>
          ) : null}
          {canRunBackup && record.status !== "running" && record.status !== "pending" ? (
            <Tooltip title="删除任务历史">
              <Button
                danger
                type="text"
                size="small"
                aria-label="删除任务历史"
                icon={<DeleteOutlined />}
                onClick={(e) => {
                  e.stopPropagation();
                  setDeleteTaskId(record.id);
                }}
              />
            </Tooltip>
          ) : null}
        </Space>
      ),
    },
  ];

  const expandedRowRender = (task: TaskHistory) => (
    <div className="vf-task-expanded-row">
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
        {task.policy_id && (
          <Col xs={24} md={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              策略
            </Typography.Text>
            <div>
              <Typography.Text style={{ fontSize: 12 }}>
                {task.policy_name || task.manifest?.policy?.name || task.policy_id}
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
              <Tag style={{ fontSize: 11 }}>
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
          <pre className="vf-task-error-log">
            {task.error_log}
          </pre>
        </Tooltip>
      )}

      {task.verification?.checks?.length ? (
        <div style={{ marginTop: 12 }}>
          <Typography.Text strong style={{ fontSize: 12 }}>
            验证检查
          </Typography.Text>
          <div style={{ display: "grid", gap: 6, marginTop: 8 }}>
            {task.verification.checks.map((check) => (
              <div
                key={`${check.code}-${check.status}`}
                className="vf-task-check-row"
              >
                <Space wrap>
                  <Tag
                    color={
                      check.severity === "error"
                        ? "red"
                        : check.severity === "warning"
                          ? "gold"
                          : "green"
                    }
                  >
                    {check.status}
                  </Tag>
                  <Typography.Text code style={{ fontSize: 12 }}>
                    {check.code}
                  </Typography.Text>
                  <Typography.Text style={{ fontSize: 12 }}>
                    {check.message}
                  </Typography.Text>
                  {check.duration_ms ? (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {formatDuration(check.duration_ms)}
                    </Typography.Text>
                  ) : null}
                </Space>
                {check.detail && (
                  <div style={{ marginTop: 4 }}>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {check.detail}
                    </Typography.Text>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {renderTaskManifestSummary(task)}

      {!task.error_log && task.status === "success" && (
        <div style={{ marginTop: 12 }}>
          <Typography.Text type="success" style={{ fontSize: 12 }}>
            ✓ 任务执行成功，未捕获到错误输出。
          </Typography.Text>
          {task.backup_mode === "archive" && task.artifact_name && (
            <div
              className="vf-warning-panel"
              style={{
                marginTop: 8,
                flexWrap: "wrap",
              }}
            >
              <Typography.Text strong style={{ fontSize: 12, color: "inherit" }}>
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
              {canRunRestore && <a href={taskArtifactDownloadUrl(task.id)}>
                <Button size="small" icon={<DownloadOutlined />}>
                  下载压缩包
                </Button>
              </a>}
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
          {canRunBackup && <Dropdown
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
          </Dropdown>}
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

      <ErrorPanel
        error={tasksError}
        title="无法加载任务记录"
        onRetry={() => void refetch()}
        retrying={isFetching}
      />

      <Card className="vf-filter-panel">
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
                { value: "verify", label: "验证" },
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

      <ConfirmDialog
        open={!!deleteTaskId}
        onOpenChange={(open) => !open && setDeleteTaskId(null)}
        title="确认删除任务历史"
        description="仅删除这条任务历史记录，不会删除远程存储中的备份数据。"
        confirmText="确认删除"
        variant="destructive"
        onConfirm={() => deleteTaskId && deleteMutation.mutate(deleteTaskId)}
        loading={deleteMutation.isPending}
      />

      <TaskLogDrawer task={logTask} onClose={() => setLogTask(null)} />
    </div>
  );
}

function TaskLogDrawer({ task, onClose }: { task: TaskHistory | null; onClose: () => void }) {
  const { message } = App.useApp();
  const [follow, setFollow] = useState(true);
  const active = task?.status === "pending" || task?.status === "running";
  const { data, isFetching, refetch, error } = useQuery({
    queryKey: ["task-logs", task?.id],
    queryFn: () => getTaskLogs(task!.id, { limit: 1000 }),
    enabled: !!task,
    refetchInterval: follow && active ? 2000 : false,
    retry: 1,
  });
  const lines = Array.isArray(data?.lines) ? data.lines : [];
  const status = data?.status ?? "empty";

  const copyLogs = async () => {
    try {
      await navigator.clipboard.writeText(lines.map(formatTaskLogLine).join("\n"));
      message.success("日志已复制");
    } catch (copyError: any) {
      message.error(copyError?.message || "复制日志失败");
    }
  };

  return (
    <Drawer
      className="vf-task-log-drawer"
      title={task ? `任务日志 · ${taskTypeLabel(task.type)}` : "任务日志"}
      open={!!task}
      onClose={onClose}
      size={760}
      destroyOnHidden
      extra={
        <Space className="vf-task-log-toolbar">
          <Tooltip title={follow ? "暂停日志跟随" : "继续日志跟随"}>
            <Button
              aria-label={follow ? "暂停日志跟随" : "继续日志跟随"}
              size="small"
              icon={follow ? <PauseCircleOutlined /> : <PlayCircleOutlined />}
              onClick={() => setFollow((value) => !value)}
            >
              <span className="vf-task-log-button-label">{follow ? "暂停" : "跟随"}</span>
            </Button>
          </Tooltip>
          <Tooltip title="刷新日志">
            <Button
              aria-label="刷新日志"
              size="small"
              icon={<ReloadOutlined spin={isFetching} />}
              onClick={() => void refetch()}
              disabled={isFetching}
            >
              <span className="vf-task-log-button-label">刷新</span>
            </Button>
          </Tooltip>
          <Tooltip title="复制可见日志">
            <Button
              aria-label="复制可见日志"
              size="small"
              icon={<CopyOutlined />}
              onClick={() => void copyLogs()}
              disabled={lines.length === 0}
            >
              <span className="vf-task-log-button-label">复制</span>
            </Button>
          </Tooltip>
        </Space>
      }
    >
      <ErrorPanel error={error} title="无法加载任务日志" onRetry={() => void refetch()} retrying={isFetching} />
      {data?.truncated && (
        <div style={{ margin: "12px 0" }}>
          <Tag color="orange">已丢弃 {data.dropped_lines} 行旧日志</Tag>
        </div>
      )}
      {!error && lines.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={taskLogStatusText(status)} />
      ) : lines.length > 0 ? (
        <div className="vf-task-log-viewport">
          {lines.map((line) => (
            <div key={`${line.sequence}-${line.timestamp}`} className="vf-task-log-line">
              <span className="vf-task-log-time">{formatLogTime(line.timestamp)}</span>{" "}
              <span className={line.level === "error" ? "vf-task-log-level-error" : "vf-task-log-level"}>{line.level}</span>{" "}
              <span className="vf-task-log-phase">{line.phase || "-"}</span>{" "}
              <span className="vf-task-log-stream">{line.stream || "system"}</span>{" "}
              <span>{String(line.line ?? "")}</span>
              {line.truncated ? <span className="vf-task-log-truncated"> [truncated]</span> : null}
            </div>
          ))}
        </div>
      ) : null}
    </Drawer>
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
      return renderMeasuredProgress("上传中", progress);
    }
    case "archive":
      return renderMeasuredProgress("打包中", progress);
    case "archive-upload":
      return renderMeasuredProgress("上传压缩包", progress);
    case "forget":
      return <ProgressText muted text="清理旧快照..." />;
    case "stats":
      return <ProgressText muted text="统计仓库大小..." />;
    default:
      return <ProgressText muted pulse text="处理中..." />;
  }
}

function renderMeasuredProgress(label: string, progress: BackupProgress) {
  const percent = formatProgressPercent(progress.percent_done);
  const speed = formatSpeed(progress.bytes_per_sec);
  const hasTotal = progress.total_bytes > 0;
  const summary = hasTotal
    ? `${label}: ${formatBytes(progress.bytes_done)} / ${formatBytes(progress.total_bytes)} (${percent}%)`
    : `${label}: ${formatBytes(progress.bytes_done)}`;
  return (
    <div style={{ display: "flex", flexDirection: "column", minHeight: 32, justifyContent: "center", maxWidth: 260 }}>
      <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{summary}</span>
      {speed ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {`↑${speed}`}
        </Typography.Text>
      ) : null}
      {progress.current_file ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis>
          {progress.current_file}
        </Typography.Text>
      ) : null}
    </div>
  );
}

function taskSupportsLogs(task: TaskHistory): boolean {
  return task.type === "backup" || task.type === "verify";
}

function taskTypeLabel(type: TaskHistory["type"]): string {
  if (type === "backup") return "备份";
  if (type === "verify") return "验证";
  return "恢复";
}

export function taskLogStatusText(status: TaskLogStatus): string {
  switch (status) {
    case "missing_message_id":
      return "此任务没有可关联的命令消息 ID";
    case "unsupported_agent":
      return "该节点版本暂不支持实时任务日志";
    case "expired":
      return "日志缓冲已过期";
    case "empty":
      return "暂无日志输出";
    default:
      return "暂无新增日志";
  }
}

export function formatTaskLogLine(line: TaskLogLine): string {
  const time = formatLogTime(line.timestamp);
  return `${time} ${line.level} ${line.phase || "-"} ${line.stream || "system"} ${line.line}`;
}

function formatLogTime(value: string): string {
  if (!value) return "--:--:--";
  return safeFormatDate(value, "HH:mm:ss");
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
        color: muted ? "var(--vf-text-muted)" : undefined,
        animation: pulse ? "pulse 2s infinite" : undefined,
      }}
    >
      {text}
    </div>
  );
}
