import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Row,
  Spin,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  CheckCircleTwoTone,
  CloseCircleTwoTone,
  DatabaseOutlined,
  HistoryOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  ThunderboltTwoTone,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import "dayjs/locale/zh-cn";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listTasks } from "@/services/tasks";
import { checkReady } from "@/services/health";
import { StatusBadge } from "@/components/status-badge";

dayjs.extend(relativeTime);
dayjs.locale("zh-cn");

interface DashboardTaskRow {
  id: string;
  agent_id: string;
  agent_name: string;
  type: string;
  status: string;
  created_at: string;
}

export function DashboardPage() {
  const { data: agents } = useQuery({ queryKey: ["agents"], queryFn: listAgents });
  const { data: policies } = useQuery({
    queryKey: ["policies"],
    queryFn: () => listPolicies(),
  });
  const { data: tasks } = useQuery({
    queryKey: ["tasks", { limit: 200 }],
    queryFn: () => listTasks({ limit: 200 }),
  });
  const { data: readyStatus } = useQuery({
    queryKey: ["ready"],
    queryFn: checkReady,
    refetchInterval: 30000,
  });

  const onlineNodes = agents?.filter((a) => a.status === "online").length || 0;
  const offlineNodes = agents?.filter((a) => a.status === "offline").length || 0;
  const syncedPolicies = policies?.filter((p) => p.synced).length || 0;
  const unsyncedPolicies = policies?.filter((p) => !p.synced).length || 0;

  const last24h = dayjs().subtract(24, "hour");
  const recentTasks =
    tasks?.filter((t) => dayjs(t.created_at).isAfter(last24h)) || [];
  const successCount = recentTasks.filter((t) => t.status === "success").length;
  const failedCount = recentTasks.filter((t) => t.status === "failed").length;

  const latestTasks = tasks?.slice(0, 10) || [];

  const columns: ColumnsType<DashboardTaskRow> = [
    {
      title: "节点",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (text, record) =>
        record.agent_name || record.agent_id,
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
      title: "开始时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {dayjs(v).fromNow()}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Link to={`/tasks?agent_id=${record.agent_id}`}>
          <Button type="link" size="small">
            详情
          </Button>
        </Link>
      ),
    },
  ];

  const tableData: DashboardTaskRow[] = latestTasks.map((t) => ({
    id: t.id,
    agent_id: t.agent_id,
    agent_name:
      agents?.find((a) => a.id === t.agent_id)?.name || t.agent_id,
    type: t.type,
    status: t.status,
    created_at: t.created_at,
  }));

  return (
    <div className="vf-page" style={{ gap: 20 }}>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={8} xl={6}>
          <Card styles={{ body: { padding: 20 } }}>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: 12,
              }}
            >
              <Typography.Text type="secondary">系统状态</Typography.Text>
              {readyStatus?.ok ? (
                <ThunderboltTwoTone twoToneColor="#52c41a" style={{ fontSize: 18 }} />
              ) : (
                <ThunderboltTwoTone twoToneColor="#ff4d4f" style={{ fontSize: 18 }} />
              )}
            </div>
            {readyStatus?.ok ? (
              <>
                <Typography.Title level={3} style={{ color: "#52c41a", margin: 0 }}>
                  运行正常
                </Typography.Title>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  系统已就绪
                </Typography.Text>
              </>
            ) : (
              <>
                <Typography.Title level={3} style={{ color: "#ff4d4f", margin: 0 }}>
                  未就绪
                </Typography.Title>
                <Typography.Text
                  type="danger"
                  ellipsis
                  style={{ fontSize: 12, maxWidth: "100%" }}
                >
                  {readyStatus?.error || "无法连接服务器"}
                </Typography.Text>
              </>
            )}
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={8} xl={6}>
          <Card styles={{ body: { padding: 20 } }}>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: 12,
              }}
            >
              <Typography.Text type="secondary">节点状态</Typography.Text>
              <DatabaseOutlined style={{ fontSize: 18, color: "rgba(0,0,0,0.45)" }} />
            </div>
            <Typography.Title level={3} style={{ color: "#52c41a", margin: 0 }}>
              {onlineNodes}{" "}
              <Typography.Text type="secondary" style={{ fontSize: 14 }}>
                在线
              </Typography.Text>
            </Typography.Title>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {offlineNodes} 节点离线
            </Typography.Text>
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={8} xl={6}>
          <Card styles={{ body: { padding: 20 } }}>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: 12,
              }}
            >
              <Typography.Text type="secondary">策略同步</Typography.Text>
              <SafetyCertificateOutlined
                style={{ fontSize: 18, color: "rgba(0,0,0,0.45)" }}
              />
            </div>
            <Typography.Title level={3} style={{ margin: 0 }}>
              {syncedPolicies}{" "}
              <Typography.Text type="secondary" style={{ fontSize: 14 }}>
                已同步
              </Typography.Text>
            </Typography.Title>
            <Typography.Text type="warning" style={{ fontSize: 12 }}>
              {unsyncedPolicies} 策略待同步
            </Typography.Text>
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={8} xl={6}>
          <Card styles={{ body: { padding: 20 } }}>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: 12,
              }}
            >
              <Typography.Text type="secondary">24h 任务</Typography.Text>
              <HistoryOutlined style={{ fontSize: 18, color: "rgba(0,0,0,0.45)" }} />
            </div>
            <div style={{ display: "flex", gap: 16, alignItems: "baseline" }}>
              <span>
                <CheckCircleTwoTone twoToneColor="#52c41a" style={{ marginRight: 4 }} />
                <Typography.Text strong style={{ fontSize: 20 }}>
                  {successCount}
                </Typography.Text>
              </span>
              <span>
                <CloseCircleTwoTone twoToneColor="#ff4d4f" style={{ marginRight: 4 }} />
                <Typography.Text strong style={{ fontSize: 20, color: "#ff4d4f" }}>
                  {failedCount}
                </Typography.Text>
              </span>
            </div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              成功 / 失败 · 24h
            </Typography.Text>
          </Card>
        </Col>
      </Row>

      <div>
        <Typography.Title level={5} style={{ marginBottom: 12 }}>
          快捷操作
        </Typography.Title>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <Link to="/nodes">
            <Button icon={<PlusOutlined />}>添加节点</Button>
          </Link>
          <Link to="/storage">
            <Button icon={<DatabaseOutlined />}>添加存储</Button>
          </Link>
          <Link to="/policies">
            <Button icon={<SafetyCertificateOutlined />}>创建策略</Button>
          </Link>
        </div>
      </div>

      <div>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginBottom: 12,
          }}
        >
          <Typography.Title level={5} style={{ margin: 0 }}>
            最近任务
          </Typography.Title>
          <Link to="/tasks">
            <Button type="link" size="small">
              查看全部
            </Button>
          </Link>
        </div>
        <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
          <Table<DashboardTaskRow>
            columns={columns}
            dataSource={tableData}
            rowKey="id"
            pagination={false}
            scroll={{ x: 640 }}
            size="middle"
            locale={{
              emptyText: (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description="暂无任务记录"
                />
              ),
            }}
          />
        </Card>
      </div>
    </div>
  );
}
