import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listStorage, createStorage, updateStorage, deleteStorage } from "@/services/storage";
import { StorageConfig, StorageInput } from "@/types/storage";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Plus, Database, Settings2, Trash2, MoreHorizontal } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger, SheetFooter } from "@/components/ui/sheet";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { KeyValueEditor } from "@/components/key-value-editor";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorPanel } from "@/components/error-panel";
import { format } from "date-fns";
import { zhCN } from "date-fns/locale";

const STORAGE_TEMPLATES: Record<string, { name: string; defaults: Record<string, string>; fields: { key: string; label: string; type?: string }[] }> = {
  s3: {
    name: "Amazon S3 / 兼容对象存储",
    defaults: { provider: "AWS", region: "us-east-1" },
    fields: [
      { key: "provider", label: "Provider (AWS, Alibaba, etc.)" },
      { key: "access_key_id", label: "Access Key ID" },
      { key: "secret_access_key", label: "Secret Access Key", type: "password" },
      { key: "region", label: "Region" },
      { key: "endpoint", label: "Endpoint (可选)" },
    ],
  },
  webdav: {
    name: "WebDAV",
    defaults: { vendor: "other" },
    fields: [
      { key: "url", label: "URL" },
      { key: "user", label: "用户名" },
      { key: "pass", label: "密码", type: "password" },
    ],
  },
  sftp: {
    name: "SFTP",
    defaults: {},
    fields: [
      { key: "host", label: "主机地址" },
      { key: "user", label: "用户名" },
      { key: "pass", label: "密码", type: "password" },
      { key: "port", label: "端口 (默认 22)" },
    ],
  },
  local: {
    name: "本地路径",
    defaults: {},
    fields: [],
  }
};

