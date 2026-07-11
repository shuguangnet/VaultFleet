import { lazy, Suspense, useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  Col,
  Empty,
  Row,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleTwoTone,
  CloudServerOutlined,
  DatabaseOutlined,
  HistoryOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  SyncOutlined,
  ThunderboltTwoTone,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import type { EChartsOption } from "echarts";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import "dayjs/locale/zh-cn";
import { StatusBadge } from "@/components/status-badge";
import { checkReady } from "@/services/health";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listStorage } from "@/services/storage";
import { listTasks } from "@/services/tasks";
import { chartColors, colors } from "@/styles/theme-tokens";
import type { TaskHistory } from "@/types/task";
import { useColorMode } from "@/contexts/theme-context";

dayjs.extend(relativeTime);
dayjs.locale("zh-cn");

const EChart = lazy(() =>
  import("@/components/e-chart").then((module) => ({ default: module.EChart }))
);

interface DashboardTaskRow {
  id: string;
  agent_id: string;
  agent_name: string;
  type: string;
  status: string;
  created_at: string;
}

const taskStatusMeta: Record<
  TaskHistory["status"],
  { label: string; color: string }
> = {
  pending: { label: "等待中", color: chartColors.warning },
  running: { label: "运行中", color: chartColors.primary },
  success: { label: "成功", color: chartColors.success },
  failed: { label: "失败", color: chartColors.error },
  timeout: { label: "超时", color: "#9a5b13" },
  cancelled: { label: "已取消", color: chartColors.slate },
};

