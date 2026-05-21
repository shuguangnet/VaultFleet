# VaultFleet 前端功能补齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐前端与后端 API 的对接，修复断裂功能，提升交互体验，使 VaultFleet 成为完整可用的产品。

**Architecture:** 前端使用 React + TypeScript + react-query + shadcn/ui + Tailwind CSS。变更按功能域组织为独立任务：先更新类型定义和 Service 层（基础设施），再逐页面补齐功能。每个任务独立可交付。

**Tech Stack:** React 19, TypeScript, Vite, react-query (TanStack Query), shadcn/ui, Tailwind CSS, lucide-react, date-fns, vitest。

---

## Scope Check

本计划实现设计文档 [2026-05-21-vaultfleet-frontend-completion-design.md](../specs/2026-05-21-vaultfleet-frontend-completion-design.md) 的全部内容。所有功能基于已有后端 API 实现，不需要后端改动。

## File Structure

新增：

- `web/src/types/command.ts`：Agent 命令类型定义。
- `web/src/types/health.ts`：健康检查和存储测试结果类型。
- `web/src/services/commands.ts`：命令查询 API 调用。
- `web/src/services/health.ts`：健康检查 API 调用。

修改：

- `web/src/types/task.ts`：扩展 TaskHistory 字段和状态类型。
- `web/src/types/snapshot.ts`：更新 SnapshotRefreshResponse 和 RestoreAccepted 类型。
- `web/src/services/agents.ts`：更新 backupNow 返回类型。
- `web/src/services/storage.ts`：新增连接测试方法。
- `web/src/components/status-badge.tsx`：新增 timeout、dispatched、succeeded、queued 状态。
- `web/src/pages/nodes/node-detail-page.tsx`：手动备份按钮、修复恢复/详情按钮、新增命令 Tab。
- `web/src/pages/storage/storage-page.tsx`：存储连接测试功能。
- `web/src/pages/tasks/tasks-page.tsx`：新状态筛选、展开行增强、自动刷新。
- `web/src/pages/dashboard/dashboard-page.tsx`：系统就绪状态卡片。
- `web/src/pages/system/system-page.tsx`：系统健康状态卡片。
- `web/src/pages/snapshots/snapshots-page.tsx`：适配离线排队响应，使用 toast 替代内联成功展示。
- `web/src/app.tsx`：挂载 Toast provider（如果引入 sonner 或 shadcn toast）。

## 关于 UI 样式的说明

本计划只定义**功能需求和数据对接**。所有 UI 样式决策（颜色、间距、阴影、动画、具体使用哪个 shadcn variant）由实施者自行决定。本计划中出现的具体组件名称（如 `Button`、`Sheet`）仅作为功能性示意。

---

### Task 1: 类型定义和 Service 层更新

**Files:**
- Create: `web/src/types/command.ts`
- Create: `web/src/types/health.ts`
- Create: `web/src/services/commands.ts`
- Create: `web/src/services/health.ts`
- Modify: `web/src/types/task.ts`
- Modify: `web/src/types/snapshot.ts`
- Modify: `web/src/services/agents.ts`
- Modify: `web/src/services/storage.ts`

- [ ] **Step 1: 创建命令类型定义**

创建 `web/src/types/command.ts`：

```typescript
export type CommandType = "backup_now" | "restore_req" | "policy_push" | "snapshot_list_req";
export type CommandStatus = "pending" | "dispatched" | "running" | "succeeded" | "failed" | "timeout";

export interface AgentCommand {
  id: string;
  agent_id: string;
  type: CommandType;
  status: CommandStatus;
  message_id: string;
  result?: string;
  error_message?: string;
  attempts: number;
  policy_id?: string;
  storage_id?: string;
  deadline_at?: string;
  dispatched_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CommandFilters {
  status?: string;
  type?: string;
  limit?: number;
}
```

- [ ] **Step 2: 创建健康检查类型定义**

创建 `web/src/types/health.ts`：

```typescript
export interface HealthResponse {
  ok: boolean;
  status: string;
}

export interface ReadyResponse {
  ok: boolean;
  status?: string;
  error?: string;
}

export interface StorageTestResult {
  ok: boolean;
  latency_ms: number;
  error?: string;
  checked_at: string;
}
```

