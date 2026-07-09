import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { LockOutlined, UserOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { login } from "@/services/auth";
import { colors } from "@/styles/theme-tokens";

interface LoginPageProps {
  onComplete: () => void;
}

interface LoginFormValues {
  username: string;
  password: string;
}

export function LoginPage({ onComplete }: LoginPageProps) {
  const [error, setError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: login,
    onSuccess: () => onComplete(),
    onError: (err: any) => setError(err.message || "登录失败"),
  });

  const handleSubmit = (values: LoginFormValues) => {
    setError(null);
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
          maxWidth: 420,
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
          请输入您的凭据以访问控制台
        </Typography.Paragraph>

        {error && (
          <Alert
            type="error"
            showIcon
            message="登录失败"
            description={error}
            style={{ marginBottom: 16 }}
          />
        )}

        <Form<LoginFormValues>
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
            <Input
              prefix={<UserOutlined />}
              placeholder="请输入用户名"
              autoComplete="username"
            />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: "请输入密码" }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="请输入密码"
              autoComplete="current-password"
            />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button
              type="primary"
              htmlType="submit"
              block
              size="large"
              loading={mutation.isPending}
            >
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
