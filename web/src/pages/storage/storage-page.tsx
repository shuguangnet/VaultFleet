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
  Spin,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  CheckCircleTwoTone,
  CloseCircleTwoTone,
  DeleteOutlined,
  EditOutlined,
  EllipsisOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createStorage,
  deleteStorage,
  listProviders,
  listStorage,
  testSavedStorage,
  testUnsavedStorage,
  updateStorage,
} from "@/services/storage";
import type { StorageConfig, StorageInput } from "@/types/storage";
import { ErrorPanel } from "@/components/error-panel";
import { KeyValueEditor } from "@/components/key-value-editor";
import { ConfirmDialog } from "@/components/confirm-dialog";
import dayjs from "dayjs";
import { safeFormatDate } from "@/lib/date";
import type { StorageTestResult } from "@/types/health";

export const STORAGE_TEMPLATES: Record<
  string,
  {
    name: string;
    defaults: Record<string, string>;
    fields: { key: string; label: string; type?: string }[];
  }
> = {
  s3: {
    name: "Amazon S3 / 兼容对象存储",
    defaults: { provider: "AWS", region: "us-east-1" },
    fields: [
      { key: "provider", label: "Provider (提供商)" },
      { key: "endpoint", label: "Endpoint (服务地址)" },
      { key: "bucket", label: "Bucket (存储桶)" },
      { key: "access_key_id", label: "Access Key ID (访问密钥)" },
      { key: "secret_access_key", label: "Secret Access Key (秘密密钥)", type: "password" },
      { key: "region", label: "Region (区域)" },
    ],
  },
  webdav: {
    name: "WebDAV",
    defaults: { vendor: "other" },
    fields: [
      { key: "url", label: "URL" },
      { key: "user", label: "用户名" },
      { key: "pass", label: "密码", type: "password" },
    ],
  },
  swift: {
    name: "OpenStack Swift",
    defaults: { auth_version: "3", domain: "default", tenant_domain: "default" },
    fields: [
      { key: "auth", label: "Auth URL (认证地址)" },
      { key: "user", label: "User (用户名)" },
      { key: "key", label: "Key (密码 / API Key)", type: "password" },
      { key: "tenant", label: "Project / Tenant (项目)" },
      { key: "domain", label: "User Domain (用户域)" },
      { key: "tenant_domain", label: "Project Domain (项目域)" },
      { key: "auth_version", label: "Auth Version (认证版本)" },
      { key: "region", label: "Region (区域)" },
      { key: "container", label: "Storage Container (存储容器)" },
    ],
  },
  sftp: {
    name: "SFTP",
    defaults: {},
    fields: [
      { key: "host", label: "主机地址" },
      { key: "user", label: "用户名" },
      { key: "pass", label: "密码", type: "password" },
      { key: "port", label: "端口 (默认 22)" },
    ],
  },
  local: {
    name: "本地路径",
    defaults: {},
    fields: [],
  },
};

interface StorageFormValues {
  name: string;
  rclone_type: string;
  rclone_config: Record<string, string>;
}

