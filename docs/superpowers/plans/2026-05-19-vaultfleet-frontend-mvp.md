# VaultFleet Frontend MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `web/` 下实现 VaultFleet 第一版真实前端控制台，并替换当前 Go 内嵌占位 HTML。

**Architecture:** 前端源码固定在 `web/`，使用 React/Vite/TypeScript 构建单页应用；API 访问集中在 `web/src/services/`，统一处理 cookie、响应包裹和错误归一化。生产构建输出到 Go 可嵌入目录 `internal/master/api/frontend_dist/`，由 `internal/master/api/frontend.go` 使用 `go:embed` 服务静态资源和 SPA fallback。

**Tech Stack:** React 19, Vite, TypeScript, React Router, TanStack Query, Tailwind CSS, shadcn/ui, lucide-react, Go `embed`, Gin.

---

## 参考文档

- 主方案：`docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-design.md`
- UX 与组件设计：`docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-ux-component-design.md`

## 硬约束

- 不替换技术栈。
- 页面视觉风格、布局表现、配色、空间密度和组件组合方式以 Gemini 产出的 UX/组件设计稿为准。
- 如果 UX/组件设计稿已经指定风格或交互形式，按设计稿实现；如果没有指定，本 plan 不额外追加风格要求。
- 本 plan 只补充功能范围、API 集成、后端能力边界、构建接入和验收标准。
- 不设计 landing page。
- 不展示 logout 入口；后端当前没有 logout 接口。
- 不展示存储“测试连接”按钮；后端当前没有存储测试接口。
- 不展示系统版本、审计日志、API key、RBAC 页面。
- Notifications 第一版事件只允许 `backup_failed` 和 `agent_offline`。
- Snapshots 页面只展示所选 Agent 的快照，不做跨节点聚合。
- 创建和编辑流程按 UX/组件设计稿统一使用 Drawer。
- 所有 API 请求必须使用 `credentials: "same-origin"`。

## 文件结构

### 创建前端项目文件

- `web/package.json`：前端依赖、脚本。
- `web/index.html`：Vite HTML 入口。
- `web/vite.config.ts`：React 插件、dev proxy、build 输出到 `../internal/master/api/frontend_dist`。
- `web/tsconfig.json`：TypeScript 配置。
- `web/src/main.tsx`：React 入口。
- `web/src/app.tsx`：QueryClientProvider、RouterProvider、认证初始化。
- `web/src/router.tsx`：固定路由。
- `web/src/styles/globals.css`：Tailwind 全局样式。
- `web/src/services/*.ts`：API client 和各业务服务。
- `web/src/types/*.ts`：后端响应类型。
- `web/src/layouts/app-layout.tsx`：认证后控制台布局，按 UX/组件设计稿实现 Sidebar Layout 和 TopBar。
- `web/src/components/*.tsx`：共享组件。
- `web/src/pages/**`：页面实现。
- `web/src/test/setup.ts`：前端测试 setup。

### 修改 Go 和构建文件

- `internal/master/api/frontend.go`：删除占位 HTML，改为嵌入 `frontend_dist`。
- `internal/master/api/frontend_dist/index.html`：提交一个最小 fallback，保证未构建前端时 Go 测试仍能编译。
- `internal/master/api/frontend_test.go`：从占位 HTML 断言改为静态资源和 SPA fallback 断言。
- `Makefile`：新增 `frontend-install`、`frontend-build`，并让 `build-master` 先构建前端。
- `build/Dockerfile`：新增 Node 前端构建阶段，并把构建产物复制进 Go builder。
- `.gitignore`：忽略生成的 `frontend_dist` 产物，但保留 fallback `index.html`。

---

### Task 1: 修正文档中的通知事件范围

**Files:**
- Modify: `docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-ux-component-design.md`
- Modify: `docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-design.md`

- [ ] **Step 1: 验证 UX 文档事件范围**

Run:

```bash
rg -n "backup_failed|agent_offline|backup_success|restore_failed|policy_sync_failed" docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-ux-component-design.md
```

Expected:

```text
Only backup_failed and agent_offline appear in the Notifications section.
```

- [ ] **Step 2: 验证主方案事件范围**

Run:

```bash
rg -n "backup_failed|agent_offline|backup_success|restore_failed|policy_sync_failed" docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-design.md
```

Expected:

```text
Only backup_failed and agent_offline appear in the Notifications section.
```

- [ ] **Step 3: Commit**

Run:

```bash
git add docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-design.md docs/superpowers/specs/2026-05-19-vaultfleet-frontend-mvp-ux-component-design.md
git commit -m "docs: align frontend notification events with backend"
```

