import { useState } from "react";
import {
  Button,
  Card,
  Drawer,
  Dropdown,
  Empty,
  Input,
  Popconfirm,
  Space,
  Spin,
  Table,
  Typography,
} from "antd";
import {
  CopyOutlined,
  CheckOutlined,
  EllipsisOutlined,
  LinkOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  CodeOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { Link } from "react-router-dom";
import {safeFormatDate} from "@/lib/date";
import {
  createAgent,
  deleteAgent,
  getInstallToken,
  listAgents,
  regenerateAgentToken,
} from "@/services/agents";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { InstallCommand } from "@/components/install-command";
import { StatusBadge } from "@/components/status-badge";
import { App } from "antd";

type Agent = Awaited<ReturnType<typeof listAgents>>[number];

export function NodesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [newNodeName, setNewNodeName] = useState("");
  const [enrollToken, setEnrollToken] = useState<string | null>(null);
  const [installCommandAgent, setInstallCommandAgent] = useState<
    { id: string; token: string } | null
  >(null);

  const { data: agents, isLoading } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
    refetchInterval: 10000,
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

  const filtered = agents?.filter((a) =>
    a.name.toLowerCase().includes(search.toLowerCase())
  ) || [];

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
            <LinkOutlined style={{ color: "rgba(0,0,0,0.45)" }} />
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
              {
                key: "install",
                icon: <CodeOutlined />,
                label: "安装指令",
                onClick: () => handleShowInstallCommand(record.id),
              },
              { type: "divider" },
              {
                key: "delete",
                icon: <WarningOutlined style={{ color: "#ff4d4f" }} />,
                label: (
                  <Popconfirm
                    title="确认删除节点？"
                    description="此操作将永久删除该节点及其所有关联策略。此操作不可撤销。"
                    okText="确认删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => deleteMutation.mutate(record.id)}
                  >
                    <span style={{ color: "#ff4d4f" }}>删除</span>
                  </Popconfirm>
                ),
              },
            ],
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
      <div
        className="vf-page-header"
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          节点管理
        </Typography.Title>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setDrawerOpen(true)}
        >
          添加节点
        </Button>
      </div>

      <Input
        className="vf-mobile-full"
        allowClear
        placeholder="搜索节点名称..."
        prefix={<SearchOutlined />}
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        style={{ maxWidth: 320 }}
      />

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

      <Drawer
        title={enrollToken ? "安装指令" : "添加新节点"}
        open={drawerOpen}
        onClose={closeDrawer}
        width="min(100vw, 480px)"
        destroyOnClose
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
    </div>
  );
}