export function StoragePage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<StorageTestResult | null>(null);
  const [form] = Form.useForm<StorageFormValues>();

  const { data: storageList, isLoading } = useQuery({
    queryKey: ["storage"],
    queryFn: listStorage,
  });
  const { data: s3Providers } = useQuery({
    queryKey: ["s3-providers"],
    queryFn: listProviders,
  });

  const openAddDrawer = () => {
    setEditingId(null);
    setTestResult(null);
    form.resetFields();
    form.setFieldsValue({
      name: "",
      rclone_type: "s3",
      rclone_config: STORAGE_TEMPLATES.s3.defaults,
    });
    setDrawerOpen(true);
  };

  const openEditDrawer = (storage: StorageConfig) => {
    setEditingId(storage.id);
    setTestResult(null);
    form.setFieldsValue({
      name: storage.name,
      rclone_type: storage.rclone_type,
      rclone_config: storage.rclone_config,
    });
    setDrawerOpen(true);
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setEditingId(null);
  };

  const testMutation = useMutation({
    mutationFn: (body: {
      rclone_type: string;
      rclone_config: Record<string, string>;
    }) => testUnsavedStorage(body),
    onSuccess: (data) => {
      setTestResult(data);
      if (data.ok) {
        message.success(`连接成功 (${data.latency_ms}ms)`);
      } else {
        message.error(`连接失败: ${data.error}`);
      }
    },
    onError: (error: any) => message.error(`测试请求失败: ${error.message}`),
  });

  const listTestMutation = useMutation({
    mutationFn: (id: string) => testSavedStorage(id),
    onSuccess: (data) => {
      if (data.ok) {
        message.success(`连接成功 (${data.latency_ms}ms)`);
      } else {
        message.error(`连接失败: ${data.error}`);
      }
    },
    onError: (error: any) => message.error(`测试请求失败: ${error.message}`),
  });

  const createMutation = useMutation({
    mutationFn: createStorage,
    onSuccess: () => {
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
      message.success("存储配置已创建");
    },
    onError: (error: any) => message.error("创建存储失败", error.message),
  });

  const updateMutation = useMutation({
    mutationFn: (data: StorageInput) => updateStorage(editingId!, data),
    onSuccess: () => {
      setDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
      message.success("存储配置已更新");
    },
    onError: (error: any) => message.error("更新存储失败", error.message),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteStorage,
    onSuccess: () => {
      setConfirmDeleteId(null);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
      message.success("存储配置已删除");
    },
    onError: (error: any) => message.error("删除存储失败", error.message),
  });

  const handleSubmit = (values: StorageFormValues) => {
    const payload: StorageInput = {
      name: values.name,
      rclone_type: values.rclone_type,
      rclone_config: values.rclone_config,
    };
    if (editingId) {
      updateMutation.mutate(payload);
    } else {
      createMutation.mutate(payload);
    }
  };

  const handleTest = () => {
    const values = form.getFieldsValue();
    testMutation.mutate({
      rclone_type: values.rclone_type,
      rclone_config: values.rclone_config,
    });
  };

  const columns: ColumnsType<StorageConfig> = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      render: (v: string) => <Typography.Text strong>{v}</Typography.Text>,
    },
    {
      title: "类型",
      dataIndex: "rclone_type",
      key: "rclone_type",
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      responsive: ["md"],
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {safeFormatDate(v, "yyyy-MM-dd HH:mm")}
        </Typography.Text>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "right",
      render: (_, record) => (
        <Space>
          <Button
            size="small"
            icon={<ThunderboltOutlined />}
            onClick={() => listTestMutation.mutate(record.id)}
            loading={listTestMutation.isPending && listTestMutation.variables === record.id}
          >
            测试
          </Button>
          <Dropdown
            menu={{
              items: [
                {
                  key: "edit",
                  icon: <EditOutlined />,
                  label: "编辑",
                  onClick: () => openEditDrawer(record),
                },
                { type: "divider" },
                {
                  key: "delete",
                  icon: <DeleteOutlined />,
                  label: (
                    <span style={{ color: "#ff4d4f" }}>删除</span>
                  ),
                  onClick: () => setConfirmDeleteId(record.id),
                },
              ],
            }}
            trigger={["click"]}
          >
            <Button type="text" icon={<EllipsisOutlined />} size="small" />
          </Dropdown>
        </Space>
      ),
    },
  ];

  const currentTemplate =
    STORAGE_TEMPLATES[Form.useWatch("rclone_type", form) || "s3"];

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
          存储配置
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={openAddDrawer}>
          添加存储
        </Button>
      </div>

      <Card className="vf-table-card" styles={{ body: { padding: 0 } }}>
        <Table<StorageConfig>
          columns={columns}
          dataSource={storageList || []}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 560 }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无存储配置"
              />
            ),
          }}
        />
      </Card>

      <Drawer
        title={editingId ? "编辑存储" : "添加新存储"}
        open={drawerOpen}
        onClose={closeDrawer}
        width="min(100vw, 540px)"
        destroyOnClose
        footer={
          <div
            className="vf-drawer-footer"
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              gap: 8,
              padding: "10px 16px",
              background: "#fff",
              borderTop: "1px solid #f0f0f0",
            }}
          >
            <Space>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                连接测试:
              </Typography.Text>
              {testMutation.isPending ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  <Spin size="small" /> 测试中...
                </Typography.Text>
              ) : testResult ? (
                testResult.ok ? (
                  <Typography.Text type="success" style={{ fontSize: 12 }}>
                    <CheckCircleTwoTone twoToneColor="#52c41a" /> 通过 (
                    {testResult.latency_ms}ms)
                  </Typography.Text>
                ) : (
                  <Tooltip title={testResult.error}>
                    <Typography.Text type="danger" style={{ fontSize: 12 }}>
                      <CloseCircleTwoTone twoToneColor="#ff4d4f" /> 失败
                    </Typography.Text>
                  </Tooltip>
                )
              ) : (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  尚未测试
                </Typography.Text>
              )}
            </Space>
            <Space>
              <Button onClick={handleTest} loading={testMutation.isPending}>
                测试连接
              </Button>
              <Button
                type="primary"
                onClick={() => form.submit()}
                loading={createMutation.isPending || updateMutation.isPending}
              >
                保存配置
              </Button>
            </Space>
          </div>
        }
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            name: "",
            rclone_type: "s3",
            rclone_config: STORAGE_TEMPLATES.s3.defaults,
          }}
        >
          <ErrorPanel
            error={(createMutation.error || updateMutation.error) as any}
          />
          <Form.Item
            label="名称"
            name="name"
            rules={[{ required: true, message: "请输入名称" }]}
          >
            <Input placeholder="如: Production-S3" />
          </Form.Item>
          <Form.Item label="存储类型" name="rclone_type">
            <Select
              onChange={(val) => {
                form.setFieldValue(
                  "rclone_config",
                  STORAGE_TEMPLATES[val]?.defaults || {}
                );
              }}
              options={[
                ...Object.entries(STORAGE_TEMPLATES).map(([k, v]) => ({
                  value: k,
                  label: v.name,
                })),
                { value: "other", label: "其他 (手动配置)" },
              ]}
            />
          </Form.Item>
          <Tabs
            defaultActiveKey="template"
            items={[
              {
                key: "template",
                label: "模版模式",
                children: currentTemplate ? (
                  currentTemplate.fields.length > 0 ? (
                    <Row gutter={[12, 12]}>
                      {currentTemplate.fields.map((f) => (
                        <Col span={24} key={f.key}>
                          <Form.Item label={f.label}>
                            {f.key === "provider" &&
                            Form.useWatch("rclone_type", form) === "s3" &&
                            s3Providers &&
                            s3Providers.length > 0 ? (
                              <Select
                                value={form.getFieldValue("rclone_config")?.[f.key] || ""}
                                onChange={(val) => {
                                  const cfg = form.getFieldValue("rclone_config");
                                  form.setFieldValue("rclone_config", {
                                    ...cfg,
                                    [f.key]: val,
                                  });
                                }}
                                options={s3Providers.map((p: any) => ({
                                  value: p.value,
                                  label: p.help
                                    ? `${p.value} — ${p.help}`
                                    : p.value,
                                }))}
                              />
                            ) : (
                              <Input
                                type={f.type || "text"}
                                value={form.getFieldValue("rclone_config")?.[f.key] || ""}
                                onChange={(e) => {
                                  const cfg = form.getFieldValue("rclone_config");
                                  form.setFieldValue("rclone_config", {
                                    ...cfg,
                                    [f.key]: e.target.value,
                                  });
                                }}
                                placeholder={
                                  form.getFieldValue("rclone_config")?.[f.key] ===
                                  "[redacted]"
                                    ? "已加密 (输入以修改)"
                                    : ""
                                }
                              />
                            )}
                          </Form.Item>
                        </Col>
                      ))}
                    </Row>
                  ) : (
                    <Typography.Text type="secondary">
                      此类型无需额外字段配置。
                    </Typography.Text>
                  )
                ) : (
                  <Typography.Text type="secondary">
                    请切换到高级模式进行手动配置。
                  </Typography.Text>
                ),
              },
              {
                key: "advanced",
                label: "高级模式",
                children: (
                  <Form.Item name="rclone_config">
                    <KeyValueEditorWrapper />
                  </Form.Item>
                ),
              },
            ]}
          />
        </Form>
      </Drawer>

      <ConfirmDialog
        open={!!confirmDeleteId}
        onOpenChange={(open) => !open && setConfirmDeleteId(null)}
        title="确认删除存储配置？"
        description="如果已有策略正在使用此存储，删除将导致备份失败。此操作不可撤销。"
        onConfirm={() =>
          confirmDeleteId && deleteMutation.mutate(confirmDeleteId)
        }
        loading={deleteMutation.isPending}
      />
    </div>
  );
}

// 包装 KeyValueEditor 以适配 Form.Item 的 value/onChange 协议
function KeyValueEditorWrapper({
  value,
  onChange,
}: {
  value?: Record<string, string>;
  onChange?: (val: Record<string, string>) => void;
}) {
  return <KeyValueEditor value={value || {}} onChange={onChange || (() => {})} />;
}
