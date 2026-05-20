# VaultFleet 前端 MVP 设计方案

> 日期：2026-05-19
> 状态：给 Gemini 的前端设计输入
> 读者：Gemini，前端设计/实现者

## 1. 目标

为 VaultFleet 做第一版真正可用的 Web 控制台，替换当前嵌入在 Go 后端里的占位 HTML。

前端要让 VaultFleet 看起来像一个可用的自托管备份控制面板，而不是演示壳。第一版应围绕核心产品承诺展开：

> 通过一个 Master 统一管理多台 Linux/VPS 节点上的 restic 备份，支持简单 Agent 安装、集中策略、快照、恢复和任务历史。

这不是营销站。用户登录后的第一屏应该是运维控制台。

## 2. 产品定位

VaultFleet 是一个聚焦 `restic + rclone` 的多节点备份控制面。

它不应该伪装成覆盖所有备份类型的通用备份平台。UI 需要持续强化这些概念：

- Master 负责策略、元数据和管理入口。
- Agent 在本机执行备份和恢复。
- 备份数据从 Agent 直接写入远端存储。
- 每个 Agent 有自己的 restic 仓库路径和密码。
- 快照查看和恢复流程是一等功能。

整体体验应更接近 Nezha、Komari 这类自托管基础设施面板，而不是重营销感的 SaaS 首页。

## 3. 固定前端技术栈

前端源码固定放在 `web/`。

技术栈是硬约束，Gemini 不得替换为其他框架、状态管理方案、组件库或图标库。

固定技术栈：

- React 19
- Vite
- TypeScript
- React Router
- TanStack Query
- Tailwind CSS
- shadcn/ui
- lucide-react 图标

构建和部署方式：

- 开发环境：Vite dev server 代理 `/api`、`/ws`、`/install.sh` 到 Go Master。
- 生产环境：`web/dist` 通过 `go:embed` 嵌入 Go Master。
- 生产构建先产出嵌入前端的 Go Master 二进制，再由 Docker 镜像复用该二进制。
- 直接运行二进制和 Docker 运行使用同一套前端产物。

## 4. 当前后端现实

当前仓库还没有真正的前端项目。`web/` 只有 `.gitkeep`。

当前 UI 是一个大型占位 HTML 字符串，位置在：

- `internal/master/api/frontend.go`

现有后端 API 已经足够支撑前端 MVP：

- 初始化和登录
- 节点 CRUD 和注册 token 重新生成
- 存储配置 CRUD
- 备份策略 CRUD
- 远端目录浏览
- 立即备份命令
- 任务历史
- 快照列表和刷新
- 恢复请求
- 通知配置 CRUD 和测试
- 系统导出和密码修改

重要限制：API 响应结构还不完全统一。多数接口返回 `{ "ok": true, "data": ... }`，但部分接口会直接返回数组或 `{ "error": "..." }`。前端必须在统一 HTTP 层做响应归一化。

## 5. 信息架构

主导航：

1. Dashboard
2. Nodes
3. Storage
4. Policies
5. Tasks
6. Snapshots
7. Notifications
8. System

固定路由：

```text
/login
/setup
/
/nodes
/nodes/:agentId
/storage
/policies
/tasks
/snapshots
/notifications
/system
```

`/` 在认证后进入 Dashboard。

## 6. 核心页面

### 6.1 初始化与登录

目的：

- 判断展示初始化、登录，还是已认证控制台。

流程：

1. 调用 `GET /api/auth/check`。
2. 如果 `initialized=false`，展示初始化表单。
3. 如果已初始化但未登录，展示登录表单。
4. 如果已登录，进入控制台。

UI 要求：

- 居中的紧凑认证面板。
- 不做营销 hero。
- 清晰展示错误信息。
- 密码最小长度为 6 个字符，与后端校验一致。

后端接口：

- `GET /api/auth/check`
- `POST /api/auth/init`
- `POST /api/auth/login`

注意：

- 认证使用 HttpOnly、same-origin 的 `session` cookie。
- 前端 fetch 必须使用 `credentials: "same-origin"`。