- [ ] **Step 3: 更新任务类型定义**

修改 `web/src/types/task.ts`，将 `TaskHistory` 接口替换为：

```typescript
export interface TaskHistory {
  id: number;
  message_id: string;
  agent_id: string;
  type: "backup" | "restore";
  status: "pending" | "running" | "success" | "failed" | "timeout";
  snapshot_id?: string;
  command_id?: string;
  policy_id?: string;
  storage_id?: string;
  started_at?: string;
  finished_at?: string;
  repo_size?: number;
  duration_ms?: number;
  error_log?: string;
  created_at: string;
  updated_at?: string;
}

export interface TaskFilters {
  agent_id?: string;
  type?: string;
  status?: string;
  limit?: number;
}
```

- [ ] **Step 4: 更新快照类型定义**

修改 `web/src/types/snapshot.ts`，更新 `SnapshotRefreshResponse` 和 `RestoreAccepted` 以包含新字段：

```typescript
export interface Snapshot {
  id: string;
  time: string;
  paths: string[];
  hostname: string;
  username: string;
  tags?: string[];
}

export interface SnapshotRefreshResponse {
  message_id: string;
  command_id?: string;
  message?: string;
}

export interface RestoreRequest {
  snapshot_id: string;
  target_path: string;
}

export interface RestoreAccepted {
  message_id: string;
  command_id?: string;
  message?: string;
}
```

- [ ] **Step 5: 创建命令 Service**

创建 `web/src/services/commands.ts`：

```typescript
import { AgentCommand, CommandFilters } from "@/types/command";
import { apiGet } from "./http";

export const getCommand = (id: string) => apiGet<AgentCommand>(`/api/commands/${id}`);

export const listAgentCommands = (agentId: string, filters: CommandFilters = {}) =>
  apiGet<AgentCommand[]>(`/api/agents/${agentId}/commands${toQuery(filters)}`);

function toQuery(filters: CommandFilters): string {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.type) params.set("type", filters.type);
  if (filters.limit) params.set("limit", filters.limit.toString());
  const query = params.toString();
  return query ? `?${query}` : "";
}
```

- [ ] **Step 6: 创建健康检查 Service**

创建 `web/src/services/health.ts`。

注意：`/health` 和 `/ready` 端点不在 `/api` 路径前缀下且不需要认证。它们的响应格式是直接的 JSON 对象（`{"ok": true, "status": "healthy"}`），不走 `{ok: true, data: ...}` 封装。因此不能使用 `apiGet`（`apiGet` 会尝试解包 `data` 字段）。需要直接使用 fetch：

```typescript
import { HealthResponse, ReadyResponse } from "@/types/health";

export async function checkHealth(): Promise<HealthResponse> {
  const response = await fetch("/health");
  return response.json();
}

export async function checkReady(): Promise<ReadyResponse> {
  const response = await fetch("/ready");
  return response.json();
}
```

- [ ] **Step 7: 更新 agents Service**

修改 `web/src/services/agents.ts`，更新 `backupNow` 的返回类型：

将：
```typescript
export const backupNow = (id: string) => apiPost<{ message_id: string }>(`/api/agents/${id}/backup-now`);
```

替换为：
```typescript
export const backupNow = (id: string) => apiPost<{ command_id: string; message_id: string }>(`/api/agents/${id}/backup-now`);
```

- [ ] **Step 8: 更新 storage Service**

修改 `web/src/services/storage.ts`，在文件末尾新增两个存储测试方法：

```typescript
import { StorageTestResult } from "@/types/health";
```

```typescript
export const testUnsavedStorage = (body: { rclone_type: string; rclone_config: Record<string, string> }) =>
  apiPost<StorageTestResult>("/api/storage/test", body);

export const testSavedStorage = (id: string) =>
  apiPost<StorageTestResult>(`/api/storage/${id}/test`);
```

- [ ] **Step 9: 验证构建**

运行：

```bash
cd web && npm run build
```

期望：构建成功，无 TypeScript 错误。

- [ ] **Step 10: 验证测试**

运行：

```bash
cd web && npm test
```

期望：所有现有测试通过（5 个测试）。

- [ ] **Step 11: 提交 Task 1**

