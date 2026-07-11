import { Alert, Button } from "antd";
import { ExclamationCircleOutlined, ReloadOutlined } from "@ant-design/icons";

interface ErrorPanelProps {
  error: unknown;
  title?: string;
  onRetry?: () => void;
  retrying?: boolean;
}

export function ErrorPanel({
  error,
  title = "出错了",
  onRetry,
  retrying = false,
}: ErrorPanelProps) {
  if (!error) return null;
  const description =
    typeof error === "string"
      ? error
      : error instanceof Error
        ? error.message
        : "请求未能完成，请稍后重试。";
  return (
    <Alert
      type="error"
      showIcon
      icon={<ExclamationCircleOutlined />}
      title={title}
      description={description}
      action={
        onRetry ? (
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={retrying}
            onClick={onRetry}
          >
            重新加载
          </Button>
        ) : undefined
      }
      className="vf-error-panel"
    />
  );
}
