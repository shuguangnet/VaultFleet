import { useQuery } from "@tanstack/react-query";
import { listAgents } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listTasks } from "@/services/tasks";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { StatusBadge } from "@/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Server, ShieldCheck, CheckCircle2, XCircle, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { zhCN } from "date-fns/locale";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";

export function DashboardPage() {
  const { data: agents } = useQuery({ queryKey: ["agents"], queryFn: listAgents });
  const { data: policies } = useQuery({ queryKey: ["policies"], queryFn: () => listPolicies() });
  const { data: tasks } = useQuery({ queryKey: ["tasks", { limit: 200 }], queryFn: () => listTasks({ limit: 200 }) });

  const onlineNodes = agents?.filter((a) => a.status === "online").length || 0;
  const offlineNodes = agents?.filter((a) => a.status === "offline").length || 0;
  
  const syncedPolicies = policies?.filter((p) => p.synced).length || 0;
  const unsyncedPolicies = policies?.filter((p) => !p.synced).length || 0;

  const last24h = new Date(Date.now() - 24 * 60 * 60 * 1000);
  const recentTasks = tasks?.filter((t) => new Date(t.created_at) > last24h) || [];
  const successCount = recentTasks.filter((t) => t.status === "success").length;
  const failedCount = recentTasks.filter((t) => t.status === "failed").length;

  const latestTasks = tasks?.slice(0, 10) || [];

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">节点状态</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">{onlineNodes} <span className="text-muted-foreground font-normal text-base">在线</span></div>
            <p className="text-xs text-muted-foreground mt-1">{offlineNodes} 节点离线</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">策略同步</CardTitle>
            <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{syncedPolicies} <span className="text-muted-foreground font-normal text-base">已同步</span></div>
            <p className="text-xs text-amber-600 mt-1">{unsyncedPolicies} 策略待同步</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">24h 成功任务</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">{successCount}</div>
            <p className="text-xs text-muted-foreground mt-1">最近 24 小时</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">24h 失败任务</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600">{failedCount}</div>
            <p className="text-xs text-muted-foreground mt-1">最近 24 小时</p>
          </CardContent>
        </Card>
      </div>

      <div className="flex items-center justify-between">
        <h2 className="text-lg font-bold">最近任务</h2>
        <Button variant="outline" size="sm" asChild>
          <Link to="/tasks">查看全部</Link>
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>节点</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>开始时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {latestTasks.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="h-24 text-center text-muted-foreground">
                  暂无任务记录
                </TableCell>
              </TableRow>
            ) : (
              latestTasks.map((task) => (
                <TableRow key={task.id}>
                  <TableCell className="font-medium">
                    {agents?.find((a) => a.id === task.agent_id)?.name || task.agent_id}
                  </TableCell>
                  <TableCell>{task.type === "backup" ? "备份" : "恢复"}</TableCell>
                  <TableCell>
                    <StatusBadge status={task.status} />
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {formatDistanceToNow(new Date(task.created_at), { addSuffix: true, locale: zhCN })}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="sm" asChild>
                      <Link to={`/tasks?agent_id=${task.agent_id}`}>详情</Link>
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