export function DashboardPage() {
  const { mode } = useColorMode();
  const chartTheme = useMemo(
    () =>
      mode === "dark"
        ? {
            text: "#edf3f7",
            textSecondary: "#a8b5c1",
            textTertiary: "#7f8d9a",
            border: "#31404f",
            borderSecondary: "#253240",
          }
        : colors,
    [mode]
  );
  const { data: agents, isLoading: agentsLoading } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });
  const { data: policies, isLoading: policiesLoading } = useQuery({
    queryKey: ["policies"],
    queryFn: () => listPolicies(),
  });
  const { data: storageList, isLoading: storageLoading } = useQuery({
    queryKey: ["storage"],
    queryFn: listStorage,
  });
  const { data: tasks, isLoading: tasksLoading } = useQuery({
    queryKey: ["tasks", { limit: 200 }],
    queryFn: () => listTasks({ limit: 200 }),
  });
  const { data: readyStatus } = useQuery({
    queryKey: ["ready"],
    queryFn: checkReady,
    refetchInterval: 30000,
  });

  const agentsList = agents ?? [];
  const policiesList = policies ?? [];
  const storageConfigs = storageList ?? [];
  const tasksList = tasks ?? [];

  const onlineNodes = agentsList.filter((a) => a.status === "online").length;
  const offlineNodes = agentsList.filter((a) => a.status === "offline").length;
  const syncedPolicies = policiesList.filter((p) => p.synced).length;
  const unsyncedPolicies = policiesList.filter((p) => !p.synced).length;
  const activeTasks = tasksList.filter(
    (t) => t.status === "pending" || t.status === "running"
  ).length;

  const last24h = dayjs().subtract(24, "hour");
  const recentTasks = tasksList.filter((t) => dayjs(t.created_at).isAfter(last24h));
  const successCount = recentTasks.filter((t) => t.status === "success").length;
  const failedCount = recentTasks.filter((t) => t.status === "failed").length;
  const successRate =
    recentTasks.length > 0 ? Math.round((successCount / recentTasks.length) * 100) : 0;

  const latestTasks = tasksList.slice(0, 10);

  const lastRefreshed = useMemo(() => dayjs().format("HH:mm:ss"), [tasksList.length]);

  const taskTrendOption = useMemo<EChartsOption>(() => {
    const days = Array.from({ length: 7 }, (_, index) =>
      dayjs().subtract(6 - index, "day")
    );
    const labels = days.map((day) => day.format("MM-DD"));
    const totals = days.map(
      (day) => tasksList.filter((task) => dayjs(task.created_at).isSame(day, "day")).length
    );
    const successes = days.map(
      (day) =>
        tasksList.filter(
          (task) =>
            task.status === "success" && dayjs(task.created_at).isSame(day, "day")
        ).length
    );
    const failures = days.map(
      (day) =>
        tasksList.filter(
          (task) =>
            (task.status === "failed" || task.status === "timeout") &&
            dayjs(task.created_at).isSame(day, "day")
        ).length
    );

    return {
      color: [chartColors.primary, chartColors.success, chartColors.error],
      tooltip: { trigger: "axis" },
      legend: {
        top: 0,
        right: 0,
        itemWidth: 10,
        itemHeight: 10,
        textStyle: { color: chartTheme.textSecondary },
      },
      grid: { top: 42, right: 18, bottom: 24, left: 34 },
      xAxis: {
        type: "category",
        boundaryGap: false,
        data: labels,
        axisLine: { lineStyle: { color: chartTheme.border } },
        axisTick: { show: false },
        axisLabel: { color: chartTheme.textTertiary },
      },
      yAxis: {
        type: "value",
        minInterval: 1,
        splitLine: { lineStyle: { color: chartTheme.borderSecondary } },
        axisLabel: { color: chartTheme.textTertiary },
      },
      series: [
        {
          name: "任务总数",
          type: "line",
          smooth: true,
          symbolSize: 6,
          lineStyle: { width: 2 },
          areaStyle: {
            opacity: 0.12,
            color: {
              type: "linear",
              x: 0,
              y: 0,
              x2: 0,
              y2: 1,
              colorStops: [
                { offset: 0, color: chartColors.primary },
                { offset: 1, color: "rgba(15, 76, 129, 0.05)" },
              ],
            },
          },
          data: totals,
        },
        {
          name: "成功",
          type: "line",
          smooth: true,
          symbolSize: 6,
          lineStyle: { width: 2 },
          data: successes,
        },
        {
          name: "失败/超时",
          type: "line",
          smooth: true,
          symbolSize: 6,
          lineStyle: { width: 2 },
          data: failures,
        },
      ],
    };
  }, [chartTheme, tasksList]);

  const taskStatusOption = useMemo<EChartsOption>(() => {
    const data = Object.entries(taskStatusMeta)
      .map(([status, meta]) => ({
        name: meta.label,
        value: tasksList.filter((task) => task.status === status).length,
        itemStyle: { color: meta.color },
      }))
      .filter((item) => item.value > 0);

    return {
      title:
        data.length === 0
          ? {
              text: "暂无任务",
              left: "center",
              top: "42%",
              textStyle: { color: chartTheme.textTertiary, fontSize: 14 },
            }
          : undefined,
      tooltip: { trigger: "item" },
      legend: {
        bottom: 0,
        left: "center",
        itemWidth: 10,
        itemHeight: 10,
        textStyle: { color: chartTheme.textSecondary },
      },
      series: [
        {
          name: "任务状态",
          type: "pie",
          radius: ["52%", "72%"],
          center: ["50%", "42%"],
          avoidLabelOverlap: true,
          label: {
            formatter: "{b}\n{c}",
            color: chartTheme.text,
          },
          labelLine: { length: 10, length2: 8 },
          data,
        },
      ],
    };
  }, [chartTheme, tasksList]);

  const assetHealthOption = useMemo<EChartsOption>(() => {
    const categories = ["在线节点", "离线节点", "已同步策略", "待同步策略", "存储配置"];
    const values = [
      onlineNodes,
      offlineNodes,
      syncedPolicies,
      unsyncedPolicies,
      storageConfigs.length,
    ];

    return {
      color: [
        chartColors.success,
        chartColors.error,
        chartColors.primary,
        chartColors.warning,
        chartColors.cyan,
      ],
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
      grid: { top: 8, right: 22, bottom: 24, left: 78 },
      xAxis: {
        type: "value",
        minInterval: 1,
        splitLine: { lineStyle: { color: chartTheme.borderSecondary } },
        axisLabel: { color: chartTheme.textTertiary },
      },
      yAxis: {
        type: "category",
        data: categories,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: chartTheme.border } },
        axisLabel: { color: chartTheme.text },
      },
      series: [
        {
          type: "bar",
          barWidth: 14,
          data: values.map((value, index) => ({
            value,
            itemStyle: {
              borderRadius: [0, 6, 6, 0],
              color: [
                chartColors.success,
                chartColors.error,
                chartColors.primary,
                chartColors.warning,
                chartColors.cyan,
              ][index],
            },
          })),
        },
      ],
    };
  }, [
    chartTheme,
    offlineNodes,
    onlineNodes,
    storageConfigs.length,
    syncedPolicies,
    unsyncedPolicies,
  ]);

  const storageTypeOption = useMemo<EChartsOption>(() => {
    const counts = storageConfigs.reduce<Record<string, number>>((acc, item) => {
      acc[item.rclone_type] = (acc[item.rclone_type] ?? 0) + 1;
      return acc;
    }, {});
    const data = Object.entries(counts).map(([name, value], index) => ({
      name,
      value,
      itemStyle: {
        color: [
          chartColors.primary,
          chartColors.cyan,
          chartColors.purple,
          chartColors.warning,
          chartColors.success,
        ][index % 5],
      },
    }));

    return {
      title:
        data.length === 0
          ? {
              text: "暂无存储",
              left: "center",
              top: "42%",
              textStyle: { color: chartTheme.textTertiary, fontSize: 14 },
            }
          : undefined,
      tooltip: { trigger: "item" },
      legend: {
        bottom: 0,
        left: "center",
        itemWidth: 10,
        itemHeight: 10,
        textStyle: { color: chartTheme.textSecondary },
      },
      series: [
        {
          name: "存储类型",
          type: "pie",
          radius: ["0%", "68%"],
          center: ["50%", "42%"],
          label: { formatter: "{b}: {c}", color: chartTheme.text },
          data,
        },
      ],
    };
  }, [chartTheme, storageConfigs]);

  const columns: ColumnsType<DashboardTaskRow> = [
    {
      title: "节点",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (text, record) => record.agent_name || record.agent_id,
    },
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      render: (t: string) => <Tag>{t === "backup" ? "备份" : "恢复"}</Tag>,
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
    agent_name: agentsList.find((a) => a.id === t.agent_id)?.name || t.agent_id,
    type: t.type,
    status: t.status,
    created_at: t.created_at,
  }));

  return (
    <div className="vf-page vf-dashboard">
      <section className="vf-dashboard-hero">
        <div>
          <Typography.Text className="vf-dashboard-eyebrow">
            云备份运维
          </Typography.Text>
          <Typography.Title level={3} className="vf-dashboard-title">
            控制台总览
          </Typography.Title>
          <Typography.Text className="vf-dashboard-subtitle">
            集中查看节点健康、策略同步、存储配置与备份任务执行趋势。
          </Typography.Text>
          <Typography.Text style={{ display: "block", marginTop: 10, color: "rgba(255,255,255,0.55)", fontSize: 12 }}>
            最后刷新：{lastRefreshed}
          </Typography.Text>
        </div>
        <Space wrap className="vf-dashboard-hero-actions">
          <Tag
            className="vf-readiness-tag"
            color={readyStatus?.ok ? "success" : "error"}
            icon={
              readyStatus?.ok ? (
                <CheckCircleOutlined />
              ) : (
                <CloseCircleTwoTone twoToneColor={chartColors.error} />
              )
            }
          >
            {readyStatus?.ok ? "系统已就绪" : readyStatus?.error || "系统未就绪"}
          </Tag>
          <Link to="/tasks">
            <Button type="primary" icon={<HistoryOutlined />}>
              查看任务
            </Button>
          </Link>
          <Link to="/nodes">
            <Button icon={<PlusOutlined />}>接入节点</Button>
          </Link>
        </Space>
      </section>

      <Row gutter={[20, 20]}>
        <Col xs={24} sm={12} lg={8} xl={6}>
          <MetricCard
            label="系统状态"
            value={readyStatus?.ok ? "运行正常" : "未就绪"}
            tone={readyStatus?.ok ? "success" : "danger"}
            icon={
              <ThunderboltTwoTone
                twoToneColor={readyStatus?.ok ? chartColors.success : chartColors.error}
              />
            }
            footer={readyStatus?.ok ? "健康检查通过" : readyStatus?.error || "无法连接服务器"}
          />
        </Col>
        <Col xs={24} sm={12} lg={8} xl={6}>
          <MetricCard
            label="节点在线"
            value={`${onlineNodes}/${agentsList.length}`}
            tone="success"
            icon={<CloudServerOutlined />}
            footer={`${offlineNodes} 个节点离线`}
          />
        </Col>
        <Col xs={24} sm={12} lg={8} xl={6}>
          <MetricCard
            label="策略同步"
            value={`${syncedPolicies}/${policiesList.length}`}
            tone={unsyncedPolicies > 0 ? "warning" : "primary"}
            icon={<SafetyCertificateOutlined />}
            footer={`${unsyncedPolicies} 条策略待同步`}
          />
        </Col>
        <Col xs={24} sm={12} lg={8} xl={6}>
          <MetricCard
            label="24h 成功率"
            value={`${successRate}%`}
            tone={failedCount > 0 ? "warning" : "primary"}
            icon={<SyncOutlined />}
            footer={`${successCount} 成功 / ${failedCount} 失败 / ${activeTasks} 进行中`}
          />
        </Col>
      </Row>

      <Row gutter={[20, 20]}>
        <Col xs={24} xl={14}>
          <Card
            className="vf-dashboard-card"
            title="近 7 天任务趋势"
            extra={<Typography.Text type="secondary">最近 200 条任务</Typography.Text>}
          >
            <DashboardChart height={320}>
              <EChart option={taskTrendOption} loading={tasksLoading} height={320} />
            </DashboardChart>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={5}>
          <Card className="vf-dashboard-card" title="任务状态分布">
            <DashboardChart height={320}>
              <EChart option={taskStatusOption} loading={tasksLoading} height={320} />
            </DashboardChart>
          </Card>
        </Col>
        <Col xs={24} md={12} xl={5}>
          <Card className="vf-dashboard-card" title="存储类型">
            <DashboardChart height={320}>
              <EChart option={storageTypeOption} loading={storageLoading} height={320} />
            </DashboardChart>
          </Card>
        </Col>
      </Row>

      <Row gutter={[20, 20]}>
        <Col xs={24} lg={14}>
          <Card className="vf-dashboard-card" title="资产健康视图">
            <DashboardChart height={292}>
              <EChart
                option={assetHealthOption}
                loading={agentsLoading || policiesLoading || storageLoading}
                height={292}
              />
            </DashboardChart>
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card className="vf-dashboard-card vf-action-card" title="快捷操作">
            <div className="vf-action-grid">
              <DashboardAction
                icon={<PlusOutlined />}
                title="接入节点"
                description="生成安装令牌并绑定新的 Agent"
                to="/nodes"
              />
              <DashboardAction
                icon={<DatabaseOutlined />}
                title="配置存储"
                description="维护 S3、SFTP、WebDAV 等备份目标"
                to="/storage"
              />
              <DashboardAction
                icon={<SafetyCertificateOutlined />}
                title="创建策略"
                description="配置目录、周期和保留规则"
                to="/policies"
              />
              <DashboardAction
                icon={<HistoryOutlined />}
                title="任务审计"
                description="追踪备份与恢复执行记录"
                to="/tasks"
              />
            </div>
          </Card>
        </Col>
      </Row>

      <Card
        className="vf-table-card vf-dashboard-card"
        title="最近任务"
        extra={
          <Link to="/tasks">
            <Button type="link" size="small">
              查看全部
            </Button>
          </Link>
        }
        styles={{ body: { padding: 0 } }}
      >
        <Table<DashboardTaskRow>
          columns={columns}
          dataSource={tableData}
          rowKey="id"
          pagination={false}
          scroll={{ x: 640 }}
          size="middle"
          loading={tasksLoading}
          locale={{
            emptyText: (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无任务记录" />
            ),
          }}
        />
      </Card>
    </div>
  );
}

