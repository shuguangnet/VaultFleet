# VaultFleet 前端功能补齐设计

> 日期：2026-05-21
> 状态：待确认
> 目标：补齐前端与后端 API 的对接，修复断裂功能，提升交互体验，使 VaultFleet 形成完整可用的产品。

## 1. 背景

后端核心能力升级已合并（PR #1），新增了持久化 Agent 命令、离线命令排队、命令超时扫描、存储连接测试、健康检查/就绪检查/指标端点。前端尚未适配这些新 API，同时存在若干已定义但未调用的功能（如 `backupNow`）和不可交互的 UI 元素（节点详情页的恢复按钮、任务详情按钮）。

本设计覆盖：
1. 前端类型和 Service 层与新后端 API 对齐。
2. 修复所有断裂的交互功能。
3. 新增存储连接测试、命令队列展示、系统健康状态展示等功能。
4. UX 提升：任务自动刷新、操作反馈统一、表格加载更多。

## 2. 范围

### 2.1 本期必须完成

1. 更新 TypeScript 类型定义，对齐后端新字段和新状态。
2. 新增 commands、health Service 层，更新 storage Service。
3. 扩展 StatusBadge 支持 timeout、dispatched、succeeded、queued 状态。
4. 节点详情页：新增手动备份按钮、修复快照恢复按钮、修复任务详情按钮、新增命令队列 Tab。
5. 存储配置页：新增连接测试功能（创建/编辑时和列表中）。
6. 任务历史页：适配新状态、展开行增强、有进行中任务时自动刷新。
7. 仪表盘：系统就绪状态展示、任务表格适配新状态。
8. 系统管理页：新增系统健康状态卡片。
9. 全局 UX：操作反馈统一使用 toast。

### 2.2 本期不做

1. 不做 WebSocket 实时推送（任务更新用轮询）。
2. 不做国际化。
3. 不做深色模式。
4. 不做用户权限管理 UI。
5. 不新增后端 API — 所有前端功能基于已有后端接口实现。

## 3. 后端 API 清单

前端需要对接的全部后端 API：

### 3.1 已有 API（前端已对接）

```
GET    /api/agents                         → Agent 列表
POST   /api/agents                         → 创建 Agent
GET    /api/agents/:id                     → Agent 详情
DELETE /api/agents/:id                     → 删除 Agent
POST   /api/agents/:id/regenerate-token    → 重生成 enrollment token
POST   /api/agents/:id/browse              → 文件浏览
GET    /api/agents/:id/snapshots           → 快照列表
POST   /api/agents/:id/snapshots/refresh   → 刷新快照
POST   /api/agents/:id/restore             → 恢复快照
GET    /api/policies                        → 策略列表
POST   /api/policies                        → 创建策略
PUT    /api/policies/:id                    → 更新策略
DELETE /api/policies/:id                    → 删除策略
GET    /api/storage                         → 存储列表
POST   /api/storage                         → 创建存储
GET    /api/storage/:id                     → 存储详情
PUT    /api/storage/:id                     → 更新存储
DELETE /api/storage/:id                     → 删除存储
GET    /api/tasks                           → 任务历史
POST   /api/agents/:id/backup-now           → 手动备份
GET    /api/notifications                   → 通知列表
POST   /api/notifications                   → 创建通知
PUT    /api/notifications/:id               → 更新通知
DELETE /api/notifications/:id               → 删除通知
POST   /api/notifications/:id/test          → 测试通知
PUT    /api/system/password                 → 修改密码
GET    /api/system/export                   → 导出数据
```

### 3.2 新增 API（前端需要新增对接）

```
GET    /api/commands/:id                    → 命令详情
GET    /api/agents/:id/commands             → Agent 命令列表（支持 status、type、limit 查询参数）
POST   /api/storage/test                    → 测试未保存的存储配置
POST   /api/storage/:id/test                → 测试已保存的存储配置
GET    /health                              → 健康检查（不需要登录）
GET    /ready                               → 就绪检查（不需要登录）
```

### 3.3 已有但未对接的 API

