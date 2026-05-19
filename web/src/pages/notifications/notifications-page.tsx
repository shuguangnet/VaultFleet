import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listNotifications,
  createNotification,
  updateNotification,
  deleteNotification,
  testNotification,
} from "@/services/notifications";
import { NotificationConfig, NotificationInput } from "@/types/notification";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Plus, Bell, Settings2, Trash2, MoreHorizontal, Send, Check } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger, SheetFooter } from "@/components/ui/sheet";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorPanel } from "@/components/error-panel";
import { format } from "date-fns";
import { zhCN } from "date-fns/locale";

const EVENT_OPTIONS = [
  { id: "backup_failed", label: "备份失败" },
  { id: "agent_offline", label: "节点离线" },
];

export function NotificationsPage() {
  const queryClient = useQueryClient();
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [testSuccessId, setTestSuccessId] = useState<string | null>(null);

  const [formData, setFormData] = useState<NotificationInput>({
    name: "",
    type: "telegram",
    config: { bot_token: "", chat_id: "" },
    events: ["backup_failed", "agent_offline"],
  });

  const { data: notifications, isLoading } = useQuery({ queryKey: ["notifications"], queryFn: listNotifications });

  const createMutation = useMutation({
    mutationFn: createNotification,
    onSuccess: () => {
      setIsDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });

  const updateMutation = useMutation({
    mutationFn: (data: NotificationInput) => updateNotification(editingId!, data),
    onSuccess: () => {
      setIsDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteNotification,
    onSuccess: () => {
      setConfirmDeleteId(null);
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });

  const testMutation = useMutation({
    mutationFn: testNotification,
    onSuccess: (_, id) => {
      setTestSuccessId(id);
      setTimeout(() => setTestSuccessId(null), 3000);
    },
  });

  const handleEdit = (n: NotificationConfig) => {
    setEditingId(n.id);
    setFormData({
      name: n.name,
      type: n.type,
      config: n.config,
      events: n.events,
    });
    setIsDrawerOpen(true);
  };

  const handleDrawerClose = (open: boolean) => {
    setIsDrawerOpen(open);
    if (!open) {
      setEditingId(null);
      setFormData({
        name: "",
        type: "telegram",
        config: { bot_token: "", chat_id: "" },
        events: ["backup_failed", "agent_offline"],
      });
      createMutation.reset();
      updateMutation.reset();
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (editingId) {
      updateMutation.mutate(formData);
    } else {
      createMutation.mutate(formData);
    }
  };

  const toggleEvent = (eventId: string) => {
    const newEvents = formData.events.includes(eventId)
      ? formData.events.filter(id => id !== eventId)
      : [...formData.events, eventId];
    setFormData({ ...formData, events: newEvents });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">通知设置</h1>
        <Sheet open={isDrawerOpen} onOpenChange={handleDrawerClose}>
          <SheetTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> 添加通知
            </Button>
          </SheetTrigger>
          <SheetContent className="sm:max-w-md">
            <SheetHeader>
              <SheetTitle>{editingId ? "编辑通知" : "添加新通知"}</SheetTitle>
              <SheetDescription>
                配置系统告警的发送渠道和触发事件。
              </SheetDescription>
            </SheetHeader>
            <form onSubmit={handleSubmit} className="space-y-6 py-6 pb-20">
              <ErrorPanel error={(createMutation.error || updateMutation.error) as any} />
              
              <div className="space-y-2">
                <Label htmlFor="name">名称</Label>
                <Input
                  id="name"
                  placeholder="如: 运维 Telegram"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  required
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="type">通知类型</Label>
                <Select
                  value={formData.type}
                  onValueChange={(val: any) => setFormData({ 
                    ...formData, 
                    type: val, 
                    config: val === "telegram" ? { bot_token: "", chat_id: "" } : { url: "" } 
                  })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="telegram">Telegram Bot</SelectItem>
                    <SelectItem value="webhook">Generic Webhook</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-4 border rounded-md p-4 bg-muted/20">
                <Label className="text-xs font-bold uppercase tracking-wider text-muted-foreground">渠道配置</Label>
                {formData.type === "telegram" ? (
                  <>
                    <div className="space-y-2">
                      <Label htmlFor="bot_token">Bot Token</Label>
                      <Input
                        id="bot_token"
                        type="password"
                        value={formData.config.bot_token || ""}
                        onChange={(e) => setFormData({ ...formData, config: { ...formData.config, bot_token: e.target.value }})}
                        placeholder={formData.config.bot_token === "[redacted]" ? "已加密 (输入以修改)" : "123456789:ABC..."}
                        required={formData.config.bot_token !== "[redacted]"}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="chat_id">Chat ID</Label>
                      <Input
                        id="chat_id"
                        value={formData.config.chat_id || ""}
                        onChange={(e) => setFormData({ ...formData, config: { ...formData.config, chat_id: e.target.value }})}
                        placeholder="-100..."
                        required
                      />
                    </div>
                  </>
                ) : (
                  <div className="space-y-2">
                    <Label htmlFor="webhook_url">Webhook URL</Label>
                    <Input
                      id="webhook_url"
                      value={formData.config.url || ""}
                      onChange={(e) => setFormData({ ...formData, config: { ...formData.config, url: e.target.value }})}
                      placeholder="https://hooks.slack.com/..."
                      required
                    />
                  </div>
                )}
              </div>

              <div className="space-y-4">
                <Label>触发事件</Label>
                <div className="grid gap-4">
                  {EVENT_OPTIONS.map((opt) => (
                    <div key={opt.id} className="flex items-center space-x-2">
                      <Checkbox 
                        id={`event-${opt.id}`} 
                        checked={formData.events.includes(opt.id)}
                        onCheckedChange={() => toggleEvent(opt.id)}
                      />
                      <label htmlFor={`event-${opt.id}`} className="text-sm font-medium leading-none cursor-pointer">
                        {opt.label}
                      </label>
                    </div>
                  ))}
                </div>
              </div>

              <div className="fixed bottom-0 right-0 left-0 bg-background border-t p-4 lg:left-auto lg:w-[var(--radix-sheet-width)]">
                 <Button type="submit" className="w-full" disabled={createMutation.isPending || updateMutation.isPending}>
                  {createMutation.isPending || updateMutation.isPending ? "正在保存..." : "保存通知配置"}
                </Button>
              </div>
            </form>
          </SheetContent>
        </Sheet>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>订阅事件</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow><TableCell colSpan={4} className="h-24 text-center">正在加载...</TableCell></TableRow>
            ) : notifications?.length === 0 ? (
              <TableRow><TableCell colSpan={4} className="h-24 text-center text-muted-foreground">暂无通知配置</TableCell></TableRow>
            ) : (
              notifications?.map((n) => (
                <TableRow key={n.id}>
                  <TableCell className="font-medium">{n.name}</TableCell>
                  <TableCell className="capitalize">{n.type}</TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {n.events.map(ev => (
                        <span key={ev} className="text-[10px] bg-muted px-1.5 py-0.5 rounded text-muted-foreground border">
                          {EVENT_OPTIONS.find(o => o.id === ev)?.label || ev}
                        </span>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                       <Button 
                        variant="ghost" 
                        size="icon" 
                        onClick={() => testMutation.mutate(n.id)}
                        disabled={testMutation.isPending && testMutation.variables === n.id}
                        title="发送测试消息"
                      >
                        {testSuccessId === n.id ? <Check className="h-4 w-4 text-green-500" /> : <Send className="h-4 w-4" />}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => handleEdit(n)}>
                            <Settings2 className="mr-2 h-4 w-4" /> 编辑
                          </DropdownMenuItem>
                          <DropdownMenuItem className="text-red-600" onClick={() => setConfirmDeleteId(n.id)}>
                            <Trash2 className="mr-2 h-4 w-4" /> 删除
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <ConfirmDialog
        open={!!confirmDeleteId}
        onOpenChange={(open) => !open && setConfirmDeleteId(null)}
        title="确认删除通知配置？"
        description="系统将停止向此渠道发送告警。此操作不可撤销。"
        onConfirm={() => confirmDeleteId && deleteMutation.mutate(confirmDeleteId)}
        loading={deleteMutation.isPending}
      />
    </div>
  );
}
