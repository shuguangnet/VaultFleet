import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listAgents, createAgent, deleteAgent, regenerateAgentToken } from "@/services/agents";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Plus, Search, MoreHorizontal, RefreshCw, Trash2, ExternalLink } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { InstallCommand } from "@/components/install-command";
import { Link } from "react-router-dom";
import { format } from "date-fns";
import { zhCN } from "date-fns/locale";

export function NodesPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [isAddDrawerOpen, setIsAddDrawerOpen] = useState(false);
  const [newNodeName, setNewNodeName] = useState("");
  const [enrollToken, setEnrollToken] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmConfirmDeleteId] = useState<string | null>(null);
  const [confirmRegenId, setConfirmRegenId] = useState<string | null>(null);

  const { data: agents, isLoading } = useQuery({ queryKey: ["agents"], queryFn: listAgents });

  const createMutation = useMutation({
    mutationFn: createAgent,
    onSuccess: (data) => {
      setEnrollToken(data.enroll_token);
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteAgent,
    onSuccess: () => {
      setConfirmConfirmDeleteId(null);
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const regenMutation = useMutation({
    mutationFn: regenerateAgentToken,
    onSuccess: (data) => {
      setEnrollToken(data.enroll_token);
      setConfirmRegenId(null);
      setIsAddDrawerOpen(true); // Re-open drawer to show new token
    },
  });

  const filteredAgents = agents?.filter((a) => a.name.toLowerCase().includes(search.toLowerCase())) || [];

  const handleAddNode = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate({ name: newNodeName });
  };

  const handleDrawerClose = (open: boolean) => {
    setIsAddDrawerOpen(open);
    if (!open) {
      setEnrollToken(null);
      setNewNodeName("");
      createMutation.reset();
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">节点管理</h1>
        <Sheet open={isAddDrawerOpen} onOpenChange={handleDrawerClose}>
          <SheetTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> 添加节点
            </Button>
          </SheetTrigger>
          <SheetContent className="sm:max-w-md">
            <SheetHeader>
              <SheetTitle>{enrollToken ? "安装指令" : "添加新节点"}</SheetTitle>
              <SheetDescription>
                {enrollToken ? "在您的服务器上运行以下命令以完成部署。" : "输入节点名称以生成安装 Token。"}
              </SheetDescription>
            </SheetHeader>
            <div className="py-6">
              {enrollToken ? (
                <InstallCommand enrollToken={enrollToken} />
              ) : (
                <form onSubmit={handleAddNode} className="space-y-4">
                  <div className="space-y-2">
                    <Input
                      placeholder="节点名称 (如: production-db-1)"
                      value={newNodeName}
                      onChange={(e) => setNewNodeName(e.target.value)}
                      required
                    />
                  </div>
                  <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                    {createMutation.isPending ? "正在生成..." : "生成安装 Token"}
                  </Button>
                </form>
              )}
            </div>
          </SheetContent>
        </Sheet>
      </div>

      <div className="flex items-center gap-2">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="搜索节点名称..."
            className="pl-8"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>最后在线</TableHead>
              <TableHead className="hidden md:table-cell">系统信息</TableHead>
              <TableHead className="hidden lg:table-cell">创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center">正在加载...</TableCell>
              </TableRow>
            ) : filteredAgents.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  未找到匹配的节点
                </TableCell>
              </TableRow>
            ) : (
              filteredAgents.map((agent) => (
                <TableRow key={agent.id}>
                  <TableCell className="font-medium">
                    <Link to={`/nodes/${agent.id}`} className="hover:underline flex items-center gap-1">
                      {agent.name}
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                    </Link>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={agent.status} />
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {agent.last_seen ? format(new Date(agent.last_seen), "yyyy-MM-dd HH:mm:ss", { locale: zhCN }) : "从未在线"}
                  </TableCell>
                  <TableCell className="hidden md:table-cell text-xs">
                    <div className="flex flex-col">
                      <span>{agent.os} / {agent.arch}</span>
                      <span className="text-muted-foreground">v{agent.version}</span>
                    </div>
                  </TableCell>
                  <TableCell className="hidden lg:table-cell text-xs text-muted-foreground">
                    {format(new Date(agent.created_at), "yyyy-MM-dd", { locale: zhCN })}
                  </TableCell>
                  <TableCell className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon">
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem asChild>
                          <Link to={`/nodes/${agent.id}`}>详情</Link>
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setConfirmRegenId(agent.id)}>
                          <RefreshCw className="mr-2 h-4 w-4" /> 重新生成 Token
                        </DropdownMenuItem>
                        <DropdownMenuItem className="text-red-600" onClick={() => setConfirmConfirmDeleteId(agent.id)}>
                          <Trash2 className="mr-2 h-4 w-4" /> 删除
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <ConfirmDialog
        open={!!confirmDeleteId}
        onOpenChange={(open) => !open && setConfirmConfirmDeleteId(null)}
        title="确认删除节点？"
        description="此操作将永久删除该节点及其所有关联策略。此操作不可撤销。"
        onConfirm={() => confirmDeleteId && deleteMutation.mutate(confirmDeleteId)}
        loading={deleteMutation.isPending}
      />

      <ConfirmDialog
        open={!!confirmRegenId}
        onOpenChange={(open) => !open && setConfirmRegenId(null)}
        title="确认重新生成 Token？"
        description="重新生成后，原有的安装 Token 将立即失效。您需要重新部署该节点。"
        confirmText="重新生成"
        variant="default"
        onConfirm={() => confirmRegenId && regenMutation.mutate(confirmRegenId)}
        loading={regenMutation.isPending}
      />
    </div>
  );
}