```bash
git add web/src/types/command.ts web/src/types/health.ts web/src/types/task.ts web/src/types/snapshot.ts web/src/services/commands.ts web/src/services/health.ts web/src/services/agents.ts web/src/services/storage.ts
git commit -m "feat(web): add command and health types and services"
```

---

### Task 2: StatusBadge 扩展和 Toast 基础设施

**Files:**
- Modify: `web/src/components/status-badge.tsx`
- Modify: `web/src/app.tsx`

本任务需要引入 toast 通知组件。推荐使用 sonner（轻量、与 shadcn/ui 生态兼容）。如果选择其他 toast 方案（如 shadcn 自带的 toast），也可以。

- [ ] **Step 1: 安装 toast 依赖**

运行：

```bash
cd web && npm install sonner
```

如果选择不用 sonner，可以使用 shadcn/ui 的 toast 组件（通过 `npx shadcn@latest add toast` 安装），用法类似。

- [ ] **Step 2: 扩展 StatusBadge**

修改 `web/src/components/status-badge.tsx`。

当前 `StatusType` 联合类型和 `config` 对象需要扩展。将 StatusType 扩展为：

```typescript
type StatusType = "online" | "offline" | "success" | "failed" | "running" | "syncing" | "unsynced" | "pending" | "timeout" | "dispatched" | "succeeded" | "queued";
```

在 `config` 对象中新增以下映射：

```typescript
timeout: { label: "超时", className: "..." },
dispatched: { label: "已下发", className: "..." },
succeeded: { label: "成功", className: "..." },
queued: { label: "已排队", className: "..." },
```

同时将 `pending` 的 label 从 "待同步" 改为 "等待中"。

具体的颜色和样式 className 由实施者自行决定。

- [ ] **Step 3: 在 App 中挂载 Toast provider**

修改 `web/src/app.tsx`，在 `RouterProvider` 同级添加 Toast provider。

如果使用 sonner，在 `<QueryClientProvider>` 内部、`<RouterProvider>` 之后添加 `<Toaster />`：

```typescript
import { Toaster } from "sonner";
```

```tsx
<QueryClientProvider client={queryClient}>
  <RouterProvider router={router} />
  <Toaster />
</QueryClientProvider>
```

如果使用 shadcn toast，按照 shadcn 文档添加 `<Toaster />`。

- [ ] **Step 4: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 5: 提交 Task 2**

```bash
git add web/src/components/status-badge.tsx web/src/app.tsx package.json package-lock.json
git commit -m "feat(web): extend status badge and add toast infrastructure"
```

---

### Task 3: 节点详情页 — 手动备份和修复恢复/详情按钮

**Files:**
- Modify: `web/src/pages/nodes/node-detail-page.tsx`

当前节点详情页（`web/src/pages/nodes/node-detail-page.tsx`）有以下问题需要修复：

1. 没有手动备份按钮。
2. 快照 Tab 中的"恢复"是纯文本 `<TableCell>` （第 142 行），不可点击。
3. 任务 Tab 中的"详情"是纯文本 `<TableCell>`（第 167 行），不可点击。

- [ ] **Step 1: 新增手动备份按钮**

在节点详情页头部区域（当前是节点名称和状态信息的 `<div>` 块），新增一个"立即备份"按钮。

功能逻辑：
- 导入 `backupNow` from `@/services/agents`。
- 使用 `useMutation` 调用 `backupNow(agentId)`。
- 点击前弹出确认框（可使用已有的 `ConfirmDialog` 组件，该组件在 `web/src/components/confirm-dialog.tsx`）。
- 成功后使用 toast 通知：
  - 如果 `agent.status === "online"`：提示"备份命令已下发"。
  - 如果 `agent.status === "offline"`：提示"备份命令已排队，Agent 上线后将自动执行"。
- 成功后通过 `queryClient.invalidateQueries` 刷新 tasks 和 commands 查询。
- 按钮在请求期间禁用并显示 loading 状态。

需要新增的 imports：

```typescript
import { backupNow } from "@/services/agents";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ConfirmDialog } from "@/components/confirm-dialog";
```

如果使用 sonner：

```typescript
import { toast } from "sonner";
```

- [ ] **Step 2: 修复快照 Tab 恢复按钮**

将快照 Tab 中第 142 行的纯文本 `<TableCell>` 改为可交互的恢复按钮。