```
POST   /api/agents/:id/backup-now           → 手动备份（Service 已定义，页面未调用）
```

### 3.4 API 返回格式变化

**`POST /api/agents/:id/backup-now`** 返回格式已变更：

```json
{
  "ok": true,
  "data": {
    "command_id": "uuid",
    "message_id": "uuid"
  }
}
```

HTTP 状态码为 `202 Accepted`。无论 Agent 是否在线都返回成功，离线时命令排队等待。

**`POST /api/agents/:id/restore`** 返回格式已变更：

```json
{
  "ok": true,
  "data": {
    "message": "restore started" | "restore queued",
    "command_id": "uuid",
    "message_id": "uuid"
  }
}
```

**`POST /api/agents/:id/snapshots/refresh`** 离线时返回 `202 Accepted`：

```json
{
  "ok": true,
  "data": {
    "message": "snapshot refresh queued",
    "command_id": "uuid",
    "message_id": "uuid"
  }
}
```

**`GET /api/tasks`** 返回的任务记录新增字段：

```json
{
  "command_id": "uuid",
  "policy_id": "uuid",
  "storage_id": "uuid",
  "started_at": "datetime",
  "updated_at": "datetime"
}
```

任务状态值扩展为：`pending`、`running`、`success`、`failed`、`timeout`。

**`GET /api/commands/:id`** 返回命令详情：

```json
{
  "ok": true,
  "data": {
    "id": "uuid",
    "agent_id": "uuid",
    "type": "backup_now | restore_req | policy_push | snapshot_list_req",
    "status": "pending | dispatched | running | succeeded | failed | timeout",
    "message_id": "uuid",
    "result": "json string",
    "error_message": "string",
    "attempts": 0,
    "policy_id": "uuid",
    "storage_id": "uuid",
    "deadline_at": "datetime",
    "dispatched_at": "datetime",
    "completed_at": "datetime",
    "created_at": "datetime",
    "updated_at": "datetime"
  }
}
```

**`POST /api/storage/test`** 请求和响应：

请求体：
```json
{
  "rclone_type": "s3",
  "rclone_config": {
    "provider": "AWS",
    "access_key_id": "...",
    "secret_access_key": "..."
  }
}
```

响应（成功）：
```json
{
  "ok": true,
  "data": {
    "ok": true,
    "latency_ms": 123,
    "checked_at": "2026-05-21T12:00:00Z"
  }
}
```

响应（连接失败）：
```json
{
  "ok": true,
  "data": {
    "ok": false,
    "latency_ms": 123,
    "error": "connection refused",
    "checked_at": "2026-05-21T12:00:00Z"
  }
}
```

**`GET /health`** 响应：

```json
{
  "ok": true,
  "status": "healthy"
}
```

**`GET /ready`** 响应：

成功：
```json
{
  "ok": true,
  "status": "ready"
}
```

失败（HTTP 503）：
```json
{
  "ok": false,
  "error": "not ready"
}
```

## 4. 类型定义

### 4.1 更新 `types/task.ts`

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

### 4.2 新增 `types/command.ts`

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

### 4.3 新增 `types/health.ts`

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

## 5. Service 层

### 5.1 新增 `services/commands.ts`

```typescript
export const getCommand = (id: string) => apiGet<AgentCommand>(`/api/commands/${id}`);

export const listAgentCommands = (agentId: string, filters: CommandFilters = {}) =>
  apiGet<AgentCommand[]>(`/api/agents/${agentId}/commands${toQuery(filters)}`);
```

### 5.2 新增 `services/health.ts`

`/health` 和 `/ready` 不在 `/api` 前缀下且不需要认证，Service 层需要直接 fetch 这两个路径（不走 `apiGet` 封装，或确保 `apiGet` 能处理非 `/api` 路径）。

```typescript
export const checkHealth = () => fetch("/health").then(r => r.json()) as Promise<HealthResponse>;
export const checkReady = () => fetch("/ready").then(r => r.json()) as Promise<ReadyResponse>;
```