### 6.2 Dashboard

目的：

- 给用户一个快速运维总览。

当前没有专用 Dashboard 统计接口，Dashboard 直接在前端汇总已有接口数据。

统计卡片：

- 在线节点数
- 离线节点数
- 策略总数
- 未同步策略数
- 最近成功备份数
- 最近失败任务数

信息面板：

- 最近任务历史表
- 需要关注的节点
- 各节点最新快照

主要操作：

- 添加节点
- 创建存储
- 创建策略
- 导出 Master 数据

后端接口：

- `GET /api/agents`
- `GET /api/policies`
- `GET /api/tasks?limit=20`

### 6.3 Nodes

目的：

- 管理 Agent，并展示当前状态。

列表字段：

- 名称
- 状态
- 最后在线时间
- 系统信息
- 创建时间
- 操作

操作：

- 添加节点
- 查看详情
- 删除节点
- 重新生成安装 token

添加节点流程：

1. 用户输入节点名称。
2. 前端调用 `POST /api/agents`。
3. 后端返回 `enroll_token`。
4. UI 展示安装命令：

```bash
curl -fsSL http://MASTER_HOST:8080/install.sh | bash -s -- \
  --server http://MASTER_HOST:8080 \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx
```

UI 应从 `window.location.origin` 推导 `MASTER_HOST`，并支持用户手动覆盖。

节点详情页标签：

- Overview
- Policy
- Snapshots
- Tasks
- File Browser
- Restore

后端接口：

- `GET /api/agents`
- `POST /api/agents`
- `GET /api/agents/:id`
- `DELETE /api/agents/:id`
- `POST /api/agents/:id/regenerate-token`

### 6.4 Storage

目的：

- 管理备份策略使用的 rclone 存储配置。

MVP 存储表单应支持：

- 名称
- rclone 类型
- rclone 配置字段

UI 包含以下内容：

- `s3`
- `webdav`
- `sftp`
- `local`
- 自定义 rclone 类型
- 高级 key-value 编辑器

敏感字段从后端返回时应显示为 `[redacted]`。

后端接口：

- `GET /api/storage`
- `POST /api/storage`
- `GET /api/storage/:id`
- `PUT /api/storage/:id`
- `DELETE /api/storage/:id`

已知缺口：

- 目前没有存储连接测试接口。
- 目前没有 rclone backend metadata 接口。

MVP UI 只展示保存操作。不要渲染“测试连接”按钮，直到后端接口存在。

### 6.5 Policies

目的：

- 定义每个 Agent 备份哪些目录、写入哪个 restic 仓库，以及保留策略。

策略表单：

- Agent 选择器
- Storage 选择器
- 仓库路径
- 备份目录
- 排除规则
- Cron 调度
- 保留策略：
  - keep last
  - keep daily
  - keep weekly
  - keep monthly
- restic 密码字段：
  - 留空表示由后端生成。
  - 当后端在创建时返回生成的密码时，UI 只展示一次，并提示用户该密码会加密存储在 Master。

目录选择：

- 当选中的 Agent 在线时，支持浏览远端目录。
- 当 Agent 离线时，仅支持手动输入路径。

列表字段：

- Agent
- Storage
- 仓库路径
- 调度
- 同步状态
- 更新时间
- 操作

后端接口：

- `GET /api/policies`
- `GET /api/policies?agent_id=:id`
- `POST /api/policies`
- `GET /api/policies/:id`
- `PUT /api/policies/:id`
- `DELETE /api/policies/:id`
- `POST /api/agents/:id/browse`

重要 UX 规则：

- `synced=false` 表示策略已存在于 Master，但 Agent 还没有确认。
- 这个状态需要在节点详情页和策略列表中明显展示。

### 6.6 Tasks

目的：

- 展示备份和恢复执行历史。

筛选项：

- Agent
- 类型：backup / restore
- 状态：success / failed / running
- Limit

列表字段：

- Agent
- 类型
- 状态
- Snapshot ID
- 开始时间
- 结束时间
- 耗时
- Repo size
- 错误摘要

