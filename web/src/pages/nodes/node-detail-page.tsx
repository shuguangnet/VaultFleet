import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getAgent } from "@/services/agents";
import { listPolicies } from "@/services/policies";
import { listTasks } from "@/services/tasks";
import { listSnapshots } from "@/services/snapshots";
import { StatusBadge } from "@/components/status-badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { format } from "date-fns";
import { zhCN } from "date-fns/locale";
import { Skeleton } from "@/components/ui/skeleton";

export function NodeDetailPage() {
  const { agentId } = useParams<{ agentId: string }>();

  const { data: agent, isLoading: agentLoading } = useQuery({
    queryKey: ["agent", agentId],
    queryFn: () => getAgent(agentId!),
    enabled: !!agentId,
  });

  const { data: policies } = useQuery({
    queryKey: ["policies", { agent_id: agentId }],
    queryFn: () => listPolicies(agentId),
    enabled: !!agentId,
  });

  const { data: snapshots } = useQuery({
    queryKey: ["snapshots", agentId],
    queryFn: () => listSnapshots(agentId!),
    enabled: !!agentId,
  });

  const { data: tasks } = useQuery({
    queryKey: ["tasks", { agent_id: agentId }],
    queryFn: () => listTasks({ agent_id: agentId }),
    enabled: !!agentId,
  });

  if (agentLoading) {
    return <div className="space-y-4"><Skeleton className="h-12 w-full" /><Skeleton className="h-[400px] w-full" /></div>;
  }

  if (!agent) {
    return <div>节点未找到</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">{agent.name}</h1>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <StatusBadge status={agent.status} />
            <span>ID: {agent.id}</span>
          </div>
        </div>
      </div>

      <Tabs defaultValue="overview" className="w-full">
        <TabsList className="grid w-full grid-cols-5 lg:w-[600px]">
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="policy">策略</TabsTrigger>
          <TabsTrigger value="snapshots">快照</TabsTrigger>
          <TabsTrigger value="tasks">任务</TabsTrigger>
          <TabsTrigger value="browser">文件浏览</TabsTrigger>
        </TabsList>
        
        <TabsContent value="overview" className="mt-6 space-y-4">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium">系统信息</CardTitle>
              </CardHeader>
              <CardContent className="text-sm space-y-2">
                <div className="flex justify-between"><span className="text-muted-foreground">操作系统</span><span>{agent.os}</span></div>
                <div className="flex justify-between"><span className="text-muted-foreground">架构</span><span>{agent.arch}</span></div>
                <div className="flex justify-between"><span className="text-muted-foreground">主机名</span><span>{agent.hostname}</span></div>
                <div className="flex justify-between"><span className="text-muted-foreground">Agent 版本</span><span>v{agent.version}</span></div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-medium">连接状态</CardTitle>
              </CardHeader>
              <CardContent className="text-sm space-y-2">
                <div className="flex justify-between"><span className="text-muted-foreground">当前状态</span><StatusBadge status={agent.status} /></div>
                <div className="flex justify-between"><span className="text-muted-foreground">最后在线</span><span>{agent.last_seen ? format(new Date(agent.last_seen), "yyyy-MM-dd HH:mm:ss", { locale: zhCN }) : "从未"}</span></div>
                <div className="flex justify-between"><span className="text-muted-foreground">创建时间</span><span>{format(new Date(agent.created_at), "yyyy-MM-dd HH:mm:ss", { locale: zhCN })}</span></div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="policy" className="mt-6">
          <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>调度</TableHead>
                  <TableHead>备份路径</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {policies?.map((policy) => (
                  <TableRow key={policy.id}>
                    <TableCell className="font-mono text-xs">{policy.schedule}</TableCell>
                    <TableCell className="text-xs truncate max-w-[200px]">{policy.backup_dirs.join(", ")}</TableCell>
                    <TableCell><StatusBadge status={policy.synced ? "success" : "unsynced"} /></TableCell>
                    <TableCell className="text-right">
                      <Link to={`/policies?id=${policy.id}`} className="text-primary hover:underline text-sm">详情</Link>
                    </TableCell>
                  </TableRow>
                )) || <TableRow><TableCell colSpan={4} className="text-center py-4">无关联策略</TableCell></TableRow>}
              </TableBody>
            </Table>
          </Card>
        </TabsContent>

        <TabsContent value="snapshots" className="mt-6">
           <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>时间</TableHead>
                  <TableHead>路径</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {snapshots?.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-mono text-xs">{s.id.substring(0, 8)}</TableCell>
                    <TableCell className="text-xs">{format(new Date(s.time), "yyyy-MM-dd HH:mm:ss", { locale: zhCN })}</TableCell>
                    <TableCell className="text-xs truncate max-w-[300px]">{s.paths.join(", ")}</TableCell>
                    <TableCell className="text-right text-sm text-primary cursor-pointer">恢复</TableCell>
                  </TableRow>
                )) || <TableRow><TableCell colSpan={4} className="text-center py-4">暂无快照</TableCell></TableRow>}
              </TableBody>
            </Table>
          </Card>
        </TabsContent>

        <TabsContent value="tasks" className="mt-6">
           <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>类型</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>时间</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks?.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell>{t.type === "backup" ? "备份" : "恢复"}</TableCell>
                    <TableCell><StatusBadge status={t.status} /></TableCell>
                    <TableCell className="text-xs text-muted-foreground">{format(new Date(t.created_at), "yyyy-MM-dd HH:mm:ss", { locale: zhCN })}</TableCell>
                    <TableCell className="text-right text-sm text-primary cursor-pointer">详情</TableCell>
                  </TableRow>
                )) || <TableRow><TableCell colSpan={4} className="text-center py-4">暂无任务</TableCell></TableRow>}
              </TableBody>
            </Table>
          </Card>
        </TabsContent>

        <TabsContent value="browser" className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>文件浏览器</CardTitle>
              <CardDescription>实时浏览 Agent 上的目录</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="h-64 flex items-center justify-center text-muted-foreground border-2 border-dashed rounded-lg">
                Directory Browser 正在开发中...
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