当前代码：
```tsx
<TableCell className="text-right text-sm text-primary cursor-pointer">恢复</TableCell>
```

替换为一个真正的按钮，点击后弹出恢复弹窗。

恢复弹窗的功能需求（可参考 `pages/snapshots/snapshots-page.tsx` 中已有的恢复交互模式）：
- 显示 snapshot_id 和快照时间。
- 输入目标恢复路径（初始值为快照的第一个 path）。
- 显示风险警告文本。
- 确认 checkbox，勾选后才能点确认。
- 调用 `restoreSnapshot(agentId, { snapshot_id, target_path })`（从 `@/services/snapshots` 导入）。
- 成功后使用 toast 通知：根据返回的 `message` 字段显示内容（"restore started" 或 "restore queued"），中文化展示。
- 成功后关闭弹窗，刷新任务列表。

需要新增的 imports：

```typescript
import { restoreSnapshot } from "@/services/snapshots";
import { Snapshot } from "@/types/snapshot";
```

组件内部需要新增的 state：
- `selectedSnapshot` — 当前选中要恢复的快照。
- `targetPath` — 用户输入的恢复目标路径。
- `confirmed` — 确认 checkbox 状态。

- [ ] **Step 3: 修复任务 Tab 详情按钮**

将任务 Tab 中第 167 行的纯文本 `<TableCell>` 改为可点击的按钮。

当前代码：
```tsx
<TableCell className="text-right text-sm text-primary cursor-pointer">详情</TableCell>
```

两种实现方式任选：

**方式 A（推荐）：展开行内联显示详情。** 类似 `pages/tasks/tasks-page.tsx` 的展开行模式，点击后在当前行下方展开显示：
- `error_log`（如果有，使用错误高亮样式）。
- `snapshot_id`（如果有）。
- `command_id`（如果有）。
- `duration_ms`（格式化为秒）。
- `started_at` 和 `finished_at`。

**方式 B：直接跳转。** 点击后导航到 `/tasks?agent_id=xxx`。

- [ ] **Step 4: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 5: 提交 Task 3**

```bash
git add web/src/pages/nodes/node-detail-page.tsx
git commit -m "feat(web): add backup button and fix restore/detail in node detail"
```

---

### Task 4: 节点详情页 — 新增命令队列 Tab

**Files:**
- Modify: `web/src/pages/nodes/node-detail-page.tsx`

- [ ] **Step 1: 新增命令 Tab**

在节点详情页的 Tabs 组件中新增第 6 个 Tab "命令"。

当前 TabsList 是 `grid-cols-5`（概览、策略、快照、任务、文件浏览），改为 `grid-cols-6`，新增 TabsTrigger "命令"。

命令 Tab 的数据获取：

```typescript
import { listAgentCommands } from "@/services/commands";
import { AgentCommand } from "@/types/command";
```

使用 react-query 获取命令列表：

```typescript
const { data: commands, isFetching: commandsFetching } = useQuery({
  queryKey: ["commands", agentId],
  queryFn: () => listAgentCommands(agentId!),
  enabled: !!agentId,
});
```

- [ ] **Step 2: 命令列表表格**

命令 Tab 内容区域显示一个表格，列定义：

| 列 | 数据字段 | 说明 |
|:---|:---|:---|
| 类型 | `type` | 中文映射：`backup_now`→手动备份, `restore_req`→恢复, `policy_push`→策略下发, `snapshot_list_req`→快照刷新 |
| 状态 | `status` | 使用 StatusBadge 组件展示 |
| 创建时间 | `created_at` | 格式化为日期时间 |
| 完成时间 | `completed_at` | 如果有值格式化为日期时间，否则显示 "-" |
| 尝试次数 | `attempts` | 数字 |
| 错误信息 | `error_message` | 如果有值显示（截断展示），否则显示 "-" |

命令类型中文映射表：

```typescript
const COMMAND_TYPE_LABELS: Record<string, string> = {
  backup_now: "手动备份",
  restore_req: "恢复",
  policy_push: "策略下发",
  snapshot_list_req: "快照刷新",
};
```

- [ ] **Step 3: 命令列表自动刷新**

当命令列表中存在 `status` 为 `pending`、`dispatched` 或 `running` 的命令时，每 5 秒自动刷新。

