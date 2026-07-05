import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";
import { initAdmin } from "@/services/auth";

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
          maxWidth: 460,
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
            初始化管理员
          </Typography.Title>
        </div>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
          欢迎使用 云备份。请设置首个管理员账户。
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