### 5.3 更新 `services/storage.ts`

新增：

```typescript
export const testUnsavedStorage = (body: { rclone_type: string; rclone_config: Record<string, string> }) =>
  apiPost<StorageTestResult>("/api/storage/test", body);

export const testSavedStorage = (id: string) =>
  apiPost<StorageTestResult>(`/api/storage/${id}/test`);
```

### 5.4 更新 `services/agents.ts`

更新 `backupNow` 返回类型：

```typescript
export const backupNow = (id: string) =>
  apiPost<{ command_id: string; message_id: string }>(`/api/agents/${id}/backup-now`);
```

## 6. StatusBadge 扩展

当前 StatusBadge 支持的状态：`online | offline | success | failed | running | syncing | unsynced | pending`。

新增状态映射：

| 状态 | 显示文本 | 用途 |
|:---|:---|:---|
| `timeout` | 超时 | 任务和命令超时 |
| `dispatched` | 已下发 | 命令已发送到 Agent WebSocket |
| `succeeded` | 成功 | 命令执行成功 |
| `queued` | 已排队 | Agent 离线时命令排队等待 |

同时更新 `pending` 状态的显示文本为"等待中"（取代当前的"待同步"），使其在命令和任务上下文中语义正确。

`StatusType` 联合类型扩展为：

```typescript
type StatusType = "online" | "offline" | "success" | "failed" | "running" | "syncing" | "unsynced" | "pending" | "timeout" | "dispatched" | "succeeded" | "queued";
```

## 7. 节点详情页

路径：`pages/nodes/node-detail-page.tsx`

### 7.1 手动备份按钮

在节点详情页头部（节点名称和状态旁边），新增"立即备份"按钮：

- 调用 `backupNow(agentId)`。
- 操作前弹出确认框。
- 成功后使用 toast 提示：
  - Agent 在线时："备份命令已下发"。
  - Agent 离线时："备份命令已排队，Agent 上线后将自动执行"。
- 成功后自动刷新任务列表和命令列表。
- 按钮在请求期间禁用并显示 loading。

判断 Agent 是否在线的依据是 `agent.status === "online"`，这是从 `GET /api/agents/:id` 获取的字段。

### 7.2 修复快照 Tab 恢复按钮

当前快照表格中的"恢复"是 `<TableCell>` 纯文本，无法点击。修改为：

- 使用 `<Button variant="ghost" size="sm">` 替代纯文本。
- 点击后弹出 Sheet 或 Dialog：
  - 输入目标恢复路径。
  - 显示 snapshot_id 和包含路径。
  - 警告文本："此操作会将快照中的文件恢复到目标路径，已有文件可能被覆盖。"
  - 确认 checkbox："我已了解此操作的风险"。
  - 确认按钮只在 checkbox 选中后可用。
- 调用 `POST /api/agents/:id/restore`，传入 `{ snapshot_id, target_path }`。
- 成功后 toast 提示：根据返回的 `message` 字段显示"恢复已开始"或"恢复命令已排队"。
- 成功后关闭弹窗，自动刷新任务列表。

交互模式可参考 `pages/snapshots/snapshots-page.tsx` 中已有的恢复实现。

### 7.3 修复任务 Tab 详情按钮

当前任务表格中的"详情"是纯文本 `<TableCell>`。修改为：

- 使用 `<Button variant="ghost" size="sm">` 替代纯文本。
- 点击后展开当前行（与 `pages/tasks/tasks-page.tsx` 一致的展开行模式），显示：
  - `error_log`（如果有，使用错误背景高亮）
  - `snapshot_id`
  - `command_id`（如果有）
  - `duration_ms`（格式化为秒）
  - `started_at` 和 `finished_at`
- 或者采用更简单的方式：点击跳转到 `/tasks?agent_id=xxx`。

推荐使用展开行方式，与主任务页体验一致。

### 7.4 新增"命令队列" Tab

在节点详情页 Tabs 中新增第 6 个 Tab"命令"：

