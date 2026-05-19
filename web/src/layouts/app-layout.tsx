import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { AuthUser } from "@/types/api";
import {
  LayoutDashboard,
  Server,
  Database,
  ShieldCheck,
  History,
  Camera,
  Bell,
  Settings,
  User,
  ChevronRight,
  Menu,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { listAgents } from "@/services/agents";

interface AppLayoutProps {
  user: AuthUser;
}

const navItems = [
  { label: "仪表盘", icon: LayoutDashboard, path: "/" },
  { label: "节点管理", icon: Server, path: "/nodes" },
  { label: "存储配置", icon: Database, path: "/storage" },
  { label: "备份策略", icon: ShieldCheck, path: "/policies" },
  { label: "任务历史", icon: History, path: "/tasks" },
  { label: "快照浏览", icon: Camera, path: "/snapshots" },
  { label: "通知设置", icon: Bell, path: "/notifications" },
  { label: "系统管理", icon: Settings, path: "/system" },
];

export function AppLayout({ user }: AppLayoutProps) {
  const location = useLocation();
  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: listAgents,
  });

  const onlineCount = agents?.filter((a) => a.status === "online").length || 0;
  const totalCount = agents?.length || 0;

  const activeItem = navItems.find((item) =>
    item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path)
  );

  const sidebarContent = (
    <div className="flex h-full flex-col gap-2">
      <div className="flex h-14 items-center border-b px-6">
        <Link to="/" className="flex items-center gap-2 font-bold text-xl">
          <ShieldCheck className="h-6 w-6 text-primary" />
          <span>VaultFleet</span>
        </Link>
      </div>
      <div className="flex-1 overflow-auto py-2">
        <nav className="grid items-start px-4 text-sm font-medium">
          {navItems.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 transition-all hover:text-primary",
                  isActive ? "bg-muted text-primary" : "text-muted-foreground"
                )
              }
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </div>
    </div>
  );

  return (
    <div className="grid min-h-screen w-full lg:grid-cols-[240px_1fr]">
      <div className="hidden border-r bg-muted/40 lg:block">
        {sidebarContent}
      </div>
      <div className="flex flex-col">
        <header className="flex h-14 items-center gap-4 border-b bg-muted/40 px-6 lg:h-[60px]">
          <Sheet>
            <SheetTrigger asChild>
              <Button variant="outline" size="icon" className="shrink-0 lg:hidden">
                <Menu className="h-5 w-5" />
                <span className="sr-only">Toggle navigation menu</span>
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-[240px] p-0">
              {sidebarContent}
            </SheetContent>
          </Sheet>
          <div className="flex-1">
            <nav className="flex items-center text-sm font-medium text-muted-foreground">
              <span className="flex items-center gap-2">
                控制台
                <ChevronRight className="h-4 w-4" />
                <span className="text-foreground font-semibold">
                  {activeItem?.label || "页面"}
                </span>
              </span>
            </nav>
          </div>
          <div className="flex items-center gap-4">
            <div className="hidden md:flex items-center gap-2 text-sm text-muted-foreground mr-4">
              <div className={cn("h-2 w-2 rounded-full", onlineCount > 0 ? "bg-green-500" : "bg-red-500")} />
              <span>{onlineCount} / {totalCount} 节点在线</span>
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" className="rounded-full">
                  <User className="h-5 w-5" />
                  <span className="sr-only">Toggle user menu</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>{user.username}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link to="/system">修改密码</Link>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>
        <main className="flex-1 overflow-auto p-4 md:p-6 bg-background">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
