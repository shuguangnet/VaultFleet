# VaultFleet — 多客户端集中管理备份系统设计文档

> 日期：2026-05-18
> 状态：待用户审核

---

## 1. 项目概述

VaultFleet 是一个多 VPS 集中管理备份系统，采用 Master + Agent 架构。管理员在 Master Web UI 统一管理所有 VPS 的备份策略，Agent 安装在每台 VPS 上自主执行备份任务。备份引擎使用 restic，存储适配使用 rclone。

### 1.1 核心目标

- 管理员在 Web UI 集中管理所有 VPS 的备份策略，不需要登录任何 VPS
- Agent 一行命令安装并自动注册，零配置
- 备份数据由 Agent 直接写入云存储，不经过 Master
- 支持多种存储后端（S3/R2/MinIO/WebDAV/SFTP/网盘等），通过 rclone 适配
- 端到端加密（restic 内置），每台 VPS 独立仓库和独立密码

### 1.2 设计参考

- **Komari Monitor**（https://github.com/komari-monitor/komari）：Agent 注册模型、WebSocket 通信、任务下发、数据导出/恢复
- **backup-tool**（https://github.com/llleixx/backup-tool）：restic + rclone + systemd 的单机备份脚本，VaultFleet 的前身经验

### 1.3 技术栈

- 语言：Go（Master + Agent 共用）
- 数据库：SQLite（WAL 模式）
- 前端：Vue 3，构建后通过 Go embed.FS 内嵌到 Master 二进制
- 备份引擎：restic
- 存储适配：rclone
- 通信：WebSocket（gorilla/websocket）
- Web 框架：Gin
- ORM：GORM

---

## 2. 系统架构

### 2.1 角色定义

| 角色 | 职责 | 部署形态 |
|------|------|----------|
| **Master（主控）** | Web UI、策略管理、Agent 管理、任务状态、快照索引、通知 | Docker 容器（单实例） |
| **Agent（子节点）** | 注册、心跳、目录上报、执行 restic 备份/恢复、上报结果 | 单二进制，systemd 服务 |
| **Storage（存储端）** | 存放 restic 仓库数据 | S3/R2/MinIO/网盘等 |

### 2.2 数据流

```
┌─────────────────────────────────────────────────────┐
│                    Master (主控)                      │
│  ┌──────┐  ┌──────────┐  ┌────────┐  ┌───────────┐ │
│  │Web UI│  │策略引擎   │  │Agent管理│  │任务/快照DB│ │
│  └──┬───┘  └────┬─────┘  └───┬────┘  └─────┬─────┘ │
│     └───────────┴────────────┴──────────────┘       │
│                        │ WebSocket                   │
└────────────────────────┼────────────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         ▼               ▼               ▼
   ┌──────────┐    ┌──────────┐    ┌──────────┐
   │ Agent A  │    │ Agent B  │    │ Agent C  │
   │ (VPS-1)  │    │ (VPS-2)  │    │ (VPS-3)  │
   └────┬─────┘    └────┬─────┘    └────┬─────┘
        │               │               │
        ▼               ▼               ▼
   ┌─────────────────────────────────────────┐
   │         Storage (S3/R2/网盘)             │
   │  /backups/agent-a/   (独立 restic 仓库)  │
   │  /backups/agent-b/                       │
   │  /backups/agent-c/                       │
   └─────────────────────────────────────────┘
```

### 2.3 关键原则

- Master 和 Agent 之间只走 WebSocket（控制面），不传备份数据
- 备份数据由 Agent 直接通过 rclone 写入 Storage
- 每台 VPS 一个独立的 restic 仓库路径和独立密码
- Master 保存所有策略的权威副本，Agent 保存运行副本
- Agent 不开放任何入站端口，所有连接由 Agent 主动发起

---

## 3. Agent 注册与身份管理

### 3.1 注册流程

```
1. 管理员在 Web UI 创建一台 VPS，填写备注名（如 "Tokyo-1"）
2. Master 生成一次性 enrollment token（如 ek_a3f8b2...）
3. Web UI 展示安装命令：
   curl -fsSL https://master.example.com/install.sh | bash -s -- \
     --server https://master.example.com \
     --token ek_a3f8b2...

4. VPS 上执行安装命令：
   - 下载 agent 二进制（自动检测架构 amd64/arm64）
   - 下载 restic 二进制
   - 下载 rclone 二进制
   - agent 使用 enrollment token 向 Master 发起注册请求

5. Master 验证 token 有效且未使用过：
   - 将 enrollment token 标记为已使用
   - 为该 Agent 签发长期 agent_token（如 ak_7d9e1c...）
   - 绑定 Agent ID 和 VPS 记录

6. Agent 收到 agent_token，保存到本地配置文件
7. Agent 使用 agent_token 建立 WebSocket 连接
```