Expected: commit succeeds.

---

### Task 2: 初始化 Vite React 项目

**Files:**
- Delete: `web/.gitkeep`
- Create: `web/package.json`
- Create: `web/index.html`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/src/main.tsx`
- Create: `web/src/app.tsx`
- Create: `web/src/router.tsx`
- Create: `web/src/styles/globals.css`
- Create: `web/src/test/setup.ts`

- [ ] **Step 1: 删除占位文件**

Use `apply_patch`:

```diff
*** Begin Patch
*** Delete File: web/.gitkeep
*** End Patch
```

- [ ] **Step 2: 创建 `package.json`**

Use `apply_patch`:

```diff
*** Begin Patch
*** Add File: web/package.json
+{
+  "name": "vaultfleet-web",
+  "private": true,
+  "version": "0.1.0",
+  "type": "module",
+  "scripts": {
+    "dev": "vite --host 0.0.0.0",
+    "build": "tsc -b && vite build",
+    "preview": "vite preview --host 0.0.0.0",
+    "test": "vitest run",
+    "test:watch": "vitest"
+  },
+  "dependencies": {
+    "@tanstack/react-query": "latest",
+    "class-variance-authority": "latest",
+    "clsx": "latest",
+    "lucide-react": "latest",
+    "react": "19",
+    "react-dom": "19",
+    "react-router-dom": "latest",
+    "tailwind-merge": "latest"
+  },
+  "devDependencies": {
+    "@testing-library/jest-dom": "latest",
+    "@testing-library/react": "latest",
+    "@testing-library/user-event": "latest",
+    "@types/react": "latest",
+    "@types/react-dom": "latest",
+    "@vitejs/plugin-react": "latest",
+    "jsdom": "latest",
+    "tailwindcss": "latest",
+    "typescript": "latest",
+    "vite": "latest",
+    "vitest": "latest"
+  }
+}
*** End Patch
```

- [ ] **Step 3: 安装依赖**

Run:

```bash
cd web && npm install
```

Expected:

```text
added ... packages
```

- [ ] **Step 4: 初始化 shadcn/ui**

Run:

```bash
cd web && npx shadcn@latest init
```

Use these choices:

```text
CSS file: src/styles/globals.css
CSS variables: yes
React Server Components: no
Import alias: @/*
```

Style and base color are Gemini design choices. Do not override them in this plan.

Expected: shadcn creates component config and keeps Tailwind wired into `src/styles/globals.css`.

- [ ] **Step 5: 添加基础 shadcn 组件**

Run:

```bash
cd web && npx shadcn@latest add button input textarea select checkbox badge table dialog sheet tabs dropdown-menu alert form separator skeleton
```

Expected: component files appear under `web/src/components/ui/`.

- [ ] **Step 6: 创建 Vite 配置**

Use `apply_patch`:

```diff
*** Begin Patch
*** Add File: web/vite.config.ts
+import react from "@vitejs/plugin-react";
+import { defineConfig } from "vite";
+import path from "node:path";
+
+export default defineConfig({
+  plugins: [react()],
+  resolve: {
+    alias: {
+      "@": path.resolve(__dirname, "./src")
+    }
+  },
+  server: {
+    port: 5173,
+    proxy: {
+      "/api": "http://127.0.0.1:8080",
+      "/ws": {
+        target: "ws://127.0.0.1:8080",
+        ws: true
+      },
+      "/install.sh": "http://127.0.0.1:8080"
+    }
+  },
+  build: {
+    outDir: "../internal/master/api/frontend_dist",
+    emptyOutDir: true
+  },
+  test: {
+    environment: "jsdom",
+    setupFiles: "./src/test/setup.ts"
+  }
+});
*** End Patch
```

- [ ] **Step 7: 创建 TypeScript 配置和 HTML 入口**

Use `apply_patch`:

```diff
*** Begin Patch
*** Add File: web/tsconfig.json
+{
+  "compilerOptions": {
+    "target": "ES2022",
+    "useDefineForClassFields": true,
+    "lib": ["ES2022", "DOM", "DOM.Iterable"],
+    "allowJs": false,
+    "skipLibCheck": true,
+    "esModuleInterop": true,
+    "allowSyntheticDefaultImports": true,
+    "strict": true,
+    "forceConsistentCasingInFileNames": true,
+    "module": "ESNext",
+    "moduleResolution": "Node",
+    "resolveJsonModule": true,
+    "isolatedModules": true,
+    "noEmit": true,
+    "jsx": "react-jsx",
+    "baseUrl": ".",
+    "paths": {
+      "@/*": ["src/*"]
+    }
+  },
+  "include": ["src", "vite.config.ts"]
+}
*** Add File: web/index.html
+<!doctype html>
+<html lang="zh-CN">
+  <head>
+    <meta charset="UTF-8" />
+    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
+    <title>VaultFleet</title>
+  </head>
+  <body>
+    <div id="root"></div>
+    <script type="module" src="/src/main.tsx"></script>
+  </body>
+</html>
*** End Patch
```

- [ ] **Step 8: 创建 React 入口骨架**

Use `apply_patch`:

```diff
*** Begin Patch
*** Add File: web/src/main.tsx
+import React from "react";
+import ReactDOM from "react-dom/client";
+import { App } from "./app";
+import "./styles/globals.css";
+
+ReactDOM.createRoot(document.getElementById("root")!).render(
+  <React.StrictMode>
+    <App />
+  </React.StrictMode>
+);
*** Add File: web/src/app.tsx
+import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
+import { RouterProvider } from "react-router-dom";
+import { router } from "./router";
+
+const queryClient = new QueryClient({
+  defaultOptions: {
+    queries: {
+      retry: 1,
+      refetchOnWindowFocus: false
+    }
+  }
+});
+
+export function App() {
+  return (
+    <QueryClientProvider client={queryClient}>
+      <RouterProvider router={router} />
+    </QueryClientProvider>
+  );
+}
*** Add File: web/src/router.tsx
+import { createBrowserRouter } from "react-router-dom";
+
+export const router = createBrowserRouter([
+  {
+    path: "*",
+    lazy: async () => {
+      const { AuthGate } = await import("./pages/auth/auth-gate");
+      return { Component: AuthGate };
+    }
+  }
+]);
*** Add File: web/src/test/setup.ts
+import "@testing-library/jest-dom/vitest";
*** End Patch
```

- [ ] **Step 9: 运行前端基础验证**

Run:

```bash
cd web && npm run build
```

Expected:

```text
vite v...
✓ built
```

- [ ] **Step 10: Commit**

Run:

```bash
git add web package-lock.json internal/master/api/frontend_dist
git commit -m "feat: scaffold vaultfleet web app"
```

Expected: commit succeeds.

---

### Task 3: 实现 API Client、类型和服务层

**Files:**
- Create: `web/src/types/api.ts`
- Create: `web/src/types/agent.ts`
- Create: `web/src/types/storage.ts`
- Create: `web/src/types/policy.ts`
- Create: `web/src/types/task.ts`
- Create: `web/src/types/snapshot.ts`
- Create: `web/src/types/notification.ts`
- Create: `web/src/services/http.ts`
- Create: `web/src/services/auth.ts`
- Create: `web/src/services/agents.ts`
- Create: `web/src/services/storage.ts`
- Create: `web/src/services/policies.ts`
- Create: `web/src/services/tasks.ts`
- Create: `web/src/services/snapshots.ts`
- Create: `web/src/services/notifications.ts`
- Create: `web/src/services/system.ts`
- Create: `web/src/services/http.test.ts`

- [ ] **Step 1: 定义响应归一化规则**

Implement `web/src/services/http.ts` with these rules:

```ts
export class ApiError extends Error {
  status: number;
  body: unknown;

  constructor(message: string, status: number, body: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin"
  });
  const body = await parseBody(response);

  if (!response.ok) {
    throw new ApiError(errorMessage(body, response.statusText), response.status, body);
  }
  if (isObject(body) && body.ok === false) {
    throw new ApiError(errorMessage(body, "request failed"), response.status, body);
  }
  if (isObject(body) && "error" in body) {
    throw new ApiError(errorMessage(body, "request failed"), response.status, body);
  }
  if (isObject(body) && body.ok === true && "data" in body) {
    return body.data as T;
  }
  return body as T;
}

export const apiGet = <T>(path: string) => request<T>(path);
export const apiPost = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) });
export const apiPut = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: "PUT", body: body === undefined ? undefined : JSON.stringify(body) });
export const apiDelete = (path: string) => request<void>(path, { method: "DELETE" });

async function parseBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function errorMessage(body: unknown, fallback: string): string {
  if (isObject(body) && typeof body.error === "string") return body.error;
  if (typeof body === "string" && body.trim()) return body;
  return fallback;
}
```

- [ ] **Step 2: 添加 HTTP client 测试**

Create `web/src/services/http.test.ts` covering:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { apiGet, ApiError } from "./http";

afterEach(() => vi.restoreAllMocks());

describe("http client", () => {
  it("unwraps ok data envelopes", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true, data: [1] }), { status: 200 })));
    await expect(apiGet<number[]>("/api/test")).resolves.toEqual([1]);
  });

  it("accepts raw arrays", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ id: "n1" }]), { status: 200 })));
    await expect(apiGet<Array<{ id: string }>>("/api/notifications")).resolves.toEqual([{ id: "n1" }]);
  });

  it("throws on direct error response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "invalid event" }), { status: 400 })));
    await expect(apiGet("/api/test")).rejects.toMatchObject({ name: "ApiError", message: "invalid event" });
  });

  it("sends same-origin credentials", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true, data: {} }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await apiGet("/api/auth/check");
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/check", expect.objectContaining({ credentials: "same-origin" }));
  });
});
```

- [ ] **Step 3: 定义服务函数**

Create service files with exact endpoint names:

```ts
// auth.ts
export const checkAuth = () => apiGet<AuthCheck>("/api/auth/check");
export const initAdmin = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/init", body);
export const login = (body: AuthCredentials) => apiPost<AuthUser>("/api/auth/login", body);

// agents.ts
export const listAgents = () => apiGet<Agent[]>("/api/agents");
export const createAgent = (body: { name: string }) => apiPost<CreateAgentResponse>("/api/agents", body);
export const getAgent = (id: string) => apiGet<Agent>(`/api/agents/${id}`);
export const deleteAgent = (id: string) => apiDelete(`/api/agents/${id}`);
export const regenerateAgentToken = (id: string) => apiPost<CreateAgentResponse>(`/api/agents/${id}/regenerate-token`);
export const browseAgent = (id: string, body: BrowseRequest) => apiPost<BrowseResponse>(`/api/agents/${id}/browse`, body);
export const backupNow = (id: string) => apiPost<{ message_id: string }>(`/api/agents/${id}/backup-now`);

// storage.ts
export const listStorage = () => apiGet<StorageConfig[]>("/api/storage");
export const createStorage = (body: StorageInput) => apiPost<StorageConfig>("/api/storage", body);
export const getStorage = (id: string) => apiGet<StorageConfig>(`/api/storage/${id}`);
export const updateStorage = (id: string, body: Partial<StorageInput>) => apiPut<StorageConfig>(`/api/storage/${id}`, body);
export const deleteStorage = (id: string) => apiDelete(`/api/storage/${id}`);

// policies.ts
export const listPolicies = (agentId?: string) => apiGet<BackupPolicy[]>(agentId ? `/api/policies?agent_id=${encodeURIComponent(agentId)}` : "/api/policies");
export const createPolicy = (body: PolicyInput) => apiPost<BackupPolicy>("/api/policies", body);
export const getPolicy = (id: string) => apiGet<BackupPolicy>(`/api/policies/${id}`);
export const updatePolicy = (id: string, body: Partial<PolicyInput>) => apiPut<BackupPolicy>(`/api/policies/${id}`, body);
export const deletePolicy = (id: string) => apiDelete(`/api/policies/${id}`);

// tasks.ts
export const listTasks = (filters: TaskFilters = {}) => apiGet<TaskHistory[]>(`/api/tasks${toQuery(filters)}`);

// snapshots.ts
export const listSnapshots = (agentId: string) => apiGet<Snapshot[]>(`/api/agents/${agentId}/snapshots`);
export const refreshSnapshots = (agentId: string) => apiPost<SnapshotRefreshResponse>(`/api/agents/${agentId}/snapshots/refresh`);
export const restoreSnapshot = (agentId: string, body: RestoreRequest) => apiPost<RestoreAccepted>(`/api/agents/${agentId}/restore`, body);

// notifications.ts
export const listNotifications = () => apiGet<NotificationConfig[]>("/api/notifications");
export const createNotification = (body: NotificationInput) => apiPost<NotificationConfig>("/api/notifications", body);
export const getNotification = (id: string) => apiGet<NotificationConfig>(`/api/notifications/${id}`);
export const updateNotification = (id: string, body: Partial<NotificationInput>) => apiPut<NotificationConfig>(`/api/notifications/${id}`, body);
export const deleteNotification = (id: string) => apiDelete(`/api/notifications/${id}`);
export const testNotification = (id: string) => apiPost<{ ok: true }>(`/api/notifications/${id}/test`);

// system.ts
export const changePassword = (body: { current_password: string; new_password: string }) => apiPut<{ ok: true }>("/api/system/password", body);
export const exportSystemData = async () => {
  const response = await fetch("/api/system/export", { credentials: "same-origin" });
  if (!response.ok) throw new ApiError("export failed", response.status, await response.text());
  return response.blob();
};
```

Also implement `toQuery(filters)` in `tasks.ts` so empty filters return an empty string and non-empty filters return `?agent_id=...&type=...&status=...&limit=...`.

- [ ] **Step 4: 运行服务层测试**

Run:

```bash
cd web && npm test
```

Expected:

```text
Test Files  1 passed
Tests       4 passed
```

- [ ] **Step 5: Commit**

Run:

```bash
git add web/src/types web/src/services
git commit -m "feat: add frontend api client"
```

Expected: commit succeeds.

---

### Task 4: 实现认证门禁和应用骨架

**Files:**
- Create: `web/src/pages/auth/auth-gate.tsx`
- Create: `web/src/pages/auth/setup-page.tsx`
- Create: `web/src/pages/auth/login-page.tsx`
- Create: `web/src/layouts/app-layout.tsx`
- Modify: `web/src/router.tsx`
- Create: `web/src/components/status-badge.tsx`
- Create: `web/src/components/error-panel.tsx`
- Create: `web/src/components/empty-state.tsx`
- Create: `web/src/components/confirm-dialog.tsx`

- [ ] **Step 1: 实现 AuthGate**

Behavior:

- Calls `GET /api/auth/check`.
- `initialized=false` renders Setup page.
- `initialized=true` and `authenticated=false` renders Login page.
- Authenticated users enter the console routes.
- Setup confirmation password is local-only; submit body must be `{ username, password }`.

- [ ] **Step 2: 实现固定路由**

Routes:

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

`/` renders Dashboard after authentication.

- [ ] **Step 3: 实现 AppLayout**

Functional requirements:

- Implement the Sidebar Layout and TopBar from the UX/组件设计稿.
- Authenticated console must expose navigation to Dashboard, Nodes, Storage, Policies, Tasks, Snapshots, Notifications, System.
- TopBar must expose breadcrumb/current page, system status summary, and a “修改密码” entry.
- No logout entry.
- Responsive behavior follows the UX/组件设计稿.

- [ ] **Step 4: 添加共享基础组件**

Components:

- `StatusBadge`: visually distinguishes online, offline, success, failed, running, syncing, unsynced.
- `ErrorPanel`: shows backend `error` text.
- `EmptyState`: shows useful empty-state guidance.
- `ConfirmDialog`: required for delete and regenerate token.

- [ ] **Step 5: 验证认证和布局**

Run:

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

Run:

```bash
git add web/src
git commit -m "feat: add auth gate and app layout"
```

Expected: commit succeeds.

---

### Task 5: 实现 Dashboard 和 Nodes

**Files:**
- Create: `web/src/pages/dashboard/dashboard-page.tsx`
- Create: `web/src/pages/nodes/nodes-page.tsx`
- Create: `web/src/pages/nodes/node-detail-page.tsx`
- Create: `web/src/components/install-command.tsx`

- [ ] **Step 1: 实现 Dashboard**

Data:

- `GET /api/agents`
- `GET /api/policies`
- `GET /api/tasks?limit=200`

Metrics:

- Online nodes.
- Offline nodes.
- Synced/unsynced policies.
- 24h successful/failed task counts computed locally from `created_at` or `finished_at`.
- Recent tasks table shows latest 10 rows; clicking a row navigates to `/tasks` with query filters, not a task detail page.

- [ ] **Step 2: 实现 Nodes list**

Columns:

- Name
- Status
- Last seen
- System info
- Created at
- Actions

Actions:

- Details: `/nodes/:agentId`
- Regenerate install token: confirm dialog, then `POST /api/agents/:id/regenerate-token`
- Delete: confirm dialog, then `DELETE /api/agents/:id`

- [ ] **Step 3: 实现 Add Node Drawer**

Flow:

- User enters node name.
- Submit `POST /api/agents` with `{ "name": "..." }`.
- Show install command using returned `enroll_token`.
- `MASTER_HOST` defaults to `window.location.origin`.
- User can override master URL before copying.

Install command format:

```bash
curl -fsSL http://MASTER_HOST:8080/install.sh | bash -s -- \
  --server http://MASTER_HOST:8080 \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx
```

- [ ] **Step 4: 实现 Node Detail tabs**

Tabs:

- Overview
- Policy
- Snapshots
- Tasks
- File Browser
- Restore

Use existing page-level components where possible; do not duplicate API logic.

- [ ] **Step 5: 验证**

Run:

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

Run:

```bash
git add web/src/pages/dashboard web/src/pages/nodes web/src/components/install-command.tsx
git commit -m "feat: implement dashboard and node management"
```

Expected: commit succeeds.

---

### Task 6: 实现 Storage、Policies 和 Directory Browser

**Files:**
- Create: `web/src/pages/storage/storage-page.tsx`
- Create: `web/src/pages/policies/policies-page.tsx`
- Create: `web/src/components/directory-browser.tsx`
- Create: `web/src/components/key-value-editor.tsx`

- [ ] **Step 1: 实现 Storage 页面**

Form fields:

- `name`
- `rclone_type`
- `rclone_config`

Editor modes:

- Template mode: `s3`, `webdav`, `sftp`, `local`.
- Advanced mode: key-value editor for custom rclone fields.

Rules:

- Show `[redacted]` for redacted secret values returned by backend.
- Preserve `[redacted]` when editing an unchanged secret.
- Only render Save. Do not render Test Connection.

- [ ] **Step 2: 实现 Policies 页面**

Form body for create:

```json
{
  "agent_id": "agent-id",
  "storage_id": "storage-id",
  "repo_path": "vaultfleet/agent-id",
  "restic_password": "",
  "backup_dirs": ["/etc"],
  "exclude_patterns": ["/tmp"],
  "schedule": "0 2 * * *",
  "retention": {
    "keep_last": 7,
    "keep_daily": 7,
    "keep_weekly": 4,
    "keep_monthly": 6
  }
}
```

Rules:

- Empty `restic_password` lets backend generate one.
- If create response includes `restic_password`, show it once in a copyable warning panel.
- `synced=false` displays amber “待同步”.

- [ ] **Step 3: 实现 DirectoryBrowser**

Request:

```json
{
  "path": "/",
  "depth": 2
}
```

Response shape:

```json
{
  "path": "/",
  "entries": [
    { "path": "/etc", "type": "dir", "size": 4096 },
    { "path": "/etc/hosts", "type": "file", "size": 228 }
  ]
}
```

Rules:

- Only enabled when selected Agent is online.
- Offline Agent falls back to manual path input.
- Clicking a directory navigates deeper.
- Selecting a path inserts it into the policy backup directories field.

- [ ] **Step 4: 验证**

Run:

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 5: Commit**

Run:

```bash
git add web/src/pages/storage web/src/pages/policies web/src/components/directory-browser.tsx web/src/components/key-value-editor.tsx
git commit -m "feat: add storage and policy workflows"
```

Expected: commit succeeds.

---

### Task 7: 实现 Tasks、Snapshots 和 Restore

**Files:**
- Create: `web/src/pages/tasks/tasks-page.tsx`
- Create: `web/src/pages/snapshots/snapshots-page.tsx`
- Create: `web/src/components/restore-drawer.tsx`

- [ ] **Step 1: 实现 Tasks 页面**

Filters:

- `agent_id`
- `type`: `backup` or `restore`
- `status`: `success`, `failed`, or `running`
- `limit`

Rows:

- Agent
- Type
- Status
- Snapshot ID
- Started
- Finished
- Duration
- Repo size
- Error summary

Detail:

- Expand row to show full `error_log`.
- Show `message_id`.

- [ ] **Step 2: 实现 Snapshots 页面**

Rules:

- Top Agent selector is required.
- Call `GET /api/agents/:id/snapshots`.
- Refresh button calls `POST /api/agents/:id/snapshots/refresh`.
- Refresh is enabled only when Agent is online.
- Do not aggregate snapshots across all Agents.

- [ ] **Step 3: 实现 Restore Drawer**

Request:

```json
{
  "snapshot_id": "abc123",
  "target_path": "/restore/target"
}
```

Rules:

- Trigger from Snapshots row and Node Detail snapshot tab.
- Show selected Snapshot ID read-only.
- Require target path.
- Require “确认恢复” checkbox before submit.
- On accepted response, show `message_id` and link to `/tasks?type=restore`.
- Do not show rich progress; backend has no restore progress endpoint for browser UI.

- [ ] **Step 4: 验证**

Run:

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 5: Commit**

Run:

```bash
git add web/src/pages/tasks web/src/pages/snapshots web/src/components/restore-drawer.tsx
git commit -m "feat: add task snapshot and restore views"
```

Expected: commit succeeds.

---

### Task 8: 实现 Notifications 和 System

**Files:**
- Create: `web/src/pages/notifications/notifications-page.tsx`
- Create: `web/src/pages/system/system-page.tsx`

- [ ] **Step 1: 实现 Notifications 页面**

Rules:

- List API returns raw array: `GET /api/notifications`.
- Create/update request body uses `type`, `config`, `events`.
- Type options: `telegram`, `webhook`.
- Event options only: `backup_failed`, `agent_offline`.
- Test button calls `POST /api/notifications/:id/test`.
- Preserve `[redacted]` secret values when editing unchanged secrets.

Telegram config:

```json
{
  "bot_token": "123:token",
  "chat_id": "123456"
}
```

Webhook config:

```json
{
  "url": "https://example.test/hook",
  "headers": {
    "Authorization": "Bearer token"
  }
}
```

- [ ] **Step 2: 实现 System 页面**

Sections:

- Change password: submit `PUT /api/system/password` with `current_password` and `new_password`; confirm password is frontend-only.
- Export Master data: download `GET /api/system/export` as zip.

Do not render:

- logout
- version/build info
- audit log
- API key
- RBAC

- [ ] **Step 3: 验证**

Run:

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 4: Commit**

Run:

```bash
git add web/src/pages/notifications web/src/pages/system
git commit -m "feat: add notifications and system pages"
```

Expected: commit succeeds.

---

### Task 9: 接入 Go embed 和 SPA fallback

**Files:**
- Modify: `internal/master/api/frontend.go`
- Modify: `internal/master/api/frontend_test.go`
- Create: `internal/master/api/frontend_dist/index.html`
- Modify: `.gitignore`

- [ ] **Step 1: 添加 fallback dist 文件**

Use `apply_patch`:

```diff
*** Begin Patch
*** Add File: internal/master/api/frontend_dist/index.html
+<!doctype html>
+<html lang="zh-CN">
+  <head>
+    <meta charset="UTF-8" />
+    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
+    <title>VaultFleet</title>
+  </head>
+  <body>
+    <div id="root">VaultFleet frontend has not been built.</div>
+  </body>
+</html>
*** End Patch
```

- [ ] **Step 2: 忽略生成产物但保留 fallback**

Append to `.gitignore`:

```gitignore
# Frontend build output embedded by Go
internal/master/api/frontend_dist/*
!internal/master/api/frontend_dist/index.html
```

- [ ] **Step 3: 替换 `frontend.go` 为嵌入静态资源**

Implementation rules:

- Use `//go:embed all:frontend_dist`.
- Keep `isBackendRoute` so `/api/*` and `/ws/*` never fall through to frontend.
- Serve existing asset files directly.
- Unknown non-backend routes return `index.html` for SPA routing.
- Remove `frontendPlaceholderHTML`.

- [ ] **Step 4: 更新 Go 测试**

Update tests to assert:

- `/` returns HTML.
- `/nodes/agent-1` returns HTML.
- `/assets/missing.js` should return SPA only if Vite router path; if this behavior is ambiguous, only assert `/api/missing` and `/ws/missing` do not return HTML.
- `/api/missing` returns JSON 404 and does not contain `VaultFleet`.

- [ ] **Step 5: 运行 Go 测试**

Run:

```bash
go test ./internal/master/api -count=1
```

Expected:

```text
ok  	vaultfleet/internal/master/api
```

- [ ] **Step 6: Commit**

Run:

```bash
git add internal/master/api/frontend.go internal/master/api/frontend_test.go internal/master/api/frontend_dist/index.html .gitignore
git commit -m "feat: serve embedded frontend assets"
```

Expected: commit succeeds.

---

### Task 10: 更新 Makefile 和 Dockerfile

**Files:**
- Modify: `Makefile`
- Modify: `build/Dockerfile`

- [ ] **Step 1: 更新 Makefile**

Rules:

- Add `frontend-install`.
- Add `frontend-build`.
- Make `build-master` depend on `frontend-build`.
- Keep `test` as Go tests only.

Expected targets:

```make
.PHONY: frontend-install frontend-build build-master build-agent build-all test docker-build clean

frontend-install:
	cd web && npm install

frontend-build:
	cd web && npm run build

build-master: frontend-build
	CGO_ENABLED=1 go build $(LDFLAGS) -o bin/vaultfleet-master ./cmd/master
```

- [ ] **Step 2: 更新 Dockerfile**

Rules:

- Add Node builder stage before Go builder.
- Run `npm ci` when `package-lock.json` exists.
- Run `npm run build`.
- Copy `internal/master/api/frontend_dist` from frontend builder into Go builder before `go build`.
- Keep final runtime image Debian slim.

- [ ] **Step 3: 验证本地构建**

Run:

```bash
make build-master
```

Expected:

```text
bin/vaultfleet-master exists
```

- [ ] **Step 4: 验证 Docker 构建**

Run:

```bash
make docker-build
```

Expected: Docker image builds successfully.

- [ ] **Step 5: Commit**

Run:

```bash
git add Makefile build/Dockerfile
git commit -m "build: include frontend in master builds"
```

Expected: commit succeeds.

---

### Task 11: 最终验证

**Files:**
- Modify only files needed to fix verification failures.

- [ ] **Step 1: 前端测试**

Run:

```bash
cd web && npm test
```

Expected: all tests pass.

- [ ] **Step 2: 前端构建**

Run:

```bash
cd web && npm run build
```

Expected: Vite build succeeds and writes files to `internal/master/api/frontend_dist/`.

- [ ] **Step 3: Go 测试**

Run:

```bash
go test ./... -count=1
```

Expected: all Go packages pass.

- [ ] **Step 4: Master 构建**

Run:

```bash
make build-master
```

Expected: `bin/vaultfleet-master` exists.

- [ ] **Step 5: Start local smoke-test server**

Run:

```bash
./bin/vaultfleet-master --data-dir ./data-dev --addr :8080
```

Expected: server starts and listens on `http://127.0.0.1:8080`.

Keep this process running until Step 7 is complete.

- [ ] **Step 6: Playwright CLI browser smoke test**

Use `playwright-cli` for browser-level verification only. Do not use this step to judge or change Gemini's visual style.

Run:

```bash
playwright-cli open http://127.0.0.1:8080
playwright-cli snapshot
```

Verify from the snapshot:

- Setup page appears on first run.
- Page is not blank.
- There is no visible fatal error.

Initialize admin:

```bash
playwright-cli fill <username-field-ref> "admin"
playwright-cli fill <password-field-ref> "secret123"
playwright-cli fill <confirm-password-field-ref> "secret123"
playwright-cli click <submit-button-ref>
playwright-cli snapshot
```

Expected:

- Dashboard appears after initialization.
- No logout entry is visible.

Verify fixed routes:

```bash
playwright-cli goto http://127.0.0.1:8080/nodes
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/storage
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/policies
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/tasks
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/snapshots
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/notifications
playwright-cli snapshot
playwright-cli goto http://127.0.0.1:8080/system
playwright-cli snapshot
```

Expected:

- Each route renders the frontend without server 404.
- Each route shows the expected page identity or content.
- No route displays storage test connection, logout, version/build info, audit log, API key, or RBAC UI.

Check browser console:

```bash
playwright-cli console
```

Expected:

```text
No uncaught runtime errors.
```

Check mobile-width usability:

```bash
playwright-cli resize 390 844
playwright-cli goto http://127.0.0.1:8080/nodes
playwright-cli snapshot
```

Expected:

- Main status and list content remain reachable.

Close the browser:

```bash
playwright-cli close
```

- [ ] **Step 7: Backend fallback smoke test**

Run:

```text
curl -i http://127.0.0.1:8080/api/not-a-route
```

Expected:

```text
HTTP/1.1 404 Not Found
...
{"error":"not found","ok":false}
```

Stop the server after verification.

- [ ] **Step 8: Cleanup local smoke data**

Run:

```bash
rm -rf data-dev
```

Expected: local smoke test data removed.

- [ ] **Step 9: Final commit**

Run:

```bash
git status --short
git add web internal/master/api Makefile build/Dockerfile .gitignore
git commit -m "feat: add vaultfleet frontend mvp"
```

Expected: commit succeeds if there are staged implementation changes.

---

## 自检清单

- [ ] 技术栈仍是 React 19 + Vite + TypeScript + React Router + TanStack Query + Tailwind CSS + shadcn/ui + lucide-react。
- [ ] 没有 logout UI。
- [ ] 没有 storage test connection UI。
- [ ] Notifications 只暴露 `backup_failed` 和 `agent_offline`。
- [ ] Storage 支持 template mode 和 key-value advanced mode。
- [ ] Dashboard 24h 统计由 `GET /api/tasks?limit=200` 本地计算。
- [ ] API client 对 HTTP 非 2xx、`{ ok:false,error }`、直接 `{ error }` 都抛出错误。
- [ ] Go `/api/*` 和 `/ws/*` 不 fall through 到前端。
- [ ] `playwright-cli` browser smoke test 通过，且没有用它改写 Gemini 的视觉风格。
- [ ] `go test ./... -count=1` 通过。
- [ ] `cd web && npm test && npm run build` 通过。
