import { useState } from "react";
import {
  App,
  Button,
  Card,
  Col,
  Drawer,
  Dropdown,
  Empty,
  Form,
  Input,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  CheckOutlined,
  DeleteOutlined,
  EditOutlined,
  EllipsisOutlined,
  MessageOutlined,
  PlusOutlined,
  SendOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createNotification,
  deleteNotification,
  listNotifications,
  testNotification,
  testNotificationConfig,
  testNotificationDraft,
  updateNotification,
} from "@/services/notifications";
import type { NotificationConfig, NotificationInput } from "@/types/notification";
import { ErrorPanel } from "@/components/error-panel";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { useAuth } from "@/contexts/auth-context";
import { permissions } from "@/services/identity";

const EVENT_OPTIONS = [
  { id: "backup_failed", label: "备份失败" },
  { id: "agent_offline", label: "节点离线" },
];

type NotificationType = NotificationInput["type"];

const DEFAULT_SUBJECT_TEMPLATE = "[云备份] {{.Title}} - {{.AgentName}}";
const DEFAULT_BODY_TEMPLATE =
  "{{.Title}}\nLevel: {{.Level}}\nAgent: {{.AgentName}}\nTime: {{.Timestamp}}\n\n{{.Body}}";

function defaultConfigForType(type: NotificationType): Record<string, unknown> {
  switch (type) {
    case "telegram":
      return { bot_token: "", chat_id: "" };
    case "webhook":
      return { url: "" };
    case "email":
      return {
        smtp_host: "",
        smtp_port: 587,
        smtp_security: "starttls",
        smtp_username: "",
        smtp_password: "",
        from: "",
        from_name: "云备份",
        to: [],
        cc: [],
        bcc: [],
        subject_template: DEFAULT_SUBJECT_TEMPLATE,
        body_template: DEFAULT_BODY_TEMPLATE,
        body_format: "text",
      };
  }
}

function configString(config: Record<string, unknown>, key: string) {
  const v = config[key];
  if (typeof v === "string") return v;
  if (typeof v === "number") return String(v);
  return "";
}

function configListText(config: Record<string, unknown>, key: string) {
  const v = config[key];
  if (Array.isArray(v))
    return v.filter((i): i is string => typeof i === "string").join("\n");
  return typeof v === "string" ? v : "";
}

function parseConfigList(value: string) {
  return value.split(/[\n,]/).map((i) => i.trim()).filter(Boolean);
}