### 3.2 与 Komari 的区别

Komari 使用单一 token（既用于注册也用于长期认证）。VaultFleet 采用两段式设计（enrollment token 一次性 + agent_token 长期），因为备份系统涉及存储凭据和加密密码，安全要求更高。一次性 enrollment token 防止安装命令被截获后重放。

### 3.3 身份凭证

| 凭证 | 生命周期 | 存储位置 | 用途 |
|------|---------|---------|------|
| enrollment token | 一次性，注册后失效 | Master DB | 安装命令中使用 |
| agent_token | 长期，可由管理员吊销/轮换 | Agent 本地 + Master DB | WebSocket 认证 |

### 3.4 Agent 本地配置

路径：`/etc/vaultfleet/agent.yaml`

```yaml
server: https://master.example.com
agent_id: "uuid-xxx"
agent_token: "ak_7d9e1c..."
```

### 3.5 重装/崩溃恢复

- 同一台 VPS 再次执行相同安装命令 → enrollment token 已失效 → 安装失败
- 管理员在 Web UI 点击"重新生成安装命令" → 生成新的 enrollment token
- Agent 重新注册后，Master 识别为同一台 VPS（通过 Agent ID 绑定），自动下发之前保存的策略
- Agent 拿到策略后恢复定时任务，无需重新配置

### 3.6 吊销

管理员在 Web UI 删除一台 VPS → Master 标记 agent_token 失效 → Agent 下次心跳时连接被拒 → Agent 停止工作

---

## 4. WebSocket 通信协议

### 4.1 连接建立

```
Agent → Master: GET /ws/agent?token=ak_7d9e1c...
Master 验证 token → 升级为 WebSocket → 标记 Agent 在线
```

### 4.2 消息格式

所有消息均为 JSON：

```json
{
  "type": "消息类型",
  "id": "msg-uuid（用于请求-响应配对）",
  "payload": { ... }
}
```

### 4.3 消息类型

| 方向 | type | 说明 |
|------|------|------|
| Agent → Master | `heartbeat` | 定时心跳，携带 Agent 基础状态（CPU/内存/磁盘/restic 版本/rclone 版本） |
| Master → Agent | `dir_browse_req` | 请求浏览指定路径下的目录结构 |
| Agent → Master | `dir_browse_resp` | 返回目录树（路径、大小、类型），限制深度，排除系统目录 |
| Master → Agent | `policy_push` | 下发完整备份策略 |
| Agent → Master | `policy_ack` | 确认策略已接收并保存到本地 |
| Master → Agent | `backup_now` | 立即执行一次备份（不影响定时调度） |
| Agent → Master | `task_result` | 备份/恢复/prune 任务执行结果 |
| Master → Agent | `restore_req` | 下发恢复任务（快照 ID、目标路径） |
| Agent → Master | `restore_progress` | 恢复进度上报 |
| Master → Agent | `snapshot_list_req` | 请求 Agent 执行 restic snapshots 并返回 |
| Agent → Master | `snapshot_list_resp` | 返回快照列表 JSON |

### 4.4 心跳与离线检测

- Agent 每 30 秒发送 heartbeat
- Master 60 秒未收到心跳 → 标记 Agent 离线，触发通知
- Agent 检测 WebSocket 断开后自动重连，指数退避（1s → 2s → 4s → ... 最大 5min）

与 Komari 的区别：Komari 用数据上报本身当心跳（监控场景天然高频上报）。VaultFleet 备份场景两次备份之间可能间隔数小时，必须有独立心跳。

### 4.5 离线行为

- Agent 本地有策略副本和定时器，Master 离线不影响备份执行
- 离线期间的任务结果缓存在本地（`/etc/vaultfleet/pending_results.json`），重连后批量上报
- Master 重启后，所有在线 Agent 自动重连

---

## 5. 备份策略与执行

### 5.1 策略数据结构

Master 下发给 Agent 的完整策略：

