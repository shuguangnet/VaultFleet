import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { LockOutlined, UserOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { login } from "@/services/auth";

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
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background:
          "radial-gradient(circle at 30% 20%, #1b3a6b 0%, #0f1f3d 60%, #0a1530 100%)",
        padding: 16,
      }}
    >
      <Card
        style={{
          width: "100%",
          maxWidth: 420,
          boxShadow: "0 20px 60px rgba(0, 0, 0, 0.3)",
          borderRadius: 10,
        }}
        styles={{ body: { padding: "32px 32px 24px" } }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            marginBottom: 8,
          }}
        >
          <SafetyCertificateOutlined
            style={{ fontSize: 28, color: "#1668dc" }}
          />
          <Typography.Title level={4} style={{ margin: 0 }}>
            云备份
          </Typography.Title>
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