import { useState, useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { cancelTask, listTasks } from "@/services/tasks";
import { listAgents, backupNow } from "@/services/agents";
import type { TaskHistory } from "@/types/task";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ChevronDown, ChevronUp, XCircle, Info, RefreshCw, Play, X } from "lucide-react";
import { format } from "date-fns";
import { zhCN } from "date-fns/locale";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";

export function TasksPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [backupAgentId, setBackupAgentId] = useState<string | null>(null);
  const [cancelTaskId, setCancelTaskId] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const filters = useMemo(() => ({
    agent_id: searchParams.get("agent_id") || undefined,
    status: searchParams.get("status") || undefined,
    type: searchParams.get("type") || undefined,
    limit: 100,
  }), [searchParams]);

  const { data: tasks, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["tasks", filters],
    queryFn: () => listTasks(filters),
    refetchInterval: (query) => {
      const data = query.state.data;
      const hasActive = data?.some(
        (t) => t.status === "pending" || t.status === "running"
      );
      return hasActive ? 5000 : false;
    },
  });

  const { data: agents } = useQuery({ queryKey: ["agents"], queryFn: listAgents });

  const backupMutation = useMutation({
    mutationFn: (agentId: string) => backupNow(agentId),
    onSuccess: () => {
      setBackupAgentId(null);
      toast.success("备份命令已下发");
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => {
      toast.error("发起备份失败", { description: error.message });
    },
  });

  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => cancelTask(taskId),
    onSuccess: () => {
      setCancelTaskId(null);
      toast.success("取消命令已发送");
      queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: (error: any) => {
      toast.error("取消失败", { description: error.message });
    },
  });

  const handleFilterChange = (key: string, value: string) => {
    const newParams = new URLSearchParams(searchParams);
    if (value && value !== "all") {
      newParams.set(key, value);
    } else {
      newParams.delete(key);
    }
    setSearchParams(newParams);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">任务历史</h1>
        <div className="flex gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <Play className="mr-2 h-4 w-4" />
                手动备份
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {agents?.length === 0 ? (
                <DropdownMenuItem disabled>暂无节点</DropdownMenuItem>
              ) : (
                agents?.map((a) => (
                  <DropdownMenuItem key={a.id} onClick={() => setBackupAgentId(a.id)}>
                    {a.name}
                  </DropdownMenuItem>
                ))
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={cn("mr-2 h-4 w-4", isFetching && "animate-spin")} />
            刷新
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap gap-4 items-end bg-muted/30 p-4 rounded-lg border">
        <div className="space-y-2">
          <label className="text-xs font-medium">按节点筛选</label>
          <Select value={filters.agent_id || "all"} onValueChange={(v) => handleFilterChange("agent_id", v)}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="全部节点" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部节点</SelectItem>
              {agents?.map(a => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-2">
          <label className="text-xs font-medium">任务类型</label>
          <Select value={filters.type || "all"} onValueChange={(v) => handleFilterChange("type", v)}>
            <SelectTrigger className="w-[140px]">
              <SelectValue placeholder="全部类型" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              <SelectItem value="backup">备份</SelectItem>
              <SelectItem value="restore">恢复</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-2">
          <label className="text-xs font-medium">状态</label>
          <Select value={filters.status || "all"} onValueChange={(v) => handleFilterChange("status", v)}>
            <SelectTrigger className="w-[140px]">
              <SelectValue placeholder="全部状态" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="pending">等待中</SelectItem>
              <SelectItem value="running">运行中</SelectItem>
              <SelectItem value="success">成功</SelectItem>
              <SelectItem value="failed">失败</SelectItem>
              <SelectItem value="timeout">超时</SelectItem>
              <SelectItem value="cancelled">已取消</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10"></TableHead>
              <TableHead>节点</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>耗时 / 大小</TableHead>
              <TableHead>完成时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow><TableCell colSpan={6} className="h-24 text-center">正在加载...</TableCell></TableRow>
            ) : tasks?.length === 0 ? (
              <TableRow><TableCell colSpan={6} className="h-24 text-center text-muted-foreground">暂无符合条件的任务</TableCell></TableRow>
            ) : (
              tasks?.map((task) => (
                <>
                  <TableRow 
                    key={task.id} 
                    className={cn("group cursor-pointer hover:bg-muted/50", expandedId === task.id && "bg-muted/30")}
                    onClick={() => setExpandedId(expandedId === task.id ? null : task.id)}
                  >
                    <TableCell>
                      {expandedId === task.id ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                    </TableCell>
                    <TableCell className="font-medium">
                      {agents?.find(a => a.id === task.agent_id)?.name || task.agent_id}
                    </TableCell>
                    <TableCell>{task.type === "backup" ? "备份" : "恢复"}</TableCell>
                    <TableCell>
                      <StatusBadge status={task.status} />
                    </TableCell>
                    <TableCell className="text-xs">
                      {renderTaskMetricContent(task, setCancelTaskId)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {task.finished_at ? format(new Date(task.finished_at), "yyyy-MM-dd HH:mm:ss", { locale: zhCN }) : "-"}
                    </TableCell>
                  </TableRow>
                  {expandedId === task.id && (
                    <TableRow key={`${task.id}-detail`}>
                      <TableCell colSpan={6} className="bg-muted/10 p-0">
                        <div className="p-4 space-y-4">
                          <div className="grid grid-cols-2 md:grid-cols-3 gap-4 text-xs">
                            <div className="space-y-1">
                              <span className="text-muted-foreground">Message ID:</span>
                              <code className="block p-1 bg-muted rounded truncate">{task.message_id}</code>
                            </div>
                            {task.command_id && (
                              <div className="space-y-1">
                                <span className="text-muted-foreground">Command ID:</span>
                                <code className="block p-1 bg-muted rounded truncate">{task.command_id}</code>
                              </div>
                            )}
                            {task.snapshot_id && (
                              <div className="space-y-1">
                                <span className="text-muted-foreground">Snapshot ID:</span>
                                <code className="block p-1 bg-muted rounded truncate">{task.snapshot_id}</code>
                              </div>
                            )}
                            <div className="space-y-1">
                              <span className="text-muted-foreground">开始时间:</span>
                              <div className="p-1 bg-muted rounded">
                                {task.started_at ? format(new Date(task.started_at), "yyyy-MM-dd HH:mm:ss", { locale: zhCN }) : "-"}
                              </div>
                            </div>
                            <div className="space-y-1">
                              <span className="text-muted-foreground">结束时间:</span>
                              <div className="p-1 bg-muted rounded">
                                {task.finished_at ? format(new Date(task.finished_at), "yyyy-MM-dd HH:mm:ss", { locale: zhCN }) : "-"}
                              </div>
                            </div>
                            <div className="space-y-1">
                              <span className="text-muted-foreground">关联信息:</span>
                              <div className="flex gap-2">
                                {task.policy_id && <span className="p-1 bg-indigo-50 text-indigo-700 rounded border border-indigo-100">策略:{task.policy_id.substring(0,8)}</span>}
                                {task.storage_id && <span className="p-1 bg-slate-50 text-slate-700 rounded border border-slate-100">存储:{task.storage_id.substring(0,8)}</span>}
                                {!task.policy_id && !task.storage_id && <span className="p-1 bg-muted text-muted-foreground rounded italic">无</span>}
                              </div>
                            </div>
                          </div>
                          
                          {task.error_log && (
                            <div className="space-y-2">
                              <div className="flex items-center gap-2 text-red-600 text-xs font-bold">
                                <XCircle className="h-3 w-3" /> 错误日志
                              </div>
                              <pre className="p-3 bg-red-50 text-red-900 rounded text-xs overflow-x-auto whitespace-pre-wrap font-mono leading-relaxed max-h-[300px]">
                                {task.error_log}
                              </pre>
                            </div>
                          )}

                          {!task.error_log && task.status === "success" && (
                            <div className="flex items-center gap-2 text-green-600 text-xs">
                              <Info className="h-3 w-3" /> 任务执行成功，未捕获到错误输出。
                            </div>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )}
                </>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <ConfirmDialog
        open={!!backupAgentId}
        onOpenChange={(open) => !open && setBackupAgentId(null)}
        title="确认手动备份"
        description={`将对节点 ${agents?.find(a => a.id === backupAgentId)?.name || backupAgentId} 发起立即备份。`}
        confirmText="立即备份"
        onConfirm={() => backupAgentId && backupMutation.mutate(backupAgentId)}
        loading={backupMutation.isPending}
        variant="default"
      />

      <ConfirmDialog
        open={!!cancelTaskId}
        onOpenChange={(open) => !open && setCancelTaskId(null)}
        title="确认取消任务"
        description="确认取消此任务？取消后已上传的数据不会丢失，下次备份会继续。"
        confirmText="确认取消"
        onConfirm={() => cancelTaskId && cancelMutation.mutate(cancelTaskId)}
        loading={cancelMutation.isPending}
        variant="destructive"
      />
    </div>
  );
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes < 1024 * 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
  return `${(bytes / 1024 / 1024 / 1024 / 1024).toFixed(2)} TB`;
}

export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "";
  if (bytesPerSec < 1024) return `${bytesPerSec} B/s`;
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSec / 1024 / 1024).toFixed(1)} MB/s`;
}

export function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  if (hours > 0) return `${hours}:${pad(minutes)}:${pad(seconds)}`;
  return `${minutes}:${pad(seconds)}`;
}

export function renderTaskMetricContent(task: TaskHistory, onCancel?: (taskId: string) => void) {
  if (task.status === "pending" || task.status === "running") {
    return (
      <div className="flex items-center gap-2">
        <div className="flex-1">{renderTaskProgress(task)}</div>
        {onCancel && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onCancel(task.id);
            }}
            className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-red-50 hover:text-red-500"
            title="取消任务"
            aria-label="取消任务"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="flex min-h-8 flex-col justify-center">
      <span>{task.duration_ms ? formatDuration(task.duration_ms) : "-"}</span>
      {task.repo_size ? (
        <span className="text-muted-foreground">{formatBytes(task.repo_size)}</span>
      ) : null}
    </div>
  );
}

function formatProgressPercent(percentDone: number): number {
  return Math.round(percentDone <= 1 ? percentDone * 100 : percentDone);
}

function renderTaskProgress(task: TaskHistory) {
  const progress = task.progress;

  if (!progress) {
    return <ProgressText muted pulse text="准备中..." />;
  }

  switch (progress.phase) {
    case "init":
      return <ProgressText muted text="初始化仓库..." />;
    case "backup": {
      const percent = formatProgressPercent(progress.percent_done);
      const speed = formatSpeed(progress.bytes_per_sec);

      return (
        <div className="flex min-h-8 w-[240px] max-w-full flex-col justify-center">
          <span className="truncate">{`上传中: ${formatBytes(progress.bytes_done)} / ${formatBytes(progress.total_bytes)} (${percent}%)`}</span>
          {speed ? (
            <span className="truncate text-muted-foreground">{`↑${speed}`}</span>
          ) : null}
        </div>
      );
    }
    case "forget":
      return <ProgressText muted text="清理旧快照..." />;
    case "stats":
      return <ProgressText muted text="统计仓库大小..." />;
    default:
      return <ProgressText muted pulse text="处理中..." />;
  }
}

function ProgressText({ text, muted, pulse }: { text: string; muted?: boolean; pulse?: boolean }) {
  return (
    <div className={cn("flex min-h-8 items-center", muted && "text-muted-foreground", pulse && "animate-pulse")}>
      {text}
    </div>
  );
}