```json
{
  "agent_id": "uuid-xxx",
  "storage": {
    "rclone_type": "s3",
    "rclone_config": {
      "provider": "Cloudflare",
      "access_key_id": "...",
      "secret_access_key": "...",
      "endpoint": "https://xxx.r2.cloudflarestorage.com",
      "bucket": "backups"
    },
    "repo_path": "vaultfleet/agent-uuid-xxx"
  },
  "restic_password": "per-agent-unique-password",
  "backup_dirs": ["/etc", "/home", "/opt/myapp/data"],
  "exclude_patterns": ["*.log", "*.tmp", "node_modules", ".cache"],
  "schedule": "0 3 * * *",
  "retention": {
    "keep_last": 3,
    "keep_daily": 7,
    "keep_weekly": 4,
    "keep_monthly": 6
  }
}
```

### 5.2 Agent 执行流程

```
1. Agent 收到 policy_push → 保存到 /etc/vaultfleet/policy.json
2. Agent 用 rclone_config 生成 /etc/vaultfleet/rclone.conf（权限 600）
3. Agent 将 restic_password 写入 /etc/vaultfleet/.restic-password（权限 600）
4. Agent 检查 restic 仓库是否已初始化：
   - 未初始化 → RESTIC_PASSWORD_FILE=... restic init -r rclone:vaultfleet:repo_path
   - 已初始化 → 跳过
5. Agent 根据 schedule 设置本地定时器（内置 cron 调度器，不依赖系统 crontab）
6. 定时触发或收到 backup_now 时执行：
   a. restic backup -r rclone:vaultfleet:repo_path /etc /home /opt/myapp/data \
        --exclude="*.log" --exclude="*.tmp" ...
   b. restic forget -r rclone:vaultfleet:repo_path \
        --keep-last 3 --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune
   c. restic snapshots -r rclone:vaultfleet:repo_path --json
7. 上报 task_result 给 Master：
   - 状态（success / failed）
   - 耗时
   - 新快照 ID
   - 仓库总大小
   - 快照列表摘要
   - 失败时附带错误日志（截取最后 200 行）
```

### 5.3 rclone 配置生成

Agent 不让用户手写 rclone.conf，而是从 Master 下发的结构化配置自动生成。restic 通过 `rclone:remote:path` 方式使用，环境变量指定 `RCLONE_CONFIG=/etc/vaultfleet/rclone.conf`。

### 5.4 策略变更

管理员在 Web UI 修改策略 → 策略更新触发内部事件（借鉴 Komari 的配置变更订阅机制）→ WebSocket 模块收到事件后推送新策略给对应 Agent → Agent 覆盖本地配置 → 重置定时器。

如果 Agent 离线，Master 标记 `synced = false`，Agent 重连后自动拉取最新策略。

### 5.5 与 Komari 的区别

Komari 的任务是"主控推送一次性 shell 命令"。VaultFleet 是"主控下发策略，Agent 自主定时执行"，同时支持"立即备份"（`backup_now`）的一次性触发。

---

## 6. 目录浏览

### 6.1 交互流程

```
管理员在 Web UI 点击某台 VPS 的"浏览文件"
  → Master 通过 WebSocket 发送 dir_browse_req { path: "/", depth: 2 }
  → Agent 扫描目录，返回 dir_browse_resp：
    [
      { "path": "/etc", "type": "dir", "size": 12582912 },
      { "path": "/home", "type": "dir", "size": 524288000 },
      { "path": "/opt", "type": "dir", "size": 1073741824 },
      ...
    ]
  → Web UI 展示树形结构
  → 管理员可点击展开子目录（触发新的 dir_browse_req { path: "/home", depth: 2 }）
  → 管理员勾选要备份的目录 → 保存到策略
```

### 6.2 安全约束

- 默认排除：`/proc`、`/sys`、`/dev`、`/run`、`/tmp`、`/snap`
- 单次扫描深度上限 3 层
- 不跟随符号链接
- 超时 10 秒，超时返回已扫描的部分结果

---

## 7. 恢复

### 7.1 恢复流程

```
1. 管理员在 Web UI 选择某台 VPS → 查看快照列表（从 Master DB 读取）
2. 选择一个快照 → 可选浏览快照内容（Agent 执行 restic ls --json）
3. 指定恢复目标路径（默认原路径，或自定义如 /restore/20260518）
4. Master 下发 restore_req { snapshot_id: "abc123", target: "/restore/20260518" }
5. Agent 执行 restic restore abc123 --target /restore/20260518
6. Agent 周期性上报 restore_progress（已恢复文件数、大小）
7. 完成后上报 task_result
```

### 7.2 Agent 崩溃后的恢复

- 管理员重新生成安装命令 → Agent 重新注册
- Master 下发之前保存的策略（包含存储配置和仓库密码）
- Agent 拿到策略后可以通过 `restic snapshots` 读取存储端已有的快照
- 管理员选择快照发起恢复

