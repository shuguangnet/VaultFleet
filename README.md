# VaultFleet

<!-- markdownlint-disable MD013 -->

> 像探针一样部署 Agent，像备份平台一样统一编排多台 Linux 服务器的备份、快照和恢复。

**Language:** 中文 | [English](README.en.md)

VaultFleet 是一个面向多台 VPS / Linux 服务器的集中式备份管理系统。它采用 **Master + Agent** 架构：Master 提供 Web UI、API、策略管理、任务历史、快照和通知；Agent 安装在每台服务器上，主动连接 Master，接收备份策略，并使用 `restic` + `rclone` 将备份数据直接写入对象存储、WebDAV、SFTP、网盘或其他 rclone 后端。

备份数据不经过 Master。Master 负责控制面和元数据，Agent 负责本机执行和上传。

![仪表盘](docs/screenshots/dashboard.png)

## 特性

- **出站连接的 Agent**：节点不需要开放入站端口。
- **集中备份策略**：统一管理备份目录、排除规则、Cron 调度、保留策略、任务超时和存储配置。
- **Web 控制台**：提供仪表盘、节点、存储、策略、任务、快照、通知和系统管理页面。
- **一次性注册令牌**：安装时使用 `enrollment token`，注册后换发长期 `agent_token`。
- **restic 加密仓库**：每台 Agent 使用独立 restic 仓库密码；Master 侧敏感配置使用 `/data/master.key` 加密保存。
- **直接写入存储端**：Agent 通过 rclone 写入 S3 / R2 / MinIO、WebDAV、SFTP、本地路径或其他后端。
- **任务进度与取消**：运行中的备份会上报阶段、文件数、字节数和传输速度，长任务支持取消和策略级超时。
- **快照浏览与选择性恢复**：支持刷新快照、浏览快照内容、恢复整份快照或选择部分路径恢复。
- **诊断与通知**：支持 Telegram、Webhook、健康检查、系统诊断和 Agent 日志收集。
- **Agent 版本感知与自更新**：Master 可识别 Agent 版本，Agent 可通过 GitHub Release 下载新版二进制并重启。

## 环境要求

| 组件 | 要求 |
| --- | --- |
| Master | Docker / Docker Compose，或能构建 Go 二进制的 Linux 环境 |
| Agent | Linux `amd64` 或 `arm64`，安装脚本需要 root 权限 |
| Agent 服务管理 | systemd、OpenRC，或安装脚本的 `nohup` fallback |
| 源码开发 | Go 版本以 `go.mod` 为准；Web UI 使用 npm 脚本构建和测试 |
| 存储端 | 任意可由 rclone 访问的后端，例如 S3、R2、MinIO、WebDAV、SFTP、本地路径 |

## 快速开始

### 1. 启动 Master

使用 Docker Compose：

```bash
docker compose up -d
```

默认会拉取公开镜像：

```text
ghcr.io/momo-z/vaultfleet:latest
```

服务监听 `http://localhost:8080`，数据保存在当前目录的 `./data`：

```text
data/
├── vaultfleet.db
├── master.key
└── rollback/
```

也可以使用 Docker 直接启动：

```bash
docker run -d \
  --name vaultfleet \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --restart unless-stopped \
  ghcr.io/momo-z/vaultfleet:latest
```

首次访问 Web UI 时，需要初始化管理员账号。

> 生产环境建议使用固定版本 tag，并通过反向代理启用 HTTPS / WSS。`http://` 示例只适合本地或受信任内网测试。

### 2. 添加节点并安装 Agent

在 Web UI 的 **节点管理** 中创建节点后，Master 会生成一次性注册令牌和安装命令。Web UI 支持三种脚本来源：

- GitHub raw：直接从仓库下载安装脚本。
- GitHub + 代理：通过代理下载脚本和 Release 资产。
- Master 服务器：从当前 Master 的 `/install.sh` 下载脚本。

Master-hosted 示例：

```bash
curl -fsSL https://MASTER_HOST/install.sh | bash -s -- \
  --server https://MASTER_HOST \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx
```

GitHub 代理示例：

```bash
curl -fsSL https://MASTER_HOST/install.sh | bash -s -- \
  --server https://MASTER_HOST \
  --token ek_xxxxxxxxxxxxxxxxxxxxxxxx \
  --github-proxy https://gh-proxy.example.com
```

安装脚本会：

1. 检测 Linux 架构，目前支持 `amd64` 和 `arm64`。
2. 下载 `vaultfleet-agent` 到 `/usr/local/bin/`。
3. 安装或准备 `restic` 和 `rclone`。
4. 创建 `/etc/vaultfleet/` 配置目录。
5. 使用一次性 token 向 Master 注册。
6. 创建并启动 systemd / OpenRC 服务；没有受支持的 init system 时使用 `nohup` 启动。

`--agent-url` 是高级覆盖参数，用于指定完整的 Agent 二进制下载地址，主要用于测试未发布版本、内网镜像、自建 CDN 或临时下载源。

### 3. 卸载 Agent

```bash
curl -fsSL https://raw.githubusercontent.com/momo-z/VaultFleet/main/build/uninstall.sh | bash
```

该脚本会停止服务，并删除 `vaultfleet-agent`、`restic`、`rclone` 和 Agent 配置目录。

## 典型使用流程