行详情：

- 支持展开完整错误日志。
- 展示 message ID 方便排查。

操作：

- 对指定 Agent 触发立即备份。

后端接口：

- `GET /api/tasks`
- `GET /api/tasks?agent_id=:id`
- `GET /api/tasks?type=backup`
- `GET /api/tasks?status=failed`
- `POST /api/agents/:id/backup-now`

### 6.7 Snapshots

目的：

- 展示 restic 快照，并让恢复入口容易找到。

Snapshots 页面：

- 提供 Agent 选择器。
- 仅展示所选 Agent 的快照列表。
- 不做跨节点聚合。

节点快照：

- 快照 ID
- 时间
- 路径
- 大小
- 操作：
  - 从 Agent 刷新快照
  - 恢复

后端接口：

- `GET /api/agents/:id/snapshots`
- `POST /api/agents/:id/snapshots/refresh`

刷新行为：

- 需要 Agent 在线。
- 需要展示 loading 状态和超时错误。

### 6.8 Restore

目的：

- 从选中的快照发起恢复。

恢复表单：

- Snapshot 选择器
- 目标路径
- 确认复选框

安全提示：

- 明确恢复动作会在 Agent 所在机器上执行。
- 明确目标路径必须可被 Agent 进程写入。

后端接口：

- `POST /api/agents/:id/restore`

请求体：

```json
{
  "snapshot_id": "abc123",
  "target_path": "/restore/target"
}
```

恢复开始后：

- 展示已接受状态和 message ID。
- 链接到按 `type=restore` 筛选的 Tasks。

已知缺口：

- 当前没有面向浏览器的详细恢复进度接口。
- MVP 通过任务历史展示 Agent 上报后的最终结果。

### 6.9 Notifications

目的：

- 配置失败、离线等事件通知。

支持类型：

- `telegram`
- `webhook`

表单：

- 类型选择器
- 事件选择器
- 基于类型展示配置字段
- 测试按钮

通知事件：

- backup_failed
- agent_offline

后端接口：

- `GET /api/notifications`
- `POST /api/notifications`
- `GET /api/notifications/:id`
- `PUT /api/notifications/:id`
- `DELETE /api/notifications/:id`
- `POST /api/notifications/:id/test`

已知响应差异：

- `GET /api/notifications` 当前直接返回数组，和多数接口不同。

### 6.10 System

目的：

- 提供基础管理维护能力。

区块：

- 修改密码
- 导出 Master 数据
- 系统信息接口存在后再展示构建版本

后端接口：

- `GET /api/system/export`
- `PUT /api/system/password`

已知缺口：

- 目前没有 logout 接口。
- 目前没有 system info/version 接口。

## 7. 视觉设计边界

以 Gemini 输出的 UX/组件设计稿为准。本方案不额外添加 Gemini 未指定的配色、排版、空间密度或视觉装饰要求。

必须保留的边界：

- 不设计 landing page。
- 不展示后端不存在或无法工作的功能入口。
- 状态信息必须清晰区分，例如在线、离线、成功、失败、运行中、未同步。
- 破坏性操作必须有确认机制。
- 手机端至少能查看状态和列表。

## 8. 关键流程

### 首次运行

1. 用户打开 Master URL。
2. UI 调用 `/api/auth/check`。
3. UI 展示初始化表单。
4. 用户创建管理员。
5. UI 进入 Dashboard。

### 添加第一个节点

1. 用户打开 Nodes。
2. 用户点击 Add Node。
3. UI 创建 Agent 记录。
4. UI 展示安装命令。
5. 用户在服务器上执行命令。
6. Agent 连接后，节点状态从 offline 变为 online。

### 配置第一个备份

1. 用户创建 Storage。
2. 用户为在线节点创建 Policy。
3. 用户通过手动输入目录或远端目录浏览器选择目录。
4. Policy 显示为 unsynced。
5. Agent 确认策略。
6. Policy 变为 synced。

### 立即运行备份

