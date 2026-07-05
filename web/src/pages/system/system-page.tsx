import { useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Form,
  Input,
  Result,
  Row,
  Space,
  Spin,
  Tag,
  Typography,
} from "antd";
import {
  CheckCircleOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
  DownloadOutlined,
  ExclamationCircleOutlined,
  FolderOutlined,
  KeyOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import {
  changePassword,
  confirmImport,
  exportSystemData,
  getSystemVersion,
  importSystemData,
  type ImportValidationResult,
} from "@/services/system";
import { checkHealth, checkReady } from "@/services/health";
import { listAgents } from "@/services/agents";
import { downloadDiagnosticBundle } from "@/services/diagnostic";
import { StatusBadge } from "@/components/status-badge";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorPanel } from "@/components/error-panel";
import type { Agent } from "@/types/agent";

export function SystemPage() {
  const { message } = App.useApp();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [importResult, setImportResult] =
    useState<ImportValidationResult | null>(null);
  const [showImportConfirm, setShowImportConfirm] = useState(false);
  const [selectedAgents, setSelectedAgents] = useState<string[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const {
    data: healthStatus,
    refetch: refetchHealth,
    isFetching: healthFetching,
  } = useQuery({
    queryKey: ["health"],
    queryFn: checkHealth,
    refetchInterval: 30000,
  });
  const {
    data: readyStatus,
    refetch: refetchReady,
    isFetching: readyFetching,
  } = useQuery({
    queryKey: ["ready"],
    queryFn: checkReady,
    refetchInterval: 30000,
  });
  const { data: versionInfo } = useQuery({
    queryKey: ["system-version"],
    queryFn: getSystemVersion,
  });
  const { data: agents = [] } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });

  const passwordMutation = useMutation({
    mutationFn: changePassword,
    onSuccess: () => {
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      message.success("密码修改成功");
    },
    onError: (error: any) =>
      message.error("修改密码失败: " + error.message),
  });

  const exportMutation = useMutation({
    mutationFn: exportSystemData,
    onSuccess: (blob) => {
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `vaultfleet-export-${
        new Date().toISOString().split("T")[0]
      }.zip`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      message.success("数据导出成功");
    },
    onError: (error: any) =>
      message.error("数据导出失败: " + error.message),
  });

  const importMutation = useMutation({
    mutationFn: importSystemData,
    onSuccess: (result) => {
      if (result.valid) {
        setShowImportConfirm(true);
      } else {
        setImportResult(result);
        message.error(`备份文件验证失败: ${result.errors.join("；")}`);
      }
    },
    onError: (error: any) => message.error("上传失败: " + error.message),
  });

  const confirmMutation = useMutation({
    mutationFn: confirmImport,
    onSuccess: () => {
      setShowImportConfirm(false);
      message.success("导入已确认，Master 正在重启...");
      const poll = setInterval(async () => {
        try {
          const res = await fetch("/health");
          if (res.ok) {
            clearInterval(poll);
            window.location.reload();
          }
        } catch {
          /* ignore */
        }
      }, 2000);
    },
    onError: (error: any) =>
      message.error("确认导入失败: " + error.message),
  });

  const diagnosticMutation = useMutation({
    mutationFn: downloadDiagnosticBundle,
    onSuccess: (blob) => {
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `vaultfleet-diagnostic-${new Date()
        .toISOString()
        .replace(/[:.]/g, "-")
        .slice(0, 19)}.zip`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);
      message.success("诊断包已生成");
    },
    onError: (error: any) =>
      message.error("生成诊断包失败: " + error.message),
  });

  const handleImportClick = () => fileInputRef.current?.click();
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      importMutation.mutate(file);
      e.target.value = "";
    }
  };
  const handlePasswordSubmit = () => {
    if (newPassword !== confirmPassword) {
      message.error("新密码不匹配");
      return;
    }
    passwordMutation.mutate({
      current_password: currentPassword,
      new_password: newPassword,
    });
  };
  const toggleAgent = (id: string) =>
    setSelectedAgents((prev) =>
      prev.includes(id)
        ? prev.filter((x) => x !== id)
        : [...prev, id]
    );

  const isRefreshing = healthFetching || readyFetching;

  const statusItems = [
    {
      icon: <CloudServerOutlined />,
      title: "服务进程",
      subtitle: "HTTP API Server",
      ok: !!healthStatus?.ok,
    },
    {
      icon: <DatabaseOutlined />,
      title: "数据库连接",
      subtitle: "SQLite 存储",
      ok: !!readyStatus?.ok || readyStatus?.status === "ready",
    },
    {
      icon: <KeyOutlined />,
      title: "Master Key",
      subtitle: "数据加密密钥",
      ok: !!readyStatus?.ok,
    },
    {
      icon: <FolderOutlined />,
      title: "数据目录",
      subtitle: "本地存储可用性",
      ok: !!readyStatus?.ok,
    },
  ];

  return (
    <div className="vf-page" style={{ maxWidth: 1000 }}>
      <div
        className="vf-page-header"
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          系统管理
        </Typography.Title>
        <Button
          icon={<ReloadOutlined spin={isRefreshing} />}
          onClick={() => {
            refetchHealth();
            refetchReady();
          }}
          disabled={isRefreshing}
        >
          刷新状态
        </Button>
      </div>

      <Card
        title={
          <Space>
            <ThunderboltOutlined style={{ color: "#1668dc" }} />
            <Typography.Text strong>系统状态</Typography.Text>
            {versionInfo?.version && (
              <Tag style={{ fontFamily: "monospace" }}>{versionInfo.version}</Tag>
            )}
          </Space>
        }
      >
        <Row gutter={[16, 16]}>
          {statusItems.map((item) => (
            <Col xs={24} md={12} key={item.title}>
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "center",
                  padding: 12,
                  border: "1px solid #f0f0f0",
                  borderRadius: 6,
                }}
              >
                <Space>
                  <span style={{ color: "rgba(0,0,0,0.45)", fontSize: 18 }}>
                    {item.icon}
                  </span>
                  <div>
                    <div style={{ fontSize: 14, fontWeight: 500 }}>
                      {item.title}
                    </div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {item.subtitle}
                    </Typography.Text>
                  </div>
                </Space>
                <StatusBadge status={item.ok ? "success" : "failed"} />
              </div>
            </Col>
          ))}
        </Row>
        {!readyStatus?.ok && readyStatus?.error && (
          <Alert
            type="error"
            showIcon
            style={{ marginTop: 16 }}
            message={
              <span>
                <strong>系统未就绪：</strong>
                {readyStatus.error}
              </span>
            }
          />
        )}
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12}>
          <Card title="修改密码">
            <Form
              layout="vertical"
              style={{ marginTop: 0 }}
              onFinish={handlePasswordSubmit}
            >
              <ErrorPanel error={passwordMutation.error as any} />
              <Form.Item label="当前密码">
                <Input.Password
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                />
              </Form.Item>
              <Form.Item label="新密码">
                <Input.Password
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                />
              </Form.Item>
              <Form.Item label="确认新密码">
                <Input.Password
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                />
              </Form.Item>
              <Form.Item style={{ marginBottom: 0 }}>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={passwordMutation.isPending}
                >
                  提交修改
                </Button>
              </Form.Item>
            </Form>
          </Card>
        </Col>

        <Col xs={24} md={12}>
          <Card
            title="数据管理"
            styles={{ body: { display: "flex", flexDirection: "column", gap: 12 } }}
          >
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                padding: "24px 0",
                textAlign: "center",
                background: "#fafafa",
                border: "1px dashed #d9d9d9",
                borderRadius: 6,
              }}
            >
              <SafetyCertificateOutlined
                style={{ fontSize: 40, color: "rgba(0,0,0,0.25)" }}
              />
              <Typography.Paragraph
                type="secondary"
                style={{ fontSize: 12, padding: "0 16px", marginTop: 8 }}
              >
                导出的压缩包包含 SQLite 数据库和加密密钥。请务必加密存储导出的文件。
              </Typography.Paragraph>
            </div>
            {importResult && !importResult.valid && (
              <Alert
                type="error"
                showIcon
                message="验证失败"
                description={
                  <ul style={{ marginBottom: 0, paddingLeft: 16 }}>
                    {importResult.errors.map((err, i) => (
                      <li key={i}>{err}</li>
                    ))}
                  </ul>
                }
              />
            )}
            <Space className="vf-page-actions">
              <Button
                icon={<DownloadOutlined />}
                onClick={() => exportMutation.mutate()}
                loading={exportMutation.isPending}
              >
                {exportMutation.isPending ? "正在导出..." : "导出数据"}
              </Button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".zip"
                style={{ display: "none" }}
                onChange={handleFileChange}
              />
              <Button
                icon={<UploadOutlined />}
                onClick={handleImportClick}
                loading={importMutation.isPending}
              >
                {importMutation.isPending ? "正在验证..." : "导入数据"}
              </Button>
            </Space>
          </Card>
        </Col>
      </Row>

      <Card title="诊断包">
        {agents.length > 0 && (
          <>
            <Typography.Text type="secondary" style={{ display: "block", marginBottom: 8 }}>
              选择需要收集日志的 Agent（可选）：
            </Typography.Text>
            <Space direction="vertical" style={{ width: "100%" }}>
              {agents.map((agent: Agent) => (
                <Space key={agent.id}>
                  <input
                    type="checkbox"
                    checked={selectedAgents.includes(agent.id)}
                    onChange={() => toggleAgent(agent.id)}
                    disabled={
                      agent.status !== "online" || diagnosticMutation.isPending
                    }
                  />
                  <Typography.Text
                    type={
                      agent.status !== "online" ? "secondary" : undefined
                    }
                  >
                    {agent.name}
                  </Typography.Text>
                  {agent.status !== "online" && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      （离线）
                    </Typography.Text>
                  )}
                </Space>
              ))}
            </Space>
          </>
        )}
        <Button
          icon={<DownloadOutlined />}
          style={{ marginTop: 12 }}
          onClick={() => diagnosticMutation.mutate(selectedAgents)}
          loading={diagnosticMutation.isPending}
        >
          {diagnosticMutation.isPending ? "正在生成..." : "生成诊断包"}
        </Button>
      </Card>

      <ConfirmDialog
        open={showImportConfirm}
        onOpenChange={(open) => !open && setShowImportConfirm(false)}
        title="确认导入备份数据"
        description="导入将替换当前所有 Master 数据，Master 将自动重启。当前数据会保存到 rollback 目录。此操作不可撤销，是否继续？"
        confirmText="确认导入并重启"
        variant="destructive"
        onConfirm={() => confirmMutation.mutate()}
        loading={confirmMutation.isPending}
      />
    </div>
  );
}