---

## 8. 通知

### 8.1 MVP 支持的渠道

| 渠道 | 配置项 | 触发时机 |
|------|--------|---------|
| Telegram Bot | bot_token + chat_id | 备份失败、Agent 离线超过 N 分钟 |
| Webhook | URL | 所有事件（success/failed/offline），JSON POST |

### 8.2 实现方式

通知在 Master 侧触发，不需要 Agent 参与。Master 收到 `task_result` 或检测到 Agent 离线时，根据通知配置发送。

### 8.3 消息示例

```
❌ 备份失败
节点: Tokyo-1
时间: 2026-05-18 03:00:15 UTC
错误: restic exit code 1 - repository locked

⚠️ 节点离线
节点: Tokyo-1
离线时间: 2026-05-18 03:05:00 UTC
已离线: 5 分钟
```

---

## 9. Master 数据模型

### 9.1 数据库

SQLite，WAL 模式。所有持久化数据存放在 `/data/` 目录下。

### 9.2 核心表

**users**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| username | TEXT UNIQUE | 管理员用户名 |
| password_hash | TEXT | bcrypt 哈希 |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |

**agents**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | Agent 唯一标识 |
| name | TEXT | 备注名，如 "Tokyo-1" |
| enroll_token | TEXT | 一次性注册 token |
| agent_token | TEXT | 长期认证 token |
| status | TEXT | online / offline |
| last_seen_at | TIMESTAMP | 最后心跳时间 |
| system_info | JSON | Agent 上报的 OS/CPU/内存/磁盘等 |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |

**storage_configs**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| name | TEXT | 如 "Cloudflare R2 主存储" |
| rclone_type | TEXT | s3 / webdav / sftp / ... |
| rclone_config | JSON | 结构化 rclone 配置参数（加密存储） |
| created_at | TIMESTAMP | |

**backup_policies**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| agent_id | UUID FK → agents | |
| storage_id | UUID FK → storage_configs | |
| repo_path | TEXT | 仓库在存储中的子路径 |
| restic_password | TEXT | 该 Agent 的仓库密码（加密存储） |
| backup_dirs | JSON | ["/etc", "/home", ...] |
| exclude_patterns | JSON | ["*.log", ...] |
| schedule | TEXT | cron 表达式 |
| retention | JSON | { keep_last, keep_daily, keep_weekly, keep_monthly } |
| synced | BOOLEAN | Agent 是否已确认收到最新策略 |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |

**task_history**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| agent_id | UUID FK → agents | |
| type | TEXT | backup / restore / prune |
| status | TEXT | success / failed / running |
| snapshot_id | TEXT | restic 快照 ID |
| started_at | TIMESTAMP | |
| finished_at | TIMESTAMP | |
| duration_ms | INTEGER | |
| repo_size | BIGINT | 仓库总大小 bytes |
| error_log | TEXT | 失败时的错误日志 |
| created_at | TIMESTAMP | |

**snapshots**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| agent_id | UUID FK → agents | |
| snapshot_id | TEXT | restic 快照 ID |
| timestamp | TIMESTAMP | 快照时间 |
| paths | JSON | 备份的目录列表 |
| size | BIGINT | 快照大小 |
| created_at | TIMESTAMP | |

**notification_configs**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| type | TEXT | telegram / webhook |
| config | JSON | { bot_token, chat_id } 或 { url } |
| events | JSON | ["backup_failed", "agent_offline"] |
| created_at | TIMESTAMP | |

---

## 10. Web UI

### 10.1 页面

| 页面 | 功能 |
|------|------|
| 初始化向导 | 首次启动时设置管理员用户名和密码 |
| 仪表盘 | 总览：Agent 在线/离线数、最近备份状态、存储用量 |
| 节点管理 | Agent 列表、在线状态、添加/删除 VPS、生成安装命令 |
| 节点详情 | 策略配置、目录浏览、快照列表、任务历史、立即备份、恢复 |
| 存储管理 | 添加/编辑/删除存储配置（rclone 参数） |
| 通知设置 | Telegram / Webhook 配置 |
| 系统设置 | 管理员密码修改、Master 数据导出 |

### 10.2 认证

- 用户名 + 密码登录，bcrypt 哈希存储
- Session cookie（HttpOnly, Secure）
- 首次启动进入初始化向导，不使用环境变量设置密码

### 10.3 前端技术

Vue 3 构建后通过 Go `embed.FS` 内嵌到 Master 二进制，不需要单独部署前端服务。中文界面。

---

## 11. 安全模型