1. 在 **存储配置** 中添加 S3 / R2 / MinIO、WebDAV、SFTP、本地路径或其他 rclone 后端，并执行连接测试。
2. 在 **节点管理** 中创建节点，复制安装命令到目标服务器执行，等待 Agent 注册上线。
3. 在 **备份策略** 中选择节点和存储，设置仓库子路径、备份目录、排除规则、Cron 调度、保留策略和任务超时。
4. 如使用 WebDAV、AList 代理或限流存储，在策略的 **高级传输参数** 中调整 rclone 并发、请求频率、重试和超时。
5. 在 **任务历史** 中查看手动备份、定时备份、恢复任务和运行中备份进度；必要时取消仍在运行的任务。
6. 在 **快照浏览** 中刷新快照、浏览快照内容，并选择整份快照或部分路径恢复到目标目录。
7. 需要迁移到新节点时，在新节点上创建使用相同存储和仓库子路径的策略，策略同步后即可看到原有快照。

## 架构

```text
┌──────────────────────────────────────────────┐
│                   Master                      │
│  Web UI / API / SQLite / Policy / Notify      │
└──────────────────────┬───────────────────────┘
                       │ WebSocket 控制面
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─────────┐    ┌─────────┐    ┌─────────┐
   │ Agent A │    │ Agent B │    │ Agent C │
   └────┬────┘    └────┬────┘    └────┬────┘
        │              │              │
        └──────────────┼──────────────┘
                       ▼
        S3 / R2 / MinIO / WebDAV / SFTP / rclone 后端
```

关键设计原则：

- Master 负责控制面，不接收、不中转备份数据。
- Agent 保存本地策略副本，Master 短暂离线不影响已下发的定时备份。
- 每台服务器拥有独立仓库路径和独立 restic 密码。
- Agent 只主动向外连接 Master 和存储端，不要求公网入站访问。

## 安全与信任边界

- 生产环境建议通过反向代理启用 HTTPS / WSS。
- Agent 注册使用一次性 `ek_` token，注册成功后保存长期 `agent_token`。
- Master 使用 bcrypt 保存管理员密码哈希。
- rclone 配置、restic 仓库密码和通知凭据在 Master 数据库中加密保存。
- `/data/master.key` 是 Master 加密主密钥，必须和数据库一起备份并妥善保护。
- Master 会保存并向 Agent 下发执行备份所需的 restic 仓库密码，因此 Master 主机、管理员账号、`vaultfleet.db` 和 `master.key` 都属于信任边界。
- Agent 本地配置默认位于 `/etc/vaultfleet/agent.yaml`，安装目录权限应限制为 root 可读写。

安全问题请按 [Security Policy](SECURITY.md) 处理，不要在公开 issue 中粘贴漏洞细节或敏感凭据。

## 数据导出与恢复

Web UI 的 **系统管理** 页面提供 Master 数据导出和导入。导出的 zip 包含 Master 配置、元数据、密钥和任务记录，不包含远端 restic 仓库中的实际备份数据。

导入要求：

- zip 大小不超过 100 MB。
- zip 内必须包含 `vaultfleet.db` 和 `master.key`。
- 导入确认后，Master 会将文件保存为 `/data/backup.zip` 并退出；容器或进程管理器重启 Master 后会自动恢复。
- 恢复前的数据会保存到 `/data/rollback/`。

## 运维常用命令

```bash
# 拉取最新 Master 镜像并重启
docker compose pull
docker compose up -d

# 查看 Master 日志
docker compose logs --tail=200 -f vaultfleet

# 重启或停止 Master
docker compose restart vaultfleet
docker compose down
```

Agent 常用运维：

```bash
# systemd
systemctl status vaultfleet-agent
journalctl -u vaultfleet-agent --since "24 hours ago" --no-pager
systemctl restart vaultfleet-agent

# OpenRC
rc-service vaultfleet-agent status
rc-service vaultfleet-agent restart

# 无 systemd / OpenRC 时的 fallback 日志
tail -n 300 /var/log/vaultfleet-agent.log
```

## 开发

```bash
# 运行 Go 测试
make test

# 构建二进制
make build-master
make build-agent
make build-all

# 构建 Master Docker 镜像
make docker-build
```

前端开发：

```bash
cd web
npm install
npm run build
npm run test
```

## 文档

- [English README](README.en.md)
- [通信协议](docs/protocol.md)
- [发布流程](docs/release.md)
- [问题反馈和日志收集指南](docs/support.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)

## 反馈问题

遇到 bug 或需要排障支持时，请先阅读 [问题反馈和日志收集指南](docs/support.md)。

- [选择 Issue 类型](https://github.com/momo-z/VaultFleet/issues/new/choose)
- [Bug report](https://github.com/momo-z/VaultFleet/issues/new?template=bug_report.yml)
- [Support request](https://github.com/momo-z/VaultFleet/issues/new?template=support_request.yml)

发布日志前请脱敏 token、密码、cookie、rclone 凭据、存储密钥、通知凭据和私有 endpoint。

## 许可证

VaultFleet 使用 [MIT License](LICENSE)。

## 参考

- [Komari Monitor](https://github.com/komari-monitor/komari)：Agent 注册、WebSocket 通信、任务下发等探针式部署体验。
- [Nezha](https://github.com/nezhahq/nezha)：Dashboard + Agent 的监控面板项目形态。
- [restic](https://restic.net/)：加密备份引擎。
- [rclone](https://rclone.org/)：多存储后端适配。

<!-- markdownlint-enable MD013 -->