function DashboardChart({ children, height }: { children: ReactNode; height: number }) {
  return (
    <Suspense
      fallback={
        <div className="vf-chart-skeleton" style={{ height }} aria-label="图表加载中">
          <span className="vf-chart-skeleton-bar" />
          <span className="vf-chart-skeleton-bar" />
          <span className="vf-chart-skeleton-bar" />
          <span className="vf-chart-skeleton-bar" />
        </div>
      }
    >
      {children}
    </Suspense>
  );
}

function MetricCard({
  label,
  value,
  footer,
  icon,
  tone,
}: {
  label: string;
  value: string;
  footer: string;
  icon: ReactNode;
  tone: "primary" | "success" | "warning" | "danger";
}) {
  return (
    <Card className={`vf-metric-card vf-metric-card-${tone}`}>
      <div className="vf-metric-header">
        <Typography.Text className="vf-metric-label">{label}</Typography.Text>
        <span className="vf-metric-icon">{icon}</span>
      </div>
      <Typography.Title level={3} className="vf-metric-value">
        {value}
      </Typography.Title>
      <Typography.Text className="vf-metric-footer" ellipsis>
        {footer}
      </Typography.Text>
    </Card>
  );
}

function DashboardAction({
  icon,
  title,
  description,
  to,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  to: string;
}) {
  return (
    <Link to={to} className="vf-dashboard-action">
      <span className="vf-dashboard-action-icon">{icon}</span>
      <span>
        <Typography.Text strong className="vf-dashboard-action-title">
          {title}
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {description}
        </Typography.Text>
      </span>
    </Link>
  );
}