使用 react-query 的 `refetchInterval` 配置：

```typescript
const { data: commands } = useQuery({
  queryKey: ["commands", agentId],
  queryFn: () => listAgentCommands(agentId!),
  enabled: !!agentId,
  refetchInterval: (query) => {
    const data = query.state.data;
    const hasActive = data?.some(
      (c) => c.status === "pending" || c.status === "dispatched" || c.status === "running"
    );
    return hasActive ? 5000 : false;
  },
});
```

同时添加手动刷新按钮。

- [ ] **Step 4: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 5: 提交 Task 4**

```bash
git add web/src/pages/nodes/node-detail-page.tsx
git commit -m "feat(web): add command queue tab to node detail page"
```

---

### Task 5: 存储配置页 — 连接测试

**Files:**
- Modify: `web/src/pages/storage/storage-page.tsx`

- [ ] **Step 1: 表单内连接测试按钮**

在存储创建/编辑的 Sheet 表单中，在底部"保存配置"按钮旁边（或上方），新增"测试连接"按钮。

功能逻辑：

```typescript
import { testUnsavedStorage, testSavedStorage } from "@/services/storage";
import { StorageTestResult } from "@/types/health";
```

使用 `useMutation` 执行测试：

```typescript
const testMutation = useMutation({
  mutationFn: async () => {
    // 如果是编辑模式且表单值未修改，测试已保存的配置
    // 否则测试当前表单中的值
    if (editingId && !formModified) {
      return testSavedStorage(editingId);
    }
    return testUnsavedStorage({
      rclone_type: formData.rclone_type,
      rclone_config: formData.rclone_config,
    });
  },
});
```

关于编辑模式的行为：
- 如果用户正在编辑已保存的配置，且修改了表单值但尚未保存，"测试连接"应该使用 `POST /api/storage/test` 传入当前表单的值（因为 `POST /api/storage/:id/test` 测试的是数据库中的旧值）。
- 简化实现：可以统一使用 `testUnsavedStorage` 传入当前表单值，不区分是否修改过。这对用户来说更直觉 — "测试当前看到的配置"。

按钮在请求期间显示 loading 状态。

测试结果在按钮附近内联展示：
- 成功：显示通过标记和延迟毫秒数（如 "连接成功 (123ms)"）。
- 失败：显示失败标记和脱敏后的错误信息。

测试结果不阻塞表单提交。

- [ ] **Step 2: 存储列表快速测试**

在存储列表的操作下拉菜单中（当前有"编辑"和"删除"两个选项），新增"测试连接"选项。

当前下拉菜单结构位于 `storage-page.tsx` 的 `DropdownMenuContent` 中。在"编辑"和"删除"之间新增：

```tsx
<DropdownMenuItem onClick={() => handleTestStorage(s.id)}>
  测试连接
</DropdownMenuItem>
```

`handleTestStorage` 函数调用 `testSavedStorage(id)`，结果通过 toast 展示：
- 成功：toast 成功提示 "连接成功 (Xms)"。
- 失败：toast 错误提示，包含错误信息。

使用 `useMutation` 管理请求状态。

- [ ] **Step 3: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 4: 提交 Task 5**

```bash
git add web/src/pages/storage/storage-page.tsx
git commit -m "feat(web): add storage connection test"
```

---

### Task 6: 任务历史页增强

**Files:**
- Modify: `web/src/pages/tasks/tasks-page.tsx`

- [ ] **Step 1: 扩展状态筛选选项**

当前状态筛选下拉框（`tasks-page.tsx` 第 85-95 行）只有：全部状态、成功、失败、运行中。

新增两个选项：

```tsx
<SelectItem value="pending">等待中</SelectItem>
<SelectItem value="timeout">超时</SelectItem>
```

完整的状态筛选顺序：全部状态、等待中、运行中、成功、失败、超时。

- [ ] **Step 2: 增强展开行详情**

当前展开行（第 144-179 行）显示 message_id、snapshot_id、error_log。

新增以下字段展示：

- `command_id`（如果有值，显示标签或代码块）。
- `started_at` 和 `finished_at`（格式化为 `yyyy-MM-dd HH:mm:ss`）。
- `policy_id` 和 `storage_id`（如果有值，显示标签）。