export function NotificationsPage() {
  const { message } = App.useApp();
  const auth = useAuth();
  const canWriteNotifications = auth.hasPermission(permissions.writeNotifications);
  const queryClient = useQueryClient();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [testSuccessId, setTestSuccessId] = useState<string | null>(null);
  const [formData, setFormData] = useState<NotificationInput>({
    name: "",
    type: "telegram",
    config: defaultConfigForType("telegram"),
    events: ["backup_failed", "agent_offline"],
  });

  const { data: notifications, isLoading } = useQuery({
    queryKey: ["notifications"],
    queryFn: listNotifications,
  });

  const createMutation = useMutation({
    mutationFn: createNotification,
    onSuccess: () => {
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
      message.success("通知配置已创建");
    },
    onError: (err: any) => message.error("创建通知失败: " + err.message),
  });

  const updateMutation = useMutation({
    mutationFn: (data: NotificationInput) => updateNotification(editingId!, data),
    onSuccess: () => {
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
      message.success("通知配置已更新");
    },
    onError: (err: any) => message.error("更新通知失败: " + err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteNotification,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
      message.success("通知配置已删除");
    },
    onError: (err: any) => message.error("删除通知失败: " + err.message),
  });

  const testMutation = useMutation({
    mutationFn: testNotification,
    onSuccess: (_, id) => {
      setTestSuccessId(id);
      message.success("测试消息已发送");
      setTimeout(() => setTestSuccessId(null), 3000);
    },
    onError: (err: any) => message.error("发送测试消息失败: " + err.message),
  });

  const testConfigMutation = useMutation({
    mutationFn: () =>
      editingId
        ? testNotificationDraft(editingId, formData)
        : testNotificationConfig(formData),
    onSuccess: () => message.success("测试消息已发送"),
    onError: (err: any) => message.error("发送测试消息失败: " + err.message),
  });

  const handleEdit = (n: NotificationConfig) => {
    setEditingId(n.id);
    setFormData({
      name: n.name,
      type: n.type,
      config: n.config,
      events: n.events,
    });
    setDrawerOpen(true);
  };

  const resetForm = () => {
    setEditingId(null);
    setFormData({
      name: "",
      type: "telegram",
      config: defaultConfigForType("telegram"),
      events: ["backup_failed", "agent_offline"],
    });
    createMutation.reset();
    updateMutation.reset();
    testConfigMutation.reset();
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (editingId) updateMutation.mutate(formData);
    else createMutation.mutate(formData);
  };

  const toggleEvent = (eventId: string) => {
    setFormData((prev) => ({
      ...prev,
      events: prev.events.includes(eventId)
        ? prev.events.filter((id) => id !== eventId)
        : [...prev.events, eventId],
    }));
  };

  const updateConfig = (key: string, value: unknown) => {
    setFormData((prev) => ({
      ...prev,
      config: { ...prev.config, [key]: value },
    }));
  };

  const columns: ColumnsType<NotificationConfig> = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      render: (v: string) => <Typography.Text strong>{v}</Typography.Text>,
    },
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: "订阅事件",
      dataIndex: "events",
      key: "events",
      render: (events: string[]) => (
        <Space size={4} wrap>
          {events.map((ev) => (
            <Tag key={ev}>
              {EVENT_OPTIONS.find((o) => o.id === ev)?.label || ev}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Space>
          {canWriteNotifications && <Button
            type="text"
            icon={
              testSuccessId === record.id ? (
                <CheckOutlined style={{ color: "#2f855a" }} />
              ) : (
                <SendOutlined />
              )
            }
            onClick={() => testMutation.mutate(record.id)}
            loading={testMutation.isPending && testMutation.variables === record.id}
            title="发送测试消息"
          />}
          {canWriteNotifications && <Dropdown
            menu={{
              items: [
                {
                  key: "edit",
                  icon: <EditOutlined />,
                  label: "编辑",
                  onClick: () => handleEdit(record),
                },
                { type: "divider" },
                {
                  key: "delete",
                  icon: <DeleteOutlined />,
                  label: (
                    <Popconfirm
                      title="确认删除通知配置？"
                      description="系统将停止向此渠道发送告警。此操作不可撤销。"
                      okText="确认删除"
                      okButtonProps={{ danger: true }}
                      cancelText="取消"
                      onConfirm={() => deleteMutation.mutate(record.id)}
                    >
                      <span style={{ color: "#c53030" }}>删除</span>
                    </Popconfirm>
                  ),
                },
              ],
            }}
            trigger={["click"]}
          >
            <Button type="text" icon={<EllipsisOutlined />} />
          </Dropdown>}
        </Space>
      ),
    },
  ];

  return (
    <div className="vf-page">
      <PageHeader
        title="通知设置"
        description="Telegram / Webhook / Email"
        icon={<MessageOutlined />}
        actions={
          canWriteNotifications ? <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              resetForm();
              setDrawerOpen(true);
            }}
          >
            添加通知
          </Button> : null
        }
      />

      <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
        <Table<NotificationConfig>
          columns={columns}
          dataSource={notifications || []}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 680 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无通知配置"
              />
            ),
          }}
        />
      </Card>

      <Drawer
        title={editingId ? "编辑通知" : "添加新通知"}
        open={drawerOpen}
        onClose={() => {
          setDrawerOpen(false);
          resetForm();
        }}
        width="min(100vw, 520px)"
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
            <Row gutter={[8, 8]}>
              <Col xs={24} sm={10}>
                <Button
                  block
                  onClick={() => testConfigMutation.mutate()}
                  loading={testConfigMutation.isPending}
                  disabled={createMutation.isPending || updateMutation.isPending}
                >
                  测试当前配置
                </Button>
              </Col>
              <Col xs={24} sm={14}>
                <Button
                  type="primary"
                  block
                  onClick={handleSubmit as any}
                  loading={createMutation.isPending || updateMutation.isPending}
                >
                  {createMutation.isPending || updateMutation.isPending
                    ? "正在保存..."
                    : "保存通知配置"}
                </Button>
              </Col>
            </Row>
          </div>
        }
      >
        <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <ErrorPanel
            error={(createMutation.error || updateMutation.error) as any}
          />
          <div>
            <Typography.Text strong>名称</Typography.Text>
            <Input
              style={{ marginTop: 4 }}
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              placeholder="如: 运维 Telegram"
            />
          </div>
          <div>
            <Typography.Text strong>通知类型</Typography.Text>
            <Select
              style={{ width: "100%", marginTop: 4 }}
              value={formData.type}
              onChange={(val: NotificationType) =>
                setFormData({
                  ...formData,
                  type: val,
                  config: defaultConfigForType(val),
                })
              }
              options={[
                { value: "telegram", label: "Telegram Bot" },
                { value: "webhook", label: "Generic Webhook" },
                { value: "email", label: "Email SMTP" },
              ]}
            />
          </div>

          <div
            style={{
              border: "1px solid #f0f0f0",
              borderRadius: 6,
              padding: 12,
              background: "#fafafa",
            }}
          >
            <Typography.Text strong>渠道配置</Typography.Text>
            <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 12 }}>
              {formData.type === "telegram" && (
                <>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>Bot Token</Typography.Text>
                    <Input.Password
                      style={{ marginTop: 4 }}
                      value={configString(formData.config, "bot_token")}
                      onChange={(e) => updateConfig("bot_token", e.target.value)}
                      placeholder={
                        configString(formData.config, "bot_token") === "[redacted]"
                          ? "已加密 (输入以修改)"
                          : "123456789:ABC..."
                      }
                    />
                  </div>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>Chat ID</Typography.Text>
                    <Input
                      style={{ marginTop: 4 }}
                      value={configString(formData.config, "chat_id")}
                      onChange={(e) => updateConfig("chat_id", e.target.value)}
                      placeholder="-100..."
                    />
                  </div>
                </>
              )}

              {formData.type === "webhook" && (
                <div>
                  <Typography.Text style={{ fontSize: 13 }}>Webhook URL</Typography.Text>
                  <Input
                    style={{ marginTop: 4 }}
                    value={configString(formData.config, "url")}
                    onChange={(e) => updateConfig("url", e.target.value)}
                    placeholder="https://hooks.slack.com/..."
                  />
                </div>
              )}

              {formData.type === "email" && (
                <>
                  <Row gutter={[8, 8]}>
                    <Col xs={24} sm={14}>
                      <Typography.Text style={{ fontSize: 13 }}>SMTP 主机</Typography.Text>
                      <Input
                        style={{ marginTop: 4 }}
                        value={configString(formData.config, "smtp_host")}
                        onChange={(e) => updateConfig("smtp_host", e.target.value)}
                        placeholder="smtp.example.com"
                      />
                    </Col>
                    <Col xs={24} sm={10}>
                      <Typography.Text style={{ fontSize: 13 }}>端口</Typography.Text>
                      <Input
                        style={{ marginTop: 4 }}
                        type="number"
                        value={configString(formData.config, "smtp_port")}
                        onChange={(e) =>
                          updateConfig("smtp_port", Number(e.target.value))
                        }
                      />
                    </Col>
                  </Row>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>加密方式</Typography.Text>
                    <Select
                      style={{ width: "100%", marginTop: 4 }}
                      value={configString(formData.config, "smtp_security") || "starttls"}
                      onChange={(v) => updateConfig("smtp_security", v)}
                      options={[
                        { value: "starttls", label: "STARTTLS" },
                        { value: "tls", label: "TLS" },
                        { value: "none", label: "无" },
                      ]}
                    />
                  </div>
                  <Row gutter={[8, 8]}>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>用户名</Typography.Text>
                      <Input
                        style={{ marginTop: 4 }}
                        value={configString(formData.config, "smtp_username")}
                        onChange={(e) =>
                          updateConfig("smtp_username", e.target.value)
                        }
                        placeholder="ops@example.com"
                      />
                    </Col>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>密码</Typography.Text>
                      <Input.Password
                        style={{ marginTop: 4 }}
                        value={configString(formData.config, "smtp_password")}
                        onChange={(e) =>
                          updateConfig("smtp_password", e.target.value)
                        }
                        placeholder={
                          configString(formData.config, "smtp_password") ===
                          "[redacted]"
                            ? "已加密 (输入以修改)"
                            : "SMTP 密码"
                        }
                      />
                    </Col>
                  </Row>
                  <Row gutter={[8, 8]}>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>发件人邮箱</Typography.Text>
                      <Input
                        style={{ marginTop: 4 }}
                        type="email"
                        value={configString(formData.config, "from")}
                        onChange={(e) => updateConfig("from", e.target.value)}
                        placeholder="ops@example.com"
                      />
                    </Col>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>发件人名称</Typography.Text>
                      <Input
                        style={{ marginTop: 4 }}
                        value={configString(formData.config, "from_name")}
                        onChange={(e) =>
                          updateConfig("from_name", e.target.value)
                        }
                        placeholder="云备份"
                      />
                    </Col>
                  </Row>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>收件人</Typography.Text>
                    <Input.TextArea
                      style={{ marginTop: 4 }}
                      value={configListText(formData.config, "to")}
                      onChange={(e) =>
                        updateConfig("to", parseConfigList(e.target.value))
                      }
                      placeholder="admin@example.com"
                      rows={2}
                    />
                  </div>
                  <Row gutter={[8, 8]}>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>抄送</Typography.Text>
                      <Input.TextArea
                        style={{ marginTop: 4 }}
                        value={configListText(formData.config, "cc")}
                        onChange={(e) =>
                          updateConfig("cc", parseConfigList(e.target.value))
                        }
                        placeholder="cc@example.com"
                        rows={2}
                      />
                    </Col>
                    <Col xs={24} sm={12}>
                      <Typography.Text style={{ fontSize: 13 }}>密送</Typography.Text>
                      <Input.TextArea
                        style={{ marginTop: 4 }}
                        value={configListText(formData.config, "bcc")}
                        onChange={(e) =>
                          updateConfig("bcc", parseConfigList(e.target.value))
                        }
                        placeholder="bcc@example.com"
                        rows={2}
                      />
                    </Col>
                  </Row>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>主题模板</Typography.Text>
                    <Input
                      style={{ marginTop: 4 }}
                      value={configString(formData.config, "subject_template")}
                      onChange={(e) =>
                        updateConfig("subject_template", e.target.value)
                      }
                    />
                  </div>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>正文格式</Typography.Text>
                    <Select
                      style={{ width: "100%", marginTop: 4 }}
                      value={configString(formData.config, "body_format") || "text"}
                      onChange={(v) => updateConfig("body_format", v)}
                      options={[
                        { value: "text", label: "纯文本" },
                        { value: "html", label: "HTML" },
                      ]}
                    />
                  </div>
                  <div>
                    <Typography.Text style={{ fontSize: 13 }}>正文模板</Typography.Text>
                    <Input.TextArea
                      style={{ marginTop: 4, fontFamily: "monospace", fontSize: 12, minHeight: 140 }}
                      value={configString(formData.config, "body_template")}
                      onChange={(e) =>
                        updateConfig("body_template", e.target.value)
                      }
                      rows={6}
                    />
                  </div>
                </>
              )}
            </div>
          </div>

          <div>
            <Typography.Text strong>触发事件</Typography.Text>
            <Space direction="vertical" style={{ marginTop: 8 }}>
              {EVENT_OPTIONS.map((opt) => (
                <Switch
                  key={opt.id}
                  checkedChildren={opt.label}
                  unCheckedChildren={opt.label}
                  checked={formData.events.includes(opt.id)}
                  onChange={() => toggleEvent(opt.id)}
                />
              ))}
            </Space>
          </div>
          <button type="submit" style={{ display: "none" }} />
        </form>
      </Drawer>
    </div>
  );
}