export function StoragePage() {
  const queryClient = useQueryClient();
  const [isDrawerOpen, setIsAddDrawerOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  
  const [formData, setFormData] = useState<StorageInput>({
    name: "",
    rclone_type: "s3",
    rclone_config: STORAGE_TEMPLATES.s3.defaults,
  });

  const { data: storageList, isLoading } = useQuery({ queryKey: ["storage"], queryFn: listStorage });

  const createMutation = useMutation({
    mutationFn: createStorage,
    onSuccess: () => {
      setIsAddDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
    },
  });

  const updateMutation = useMutation({
    mutationFn: (data: StorageInput) => updateStorage(editingId!, data),
    onSuccess: () => {
      setIsAddDrawerOpen(false);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteStorage,
    onSuccess: () => {
      setConfirmDeleteId(null);
      queryClient.invalidateQueries({ queryKey: ["storage"] });
    },
  });

  const handleEdit = (storage: StorageConfig) => {
    setEditingId(storage.id);
    setFormData({
      name: storage.name,
      rclone_type: storage.rclone_type,
      rclone_config: storage.rclone_config,
    });
    setIsAddDrawerOpen(true);
  };

  const handleDrawerClose = (open: boolean) => {
    setIsAddDrawerOpen(open);
    if (!open) {
      setEditingId(null);
      setFormData({ name: "", rclone_type: "s3", rclone_config: STORAGE_TEMPLATES.s3.defaults });
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

  const template = STORAGE_TEMPLATES[formData.rclone_type];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">存储配置</h1>
        <Sheet open={isDrawerOpen} onOpenChange={handleDrawerClose}>
          <SheetTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> 添加存储
            </Button>
          </SheetTrigger>
          <SheetContent className="sm:max-w-lg overflow-y-auto">
            <SheetHeader>
              <SheetTitle>{editingId ? "编辑存储" : "添加新存储"}</SheetTitle>
              <SheetDescription>
                配置用于存放备份数据的 rclone 存储端点。
              </SheetDescription>
            </SheetHeader>
            <form onSubmit={handleSubmit} className="space-y-6 py-6 pb-20">
              <ErrorPanel error={(createMutation.error || updateMutation.error) as any} />
              
              <div className="space-y-2">
                <Label htmlFor="name">名称</Label>
                <Input
                  id="name"
                  placeholder="如: Production-S3"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  required
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="type">存储类型</Label>
                <Select
                  value={formData.rclone_type}
                  onValueChange={(val) => setFormData({ 
                    ...formData, 
                    rclone_type: val, 
                    rclone_config: STORAGE_TEMPLATES[val]?.defaults || {} 
                  })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(STORAGE_TEMPLATES).map(([k, v]) => (
                      <SelectItem key={k} value={k}>{v.name}</SelectItem>
                    ))}
                    <SelectItem value="other">其他 (手动配置)</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <Tabs defaultValue="template" className="w-full">
                <TabsList className="grid w-full grid-cols-2">
                  <TabsTrigger value="template">模版模式</TabsTrigger>
                  <TabsTrigger value="advanced">高级模式</TabsTrigger>
                </TabsList>
                <TabsContent value="template" className="space-y-4 pt-4">
                  {template ? (
                    template.fields.length > 0 ? (
                      template.fields.map((f) => (
                        <div key={f.key} className="space-y-2">
                          <Label htmlFor={`field-${f.key}`}>{f.label}</Label>
                          <Input
                            id={`field-${f.key}`}
                            type={f.type || "text"}
                            value={formData.rclone_config[f.key] || ""}
                            onChange={(e) => setFormData({
                              ...formData,
                              rclone_config: { ...formData.rclone_config, [f.key]: e.target.value }
                            })}
                            placeholder={formData.rclone_config[f.key] === "[redacted]" ? "已加密 (输入以修改)" : ""}
                          />
                        </div>
                      ))
                    ) : (
                      <p className="text-sm text-muted-foreground py-4">此类型无需额外字段配置。</p>
                    )
                  ) : (
                    <p className="text-sm text-muted-foreground py-4">请切换到高级模式进行手动配置。</p>
                  )}
                </TabsContent>
                <TabsContent value="advanced" className="pt-4">
                  <KeyValueEditor
                    value={formData.rclone_config}
                    onChange={(val) => setFormData({ ...formData, rclone_config: val })}
                  />
                </TabsContent>
              </Tabs>

              <div className="fixed bottom-0 right-0 left-0 bg-background border-t p-4 lg:left-auto lg:w-[var(--radix-sheet-width)]">
                 <Button type="submit" className="w-full" disabled={createMutation.isPending || updateMutation.isPending}>
                  {createMutation.isPending || updateMutation.isPending ? "正在保存..." : "保存配置"}
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
              <TableHead className="hidden md:table-cell">创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow><TableCell colSpan={4} className="h-24 text-center">正在加载...</TableCell></TableRow>
            ) : storageList?.length === 0 ? (
              <TableRow><TableCell colSpan={4} className="h-24 text-center text-muted-foreground">暂无存储配置</TableCell></TableRow>
            ) : (
              storageList?.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="font-medium">{s.name}</TableCell>
                  <TableCell className="capitalize">{s.rclone_type}</TableCell>
                  <TableCell className="hidden md:table-cell text-xs text-muted-foreground">
                    {format(new Date(s.created_at), "yyyy-MM-dd HH:mm", { locale: zhCN })}
                  </TableCell>
                  <TableCell className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => handleEdit(s)}>
                          <Settings2 className="mr-2 h-4 w-4" /> 编辑
                        </DropdownMenuItem>
                        <DropdownMenuItem className="text-red-600" onClick={() => setConfirmDeleteId(s.id)}>
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
        onOpenChange={(open) => !open && setConfirmDeleteId(null)}
        title="确认删除存储配置？"
        description="如果已有策略正在使用此存储，删除将导致备份失败。此操作不可撤销。"
        onConfirm={() => confirmDeleteId && deleteMutation.mutate(confirmDeleteId)}
        loading={deleteMutation.isPending}
      />
    </div>
  );
}
