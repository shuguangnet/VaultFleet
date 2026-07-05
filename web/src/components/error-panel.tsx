import { Alert } from "antd";
import { ExclamationCircleOutlined } from "@ant-design/icons";

interface ErrorPanelProps {
  error: string | null;
  title?: string;
}

export function ErrorPanel({
  error,
  title = "出错了",
}: ErrorPanelProps) {
  if (!error) return null;
  return (
    <Alert
      type="error"
      showIcon
      icon={<ExclamationCircleOutlined />}
      message={title}
      description={error}
      style={{ marginBottom: 16 }}
    />
  );
}