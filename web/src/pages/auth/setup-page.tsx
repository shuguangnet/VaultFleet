import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Form, Input } from "antd";
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from "@ant-design/icons";
import { AuthShell } from "@/components/auth-shell";
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
    <AuthShell
      title="初始化管理员"
      description="创建首个管理员账户，完成后即可进入控制台。"
      width="wide"
    >
      {error && <Alert type="error" showIcon message="设置失败" description={error} />}

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
          <Input
            prefix={<UserOutlined />}
            placeholder="请输入管理员用户名"
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
            placeholder="请输入管理员密码"
            autoComplete="new-password"
          />
        </Form.Item>
        <Form.Item
          label="确认密码"
          name="confirmPassword"
          rules={[{ required: true, message: "请再次输入密码" }]}
        >
          <Input.Password
            prefix={<SafetyCertificateOutlined />}
            placeholder="请再次输入密码"
            autoComplete="new-password"
          />
        </Form.Item>
        <Form.Item className="vf-auth-submit">
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
    </AuthShell>
  );
}