### 11.1 传输安全

| 环节 | 机制 |
|------|------|
| Web UI | 生产环境建议反代 + TLS |
| Agent ↔ Master | agent_token 认证 WebSocket，生产环境要求 wss:// |

### 11.2 敏感数据存储

- Master DB 中的 restic 密码、rclone 凭据使用 AES-256-GCM 加密
- 加密主密钥存储在 `/data/master.key`（首次启动自动生成）
- Agent 本地 `/etc/vaultfleet/` 目录权限 700，配置文件权限 600，仅 root 可读

### 11.3 安全边界

- Master 不持有任何 VPS 的 SSH 密钥
- Master 不直接访问存储端，不读写备份数据
- Agent 不开放任何入站端口
- 每台 VPS 独立仓库、独立密码，单台泄露不影响其他

---

## 12. 部署

### 12.1 Master 部署

```yaml
# docker-compose.yml
services:
  vaultfleet:
    image: vaultfleet/master:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```

Master 数据目录：

```
/data/
├── vaultfleet.db          -- SQLite 数据库
├── master.key             -- 加密主密钥（首次启动自动生成）
├── backup.zip             -- 用户放入触发恢复（临时，恢复后自动删除）
└── rollback/
    └── 20260518-030000.zip -- 恢复前的自动回滚备份
```

Master 通过启动参数 `--data-dir /data` 指定根目录（Docker 镜像默认 `/data`），所有文件路径从此派生。

### 12.2 Agent 安装

```bash
curl -fsSL https://master.example.com/install.sh | bash -s -- \
  --server https://master.example.com \
  --token ek_a3f8b2...
```

安装脚本执行：
1. 检测架构（amd64/arm64）
2. 下载 agent 二进制到 `/usr/local/bin/vaultfleet-agent`
3. 下载 restic 二进制到 `/usr/local/bin/restic`
4. 下载 rclone 二进制到 `/usr/local/bin/rclone`
5. 创建 `/etc/vaultfleet/` 配置目录（权限 700）
6. Agent 使用 enrollment token 向 Master 注册
7. 创建 systemd service 并启动

### 12.3 Master 导出/恢复

- **导出**：Web UI 点击"导出备份" → 整个 `/data/` 目录打包为 `backup.zip` 下载
- **恢复**：将 `backup.zip` 放入挂载的 `./data/` 目录 → Master 启动时自动检测 → 回滚当前数据到 `rollback/` → 解压覆盖 → 正常启动

---

## 13. 代码结构

```
VaultFleet/
├── cmd/
│   ├── master/              -- Master 入口 main.go
│   └── agent/               -- Agent 入口 main.go
├── internal/
│   ├── master/
│   │   ├── api/             -- HTTP + WebSocket handlers
│   │   ├── db/              -- SQLite 数据层 (GORM)
│   │   ├── ws/              -- WebSocket 连接管理
│   │   ├── notify/          -- 通知（Telegram/Webhook）
│   │   ├── events/          -- 内部事件总线（策略变更触发推送）
│   │   └── backup/          -- 导出/恢复逻辑
│   └── agent/
│       ├── connect/         -- WebSocket 客户端 + 重连
│       ├── executor/        -- restic/rclone 命令执行
│       ├── filebrowse/      -- 目录扫描
│       ├── policy/          -- 本地策略存储
│       └── scheduler/       -- 本地 cron 调度器
├── pkg/
│   └── protocol/            -- 共享消息类型定义（Master/Agent 共用）
├── web/                     -- Vue 3 前端源码
├── build/
│   ├── Dockerfile           -- Master Docker 构建
│   └── install.sh           -- Agent 安装脚本
├── go.mod
├── go.sum
└── README.md
```

---

## 14. MVP 范围

### 14.1 第一版做的

- Linux Agent（amd64/arm64）
- rclone 所有支持的存储后端
- 文件/目录备份
- cron 表达式调度 + 立即备份
- 单管理员
- Telegram + Webhook 通知
- SQLite
- 单 Master 实例
- 中文 Web UI
- 指定快照整体恢复
- Master 配置导出到 zip

### 14.2 第一版不做的（后续版本）

- Windows / macOS Agent
- 数据库 dump（MySQL/PostgreSQL 等）
- 事件触发备份（文件变更等）
- 多用户 / RBAC 权限
- 邮件 / 企业微信 / Bark 等通知渠道
- PostgreSQL / MySQL 作为 Master 数据库
- 高可用 / 集群
- 多语言 i18n
- 单文件级浏览恢复
- Master 自动备份到 S3