1. 用户打开节点详情或 Tasks。
2. 用户点击 Backup Now。
3. UI 展示已接受状态和 message ID。
4. Agent 上报结果后，任务历史更新。
5. 用户刷新快照列表。

### 恢复快照

1. 用户打开 Snapshots。
2. 用户选择快照。
3. 用户输入目标路径。
4. UI 要求确认恢复。
5. 恢复任务创建为 running。
6. 任务历史展示最终状态。

## 9. 前端架构

固定源码结构：

```text
web/src/
├── app.tsx
├── main.tsx
├── router.tsx
├── services/
│   ├── http.ts
│   ├── auth.ts
│   ├── agents.ts
│   ├── storage.ts
│   ├── policies.ts
│   ├── tasks.ts
│   ├── snapshots.ts
│   ├── notifications.ts
│   └── system.ts
├── types/
│   ├── api.ts
│   ├── agent.ts
│   ├── storage.ts
│   ├── policy.ts
│   ├── task.ts
│   └── notification.ts
├── layouts/
│   └── app-layout.tsx
├── pages/
│   ├── auth/
│   ├── dashboard/
│   ├── nodes/
│   ├── storage/
│   ├── policies/
│   ├── tasks/
│   ├── snapshots/
│   ├── notifications/
│   └── system/
└── components/
    ├── status-badge.tsx
    ├── empty-state.tsx
    ├── error-panel.tsx
    ├── install-command.tsx
    ├── directory-browser.tsx
    └── confirm-dialog.tsx
```

`services/http.ts` 需要：

- 始终发送 `credentials: "same-origin"`
- 归一化 `{ ok, data }`
- 兼容直接返回数组的接口
- 将 `{ error }` 响应转换为抛出的错误
- 暴露带类型的 `apiGet`、`apiPost`、`apiPut`、`apiDelete`

## 10. 需要明确保留的后端缺口

这些问题不阻塞前端 MVP，但 UI 不能假装它们已经存在：

1. 没有 logout 接口。
2. 没有 Dashboard summary 接口。
3. 没有存储连接测试接口。
4. 没有 rclone backend metadata 接口。
5. Notification list 响应结构不一致。
6. 没有面向浏览器的实时任务流。
7. 没有恢复进度接口。
8. 没有系统版本和信息接口。
9. 没有 API key 和 RBAC。
10. 没有审计日志。

前端处理方式：

- 为未来功能预留清晰位置。
- 不做无法工作的控制项。
- 只有当下一步后端能力非常明确时，才展示禁用按钮，并且文案必须明确。

## 11. MVP 验收标准

前端 MVP 达到以下效果即可验收：

- 支持新用户初始化管理员账号。
- 支持用户登录并看到 Dashboard。
- 支持用户添加节点并复制安装命令。
- 支持用户看到节点在线和离线状态。
- 支持用户创建存储配置。
- 支持用户创建备份策略。
- 支持用户浏览在线 Agent 的目录。
- 支持用户触发立即备份。
- 支持用户查看任务历史。
- 支持用户刷新并查看快照。
- 支持用户从快照发起恢复。
- 支持用户配置并测试通知。
- 支持用户导出 Master 数据。
- 生产构建产物嵌入 Go Master 二进制。

## 12. 第一版前端不做的内容

第一版前端设计不包含：

- 数据库备份 runner
- SAP HANA
- 多用户 RBAC 页面
- API key 管理
- 审计日志 UI
- 复杂图表
- 存储用量分析
- 计费和团队概念
- 主题市场
- 插件系统

## 13. 期望 Gemini 交付的内容

请让 Gemini 产出：

1. 逐页面 UX 设计。
2. 组件清单。
3. 路由地图。
4. API 集成假设。
5. 固定前端项目脚手架。
6. 默认中文 UI 文案。
7. 每个页面的空状态、加载状态和错误状态。
8. 可直接落地为 `web/` 下 React/Vite/TypeScript 项目的设计。

Gemini 不应设计 landing page。产品打开后应直接进入与当前状态匹配的 setup、login、console 页面之一。
