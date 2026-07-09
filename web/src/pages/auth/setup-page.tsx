import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";
import { initAdmin } from "@/services/auth";
import { colors } from "@/styles/theme-tokens";

interface SetupPageProps {
  onComplete: () => void;
}

interface SetupFormValues {
  username: string;
  password: string;
  confirmPassword: string;
}

export function SetupPage({ onComplete }: SetupPageProps) {
  const [error, setError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: initAdmin,
    onSuccess: () => onComplete(),
    onError: (err: any) => setError(err.message || "初始化失败"),
  });

  const handleSubmit = (values: SetupFormValues) => {
    setError(null);
    if (values.password !== values.confirmPassword) {
      setError("两次输入的密码不一致");
      return;
    }
    mutation.mutate({ username: values.username, password: values.password });
  };

  return (
    <div
      className="vf-login-backdrop"
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 16,
      }}
    >
      <Card
        style={{
          width: "100%",
          maxWidth: 460,
        }}
        styles={{ body: { padding: "32px 32px 24px" } }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 12,
            marginBottom: 8,
          }}
        >
          <span
            style={{
              display: "inline-flex",
              width: 40,
              height: 40,
              alignItems: "center",
              justifyContent: "center",
              color: "#e2e8f0",
              background: `linear-gradient(135deg, ${colors.primary} 0%, ${colors.info} 100%)`,
              borderRadius: 10,
              fontSize: 22,
            }}
          >
            <SafetyCertificateOutlined />
          </span>
          <div>
            <Typography.Title level={4} style={{ margin: 0, fontWeight: 700 }}>
              VaultFleet
            </Typography.Title>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              企业云备份控制台
            </Typography.Text>
          </div>
        </div>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
          欢迎使用 VaultFleet。请设置首个管理员账户。
        </Typography.Paragraph>

        {error && (
          <Alert
            type="error"
            showIcon
            message="错误"
            description={error}
            style={{ marginBottom: 16 }}
          />
        )}

        <Form<SetupFormValues>
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{ username: "admin" }}
          requiredMark={false}
        >
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, message: "请输入用户名" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: "请输入密码" }]}
          >
            <Input.Password />
          </Form.Item>
          <Form.Item
            label="确认密码"
            name="confirmPassword"
            rules={[{ required: true, message: "请再次输入密码" }]}
          >
            <Input.Password />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button
              type="primary"
              htmlType="submit"
              block
              size="large"
              loading={mutation.isPending}
            >
              完成设置
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