在现有的 `grid grid-cols-2 gap-4` 区域内新增这些字段，保持一致的展示风格（标签 + 代码块）。

- [ ] **Step 3: 添加自动刷新**

当查询结果中有 `status === "pending"` 或 `status === "running"` 的任务时，自动每 5 秒刷新。

修改 `useQuery` 调用，添加 `refetchInterval`：

```typescript
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
```

- [ ] **Step 4: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 5: 提交 Task 6**

```bash
git add web/src/pages/tasks/tasks-page.tsx
git commit -m "feat(web): enhance tasks page with new statuses and auto-refresh"
```

---

### Task 7: 仪表盘 — 系统就绪状态

**Files:**
- Modify: `web/src/pages/dashboard/dashboard-page.tsx`

- [ ] **Step 1: 新增系统就绪状态卡片**

在仪表盘现有的 4 个统计卡片区域，新增一个"系统状态"卡片。

数据获取：

```typescript
import { checkReady } from "@/services/health";
```

```typescript
const { data: readyStatus } = useQuery({
  queryKey: ["ready"],
  queryFn: checkReady,
  refetchInterval: 30000,
});
```

卡片内容：
- 就绪时（`readyStatus?.ok === true`）：显示"系统就绪"和一个正向状态指示。
- 不就绪时（`readyStatus?.ok === false`）：显示"系统未就绪"和错误原因 `readyStatus?.error`。
- 请求失败时（query error）：显示"无法连接服务器"。

卡片位置和样式由实施者自行决定 — 可以放在现有 4 卡片之前/之后/单独一行。

- [ ] **Step 2: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 3: 提交 Task 7**

```bash
git add web/src/pages/dashboard/dashboard-page.tsx
git commit -m "feat(web): add system readiness card to dashboard"
```

---

### Task 8: 系统管理页 — 健康状态卡片

**Files:**
- Modify: `web/src/pages/system/system-page.tsx`

- [ ] **Step 1: 新增系统状态卡片**

在系统管理页现有的两张卡片（修改密码、数据导出）之前，新增一个"系统状态"卡片。

数据获取：

```typescript
import { checkHealth, checkReady } from "@/services/health";
import { useQuery } from "@tanstack/react-query";
```

```typescript
const { data: healthStatus } = useQuery({
  queryKey: ["health"],
  queryFn: checkHealth,
  refetchInterval: 30000,
});

const { data: readyStatus } = useQuery({
  queryKey: ["ready"],
  queryFn: checkReady,
  refetchInterval: 30000,
});
```

卡片内容展示两行状态：

1. **服务进程**：基于 `healthStatus` 展示。如果页面能加载，服务基本在线，但仍然调用 `/health` 作为明确状态。
2. **服务就绪**：基于 `readyStatus` 展示。显示数据库连接、master key、数据目录的就绪状态。

不就绪时显示错误信息。

当前系统管理页布局是 `grid gap-6 md:grid-cols-2`。新增的状态卡片可以放在这个 grid 之前，作为独立的全宽区域；也可以改为 3 列。由实施者决定。

- [ ] **Step 2: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 3: 提交 Task 8**

```bash
git add web/src/pages/system/system-page.tsx
git commit -m "feat(web): add system health status to system page"
```

---

### Task 9: 快照页适配和全局 Toast 替换

**Files:**
- Modify: `web/src/pages/snapshots/snapshots-page.tsx`
- Modify: `web/src/pages/nodes/node-detail-page.tsx`（文件浏览器 alert 替换）

- [ ] **Step 1: 快照页适配离线排队**

当前快照页（`snapshots-page.tsx`）的 refresh 按钮在 Agent 离线时被禁用（第 98 行 `currentAgent?.status !== "online"`）。

后端现在支持离线排队：`POST /api/agents/:id/snapshots/refresh` 在 Agent 离线时返回 `202 Accepted` + `{ message: "snapshot refresh queued", command_id, message_id }`。

修改：
- 移除 refresh 按钮的 `currentAgent?.status !== "online"` 禁用条件（保留 `!agentId` 和 loading 状态的禁用）。
- refresh 成功后，如果返回了 `message` 字段且包含 "queued"，使用 toast 提示"快照刷新命令已排队，Agent 上线后将自动执行"。
- 如果没有 message 或 message 为空，使用 toast 提示"快照列表刷新成功"。