- 调用 `GET /api/agents/:id/commands`。
- 表格列：命令类型（中文映射）、状态、创建时间、完成时间、尝试次数、错误信息。
- 命令类型中文映射：
  - `backup_now` → 手动备份
  - `restore_req` → 恢复
  - `policy_push` → 策略下发
  - `snapshot_list_req` → 快照刷新
- 当有 pending/dispatched/running 状态的命令时，每 5 秒自动刷新。
- 提供手动刷新按钮。

TabsList grid 从 `grid-cols-5` 改为 `grid-cols-6`。

## 8. 存储配置页

路径：`pages/storage/storage-page.tsx`

### 8.1 创建/编辑表单中的连接测试

在存储创建和编辑的表单 Sheet 底部，"保存"按钮旁边，新增"测试连接"按钮：

- **创建模式**（未保存）：调用 `POST /api/storage/test`，传入当前表单中的 `rclone_type` 和 `rclone_config`。
- **编辑模式**（已保存）：调用 `POST /api/storage/:id/test`。
- 按钮在请求期间显示 loading。
- 测试结果在按钮下方内联展示：
  - 成功：显示通过图标 + 延迟毫秒数。
  - 失败：显示失败图标 + 错误信息。
- 测试结果不阻塞表单提交。
- 在编辑模式下，如果用户修改了配置但尚未保存，"测试连接"应该使用 `POST /api/storage/test` 传入修改后的值，而不是 `POST /api/storage/:id/test`（因为后者测试的是数据库中的旧值）。

### 8.2 存储列表操作列

在存储列表的操作下拉菜单中，新增"测试连接"选项：

- 调用 `POST /api/storage/:id/test`。
- 测试结果使用 toast 展示。

## 9. 任务历史页

路径：`pages/tasks/tasks-page.tsx`

### 9.1 状态筛选扩展

筛选器的状态下拉框新增选项：
- "等待中"（`pending`）
- "超时"（`timeout`）

完整选项：全部、等待中、运行中、成功、失败、超时。

### 9.2 展开行增强

当前展开行显示 message_id、snapshot_id、error_log。新增以下字段的展示：

- `command_id`（如果有值，显示为可点击链接或标签）
- `started_at` 和 `finished_at`（格式化为日期时间）
- `policy_id`（如果有值，显示标签）
- `storage_id`（如果有值，显示标签）

### 9.3 自动刷新

- 检测当前查询结果中是否有 `status === "pending"` 或 `status === "running"` 的任务。
- 如果有，使用 `refetchInterval: 5000` 自动刷新。
- 所有任务都完成（无 pending/running）时，停止自动刷新。
- 使用 react-query 的 `refetchInterval` 配置实现，逻辑为：

```typescript
refetchInterval: (query) => {
  const data = query.state.data;
  const hasActive = data?.some(t => t.status === "pending" || t.status === "running");
  return hasActive ? 5000 : false;
}
```

## 10. 仪表盘

路径：`pages/dashboard/dashboard-page.tsx`

### 10.1 系统就绪状态

新增一个统计卡片（放在现有 4 个卡片之前或之后），调用 `GET /ready`：

- 就绪时：显示绿色图标 + "系统就绪"。
- 不就绪时：显示红色图标 + "系统未就绪" + 错误原因。
- 请求失败时（如网络错误）：显示黄色图标 + "无法连接服务器"。

### 10.2 任务表格适配

最近任务表格需要适配新状态：
- StatusBadge 已经通过 Part 1 的扩展支持新状态。
- 无需额外修改，只要 StatusBadge 正确处理所有状态值即可。

## 11. 系统管理页

路径：`pages/system/system-page.tsx`

### 11.1 系统健康状态卡片

在现有卡片（修改密码、数据导出）之前，新增一个"系统状态"卡片：

- 调用 `GET /health` 和 `GET /ready`。
- 展示两行状态：
  - 服务进程：显示 `/health` 的结果（如果页面能加载，基本就是健康的）。
  - 服务就绪：显示 `/ready` 的结果（数据库、master key、数据目录是否正常）。
