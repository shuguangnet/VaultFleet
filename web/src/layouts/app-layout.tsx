import { useEffect, useMemo, useState } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Avatar,
  Badge,
  Breadcrumb,
  Drawer,
  Grid,
  Dropdown,
  Layout,
  Menu,
  Space,
  Tooltip,
  Typography,
} from "antd";
import {
  BellOutlined,
  CameraOutlined,
  DatabaseOutlined,
  DashboardOutlined,
  DesktopOutlined,
  HistoryOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import { listAgents } from "@/services/agents";
import { logout } from "@/services/auth";
import { antdTheme } from "@/styles/antd-theme";
import type { AuthUser } from "@/types/api";
import { AuthProvider } from "@/contexts/auth-context";

const { Header, Sider, Content } = Layout;
const { useBreakpoint } = Grid;

interface AppLayoutProps {
  user: AuthUser;
}

interface NavItem {
  key: string;
  label: string;
  icon: React.ReactNode;
  path: string;
}

const navItems: NavItem[] = [
  { key: "/", label: "仪表盘", icon: <DashboardOutlined />, path: "/" },
  { key: "/nodes", label: "节点管理", icon: <DesktopOutlined />, path: "/nodes" },
  { key: "/storage", label: "存储配置", icon: <DatabaseOutlined />, path: "/storage" },
  { key: "/policies", label: "备份策略", icon: <SafetyCertificateOutlined />, path: "/policies" },
  { key: "/tasks", label: "任务历史", icon: <HistoryOutlined />, path: "/tasks" },
  { key: "/snapshots", label: "快照浏览", icon: <CameraOutlined />, path: "/snapshots" },
  { key: "/notifications", label: "通知设置", icon: <BellOutlined />, path: "/notifications" },
  { key: "/system", label: "系统管理", icon: <SettingOutlined />, path: "/system" },
];

function findActiveLabel(pathname: string): string {
  const exact = navItems.find((item) => item.path === pathname);
  if (exact) return exact.label;
  const matched = navItems
    .filter((item) => item.path !== "/" && pathname.startsWith(item.path))
    .sort((a, b) => b.path.length - a.path.length)[0];
  return matched?.label ?? "页面";
}

export function AppLayout({ user }: AppLayoutProps) {
  const location = useLocation();
  const queryClient = useQueryClient();
  const screens = useBreakpoint();
  const isMobile = !screens.md;
  const [collapsed, setCollapsed] = useState(false);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });

  const onlineCount = agents?.filter((a) => a.status === "online").length || 0;
  const totalCount = agents?.length || 0;

  const activeLabel = useMemo(
    () => findActiveLabel(location.pathname),
    [location.pathname]
  );

  const menuItems: MenuProps["items"] = navItems.map((item) => ({
    key: item.path,
    icon: item.icon,
    label: <Link to={item.path}>{item.label}</Link>,
  }));

  const selectedKey = useMemo(() => {
    const matched = navItems
      .filter((item) =>
        item.path === "/"
          ? location.pathname === "/"
          : location.pathname.startsWith(item.path)
      )
      .sort((a, b) => b.path.length - a.path.length)[0];
    return matched?.path ?? "/";
  }, [location.pathname]);

  const userMenuItems: MenuProps["items"] = [
    { key: "username", label: `${user.username} (${user.role ?? "admin"})`, disabled: true },
    { type: "divider" },
    {
      key: "change-password",
      label: <Link to="/system">修改密码</Link>,
    },
    { type: "divider" },
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "退出登录",
      onClick: () => {
        logout().finally(() => {
          queryClient.clear();
          window.location.href = "/login";
        });
      },
    },
  ];

  useEffect(() => {
    setMobileNavOpen(false);
  }, [location.pathname]);

  useEffect(() => {
    if (!isMobile) setMobileNavOpen(false);
  }, [isMobile]);

  const brandClassName = [
    "vf-app-brand",
    collapsed && !isMobile ? "vf-app-brand-collapsed" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const brand = (
    <div
      className={brandClassName}
    >
      <span className="vf-app-brand-icon">
        <SafetyCertificateOutlined />
      </span>
      {(!collapsed || isMobile) && (
        <span className="vf-app-brand-copy">
          <span className="vf-app-brand-name">VaultFleet</span>
          <span className="vf-app-brand-subtitle">云备份控制台</span>
        </span>
      )}
    </div>
  );

  return (
    <AuthProvider user={user}>
    <Layout className="vf-app-shell">
      {!isMobile && (
        <Sider
          collapsible
          collapsed={collapsed}
          onCollapse={setCollapsed}
          width={220}
          collapsedWidth={64}
          trigger={null}
          theme="dark"
          style={{
            position: "sticky",
            top: 0,
            height: "100vh",
            overflow: "auto",
          }}
        >
          {brand}
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[selectedKey]}
            items={menuItems}
            style={{
              marginTop: 8,
              borderInlineEnd: "none",
            }}
          />
        </Sider>
      )}

      <Drawer
        title={null}
        placement="left"
        open={mobileNavOpen}
        onClose={() => setMobileNavOpen(false)}
        width="min(86vw, 320px)"
        className="vf-mobile-nav-drawer"
        styles={{
          body: { padding: 0, background: "#172033" },
          header: { display: "none" },
        }}
      >
        {brand}
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={() => setMobileNavOpen(false)}
          style={{
            marginTop: 8,
            borderInlineEnd: "none",
            background: "transparent",
          }}
        />
      </Drawer>

      <Layout>
        <Header
          className="vf-app-header"
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "0 20px",
            background: "#ffffff",
            borderBottom: "1px solid #e7ebf0",
            position: "sticky",
            top: 0,
            zIndex: 10,
          }}
        >
          <Space size="middle" align="center">
            <button
              type="button"
              onClick={() =>
                isMobile
                  ? setMobileNavOpen(true)
                  : setCollapsed((c) => !c)
              }
              aria-label={isMobile ? "打开导航" : "切换侧栏"}
              className="vf-icon-button"
              style={{
                background: "transparent",
                border: "none",
                cursor: "pointer",
                fontSize: 16,
                color: "rgba(0,0,0,0.65)",
              }}
            >
              {isMobile || collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            </button>
            <Breadcrumb
              className="vf-app-breadcrumb"
              items={[
                ...(!isMobile ? [{ title: "控制台" }] : []),
                { title: <strong>{activeLabel}</strong> },
              ]}
            />
          </Space>

          <Space size={isMobile ? "middle" : "large"} align="center">
            <Tooltip title={`${onlineCount} / ${totalCount} 节点在线`}>
              <Space size={6}>
                <Badge
                  status={onlineCount > 0 ? "success" : "error"}
                  showZero
                />
                <Typography.Text className="vf-header-online-text" style={{ fontSize: 13 }}>
                  {onlineCount} / {totalCount} 节点在线
                </Typography.Text>
              </Space>
            </Tooltip>

            <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
              <Space style={{ cursor: "pointer" }}>
                <Avatar
                  size={32}
                  icon={<UserOutlined />}
                  style={{ background: antdTheme.token?.colorPrimary }}
                />
                <Typography.Text className="vf-header-username" style={{ fontSize: 13 }}>
                  {user.username}
                </Typography.Text>
              </Space>
            </Dropdown>
          </Space>
        </Header>

        <Content
          className="vf-app-content"
          style={{
            background: "#f6f7f9",
            overflow: "auto",
          }}
        >
          <Outlet />
        </Content>
      </Layout>
    </Layout>
    </AuthProvider>
  );
}

// 重新导出 ReloadOutlined 以便其他位置使用
export { ReloadOutlined };
