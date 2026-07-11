import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Form, Input } from "antd";
import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { AuthShell } from "@/components/auth-shell";
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
    <AuthShell
      title="登录控制台"
      description="使用管理员凭据继续管理备份节点与任务。"
    >
      {error && (
        <Alert type="error" showIcon title="登录失败" description={error} />
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
        <Form.Item className="vf-auth-submit">
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
    </AuthShell>
  );
}