- 每 30 秒自动刷新。
- 不就绪时显示红色状态和错误信息。

## 12. 全局 UX 改进

### 12.1 Toast 通知

当前项目中没有统一的 toast 组件。需要：

- 引入 toast 组件（使用 shadcn/ui 的 toast 或 sonner）。
- 将以下场景从 `alert()` 或无反馈改为 toast：
  - 手动备份成功/失败
  - 恢复操作成功/失败
  - 存储连接测试结果
  - 节点删除成功
  - 策略创建/更新/删除成功
  - 存储创建/更新/删除成功
  - 文件浏览器路径复制（当前使用 `alert()`）

### 12.2 加载更多模式

后端 tasks API 和 commands API 支持 `limit` 参数。前端可以实现"加载更多"模式：

- 默认加载 50 条。
- 列表底部显示"加载更多"按钮。
- 点击后增加 limit 再次请求（如 limit=100）。
- 当返回数据条数少于请求的 limit 时，隐藏按钮。

这是轻量级的分页方案，不需要后端新增 offset 参数。

## 13. 文件变更清单

### 新增文件

```
web/src/types/command.ts
web/src/types/health.ts
web/src/services/commands.ts
web/src/services/health.ts
```

### 修改文件

```
web/src/types/task.ts                           — 扩展 TaskHistory 字段和状态类型
web/src/services/agents.ts                      — 更新 backupNow 返回类型
web/src/services/storage.ts                     — 新增 testUnsavedStorage、testSavedStorage
web/src/components/status-badge.tsx             — 新增 timeout、dispatched、succeeded、queued 状态
web/src/pages/nodes/node-detail-page.tsx        — 手动备份按钮、修复恢复/详情按钮、新增命令 Tab
web/src/pages/storage/storage-page.tsx          — 连接测试功能
web/src/pages/tasks/tasks-page.tsx              — 新状态筛选、展开行增强、自动刷新
web/src/pages/dashboard/dashboard-page.tsx      — 系统就绪卡片
web/src/pages/system/system-page.tsx            — 系统健康状态卡片
web/src/pages/snapshots/snapshots-page.tsx      — 适配 refresh 离线排队响应
```

### Toast 相关（新增或修改）

如果使用 sonner 或 shadcn toast，还需要新增 toast provider 组件并在 app.tsx 或 layout 中注册。

## 14. 实施要求

### 14.1 给 Gemini 的约束

- **不要限制 UI 具体样式**：颜色、间距、阴影、动画等视觉细节由 Gemini 自行决定。
- **不要指定 shadcn 组件的具体 variant**：本文档中提到的 `variant="ghost"` 等仅为示意，Gemini 可以根据整体设计自行选择。
- **保持现有代码风格**：遵循项目已有的 React 模式（react-query、shadcn/ui、lucide-react 图标、date-fns 格式化、zhCN locale）。
- **保持中文 UI**：所有用户可见文本使用中文。
- **不要引入新的 CSS 框架**：继续使用 Tailwind CSS + shadcn/ui。

### 14.2 测试要求

每个功能修改后：

```bash
cd web && npm test
cd web && npm run build
```

两者都必须通过。

### 14.3 验收标准

1. `backupNow` 在节点详情页可用，Agent 在线和离线时都能成功调用。
2. 节点详情页快照 Tab 的"恢复"按钮可点击并完成恢复流程。
3. 节点详情页任务 Tab 的"详情"按钮可点击并展示详细信息。
4. 节点详情页有"命令"Tab 展示命令队列。
5. 存储配置页在创建/编辑时可以测试连接。
6. 存储列表可以快速测试已保存配置。
7. 任务历史页支持 pending 和 timeout 状态筛选。
8. 任务历史页有进行中任务时自动刷新。
9. 仪表盘展示系统就绪状态。
10. 系统管理页展示系统健康和就绪状态。
11. 所有操作使用 toast 反馈替代 alert()。
12. 全部 TypeScript 类型安全，无 any 类型泄漏。
13. `npm test` 和 `npm run build` 通过。