- [ ] **Step 2: 快照页恢复成功 toast 化**

当前恢复成功后（第 164-180 行）是在 Sheet 内显示一个成功页面。

这个内联成功展示可以保留（因为它提供了 message_id 和跳转链接），但建议同时用 toast 做一个简短的全局通知，让用户即使关闭 Sheet 也能看到反馈。

在 `restoreMutation` 的 `onSuccess` 回调中新增 toast：

```typescript
onSuccess: (data) => {
  setRestoreSuccessId(data.message_id);
  const msg = data.message === "restore queued" ? "恢复命令已排队" : "恢复任务已开始";
  toast.success(msg);
},
```

- [ ] **Step 3: 替换文件浏览器 alert**

在节点详情页（`node-detail-page.tsx`）的文件浏览器 Tab 中，第 187 行有一个 `alert()` 调用：

```tsx
onSelect={(path) => {
  navigator.clipboard.writeText(path);
  alert(`路径已复制: ${path}`);
}}
```

将 `alert()` 替换为 toast：

```tsx
onSelect={(path) => {
  navigator.clipboard.writeText(path);
  toast.success(`路径已复制: ${path}`);
}}
```

- [ ] **Step 4: 验证构建和测试**

运行：

```bash
cd web && npm run build && npm test
```

期望：构建成功，所有测试通过。

- [ ] **Step 5: 提交 Task 9**

```bash
git add web/src/pages/snapshots/snapshots-page.tsx web/src/pages/nodes/node-detail-page.tsx
git commit -m "feat(web): adapt snapshot page for offline queuing and replace alerts with toast"
```

---

## Final Verification

- [ ] 运行全部前端测试：

```bash
cd web && npm test
```

期望：所有测试通过。

- [ ] 运行前端构建：

```bash
cd web && npm run build
```

期望：构建成功，无 TypeScript 错误。

- [ ] 运行后端测试（确认前端构建产物没有破坏后端）：

```bash
go test ./... -count=1
```

期望：所有后端测试通过。

- [ ] 检查提交历史：

```bash
git log --oneline -10
```

期望：每个任务有独立的 commit。

## Self-Review Notes

**Spec 覆盖对照：**

| Spec 需求 | 对应任务 |
|:---|:---|
| 类型定义更新 | Task 1 |
| Service 层更新 | Task 1 |
| StatusBadge 扩展 | Task 2 |
| Toast 基础设施 | Task 2 |
| 节点详情 — 手动备份按钮 | Task 3 |
| 节点详情 — 快照恢复按钮修复 | Task 3 |
| 节点详情 — 任务详情按钮修复 | Task 3 |
| 节点详情 — 命令队列 Tab | Task 4 |
| 存储页 — 连接测试 | Task 5 |
| 任务页 — 新状态筛选 | Task 6 |
| 任务页 — 展开行增强 | Task 6 |
| 任务页 — 自动刷新 | Task 6 |
| 仪表盘 — 系统就绪状态 | Task 7 |
| 系统页 — 健康状态卡片 | Task 8 |
| 快照页 — 离线排队适配 | Task 9 |
| 全局 Toast 替换 | Task 2 + Task 9 |

**类型一致性：**

- `TaskHistory.status` 在 `types/task.ts` 和 `tasks-page.tsx` 筛选器中一致：`pending | running | success | failed | timeout`。
- `AgentCommand` 在 `types/command.ts` 和 `services/commands.ts` 中一致。
- `StorageTestResult` 在 `types/health.ts` 和 `services/storage.ts` 中一致。
- `backupNow` 返回类型在 `services/agents.ts` 和 `node-detail-page.tsx` 使用中一致。
- `SnapshotRefreshResponse` 和 `RestoreAccepted` 在 `types/snapshot.ts` 和页面使用中一致。
- `COMMAND_TYPE_LABELS` 在 Task 4 中定义，键名与 `CommandType` 联合类型匹配。

**关于 `/health` 和 `/ready` 路径：**

这两个端点不在 `/api` 前缀下，且响应不走 `{ok, data}` 封装。`services/health.ts` 使用原生 `fetch` 而不是 `apiGet`，已在 Task 1 Step 6 中明确说明。
