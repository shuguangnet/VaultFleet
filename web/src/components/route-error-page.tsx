import { Button, Result } from "antd";
import { HomeOutlined, ReloadOutlined } from "@ant-design/icons";
import { isRouteErrorResponse, useRouteError } from "react-router-dom";

export function RouteErrorPage() {
  const error = useRouteError();
  const status = isRouteErrorResponse(error) ? error.status : 500;
  const detail =
    isRouteErrorResponse(error) && typeof error.data === "string"
      ? error.data
      : "页面暂时无法显示，请刷新后重试。";

  return (
    <main className="vf-route-error">
      <Result
        status={status === 404 ? "404" : "500"}
        title={status === 404 ? "页面不存在" : "页面加载失败"}
        subTitle={detail}
        extra={[
          <Button
            key="reload"
            type="primary"
            icon={<ReloadOutlined />}
            onClick={() => window.location.reload()}
          >
            重新加载
          </Button>,
          <Button key="home" icon={<HomeOutlined />} href="/">
            返回首页
          </Button>,
        ]}
      />
    </main>
  );
}
