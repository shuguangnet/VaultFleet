# VaultFleet 问题反馈流程设计

> 日期：2026-05-19
> 状态：已批准进入实施计划

## 目标

让用户可以通过 GitHub 清晰提交 VaultFleet 问题，同时不让 VaultFleet 接触用户的
GitHub 账号或 token。第一版只使用 GitHub 浏览器登录态、Issue 模板和支持文档；
不实现 GitHub OAuth、GitHub App、自动创建 issue 或诊断 zip 上传。

## 当前上下文

VaultFleet 现在已经有几类可用于排障的信息：

- Master 进程日志，通常来自 Docker、Docker Compose 或宿主进程管理方式。
- Agent 进程日志，通常来自 systemd、OpenRC，或 fallback 模式下的
  `/var/log/vaultfleet-agent.log`。
- Master 数据库中的任务历史，并通过 `GET /api/tasks` 暴露；失败的备份和恢复任务
  会带有 `error_log`。
- 现有 Agent API 和 UI 中的 Agent 状态、最后在线时间和系统信息。

缺口是：项目还没有公开、稳定、可复制的问题反馈路径，用户不知道应该提交什么、
怎么收集日志，以及哪些内容必须脱敏。

## 用户流程

1. 用户遇到 bug 或需要排障支持。
2. 用户从 README 的“反馈问题 / Report an issue”入口，或从支持文档进入 GitHub。
3. 用户在 GitHub issue chooser 中选择“Bug report”或“Support request”。
4. GitHub 使用用户浏览器里已有的登录态；VaultFleet 不读取、不保存 GitHub token。
5. 对应 Issue 表单要求用户填写环境信息、复现步骤、日志、任务 `error_log` 和脱敏确认。
6. 维护者收到结构化 issue 后，可以直接开始判断问题，而不是先反复追问部署方式和日志。

## 方案

使用 GitHub Issue Forms，而不是普通 Markdown issue 模板。

Issue Forms 可以提供必填字段、下拉选项、文本区域、默认提示和标签，同时仍然只是仓库
元数据，不需要任何运行时集成。这个需求的目标是让排障信息更结构化，不是做自定义自动化，
所以 Issue Forms 更合适。

新增文件：

- `.github/ISSUE_TEMPLATE/bug_report.yml`
- `.github/ISSUE_TEMPLATE/support_request.yml`
- `.github/ISSUE_TEMPLATE/config.yml`
- `docs/support.md`

更新文件：

- `README.md`

## Issue 模板

### Bug Report

Bug 表单使用完整中英双语文案，字段 label 和 description 都同时包含中文与英文。
它收集：

- 简短问题摘要。
- VaultFleet 版本、镜像 tag 或 commit。
- 部署方式：Docker Compose、Docker、源码运行或其他。
- Master 环境：系统、架构、是否使用反向代理；如果是 UI 问题，还包括浏览器。
- Agent 环境：系统、架构、init system、安装方式。
- 复现步骤。
- 预期行为。
- 实际行为。
- 相关 Master 日志。
- 相关 Agent 日志。
- 如果问题涉及备份、恢复、快照或策略同步，要求提供任务历史中的 `error_log`。
- 截图或其他补充信息。
- 确认已经脱敏。

表单应自动打上 `bug` 和 `needs-triage` 这类标签。

### Support Request

支持请求表单同样使用完整中英双语文案。它用于安装问题、配置问题、存储后端排障、
恢复操作疑问，以及其他不确定是否属于产品缺陷的使用问题。

它收集：

- 用户正在尝试完成什么。
- 当前部署状态。
- 存储后端类型，但不包含密钥。
- Master 和 Agent 环境摘要。
- 已尝试的命令或 UI 操作。
- 相关日志和任务 `error_log`。
- 同样的脱敏确认。

表单应自动打上 `support` 和 `needs-triage` 这类标签。

### Issue Chooser

Issue chooser 应该：

- 展示两个 Issue Forms。
- 禁用或不鼓励空白 issue。
- 链接到 `docs/support.md`，作为排障和日志收集指南。

## 支持文档

`docs/support.md` 应使用中英双语，但可以保持简洁。它需要包含：

- 什么时候提交 bug report，什么时候提交 support request。
- 提醒用户：GitHub 提交账号由浏览器里的 GitHub 登录态决定。
- 直接链接：
  - `https://github.com/momo-z/VaultFleet/issues/new/choose`
  - 直接打开 bug report 模板的 URL。
  - 直接打开 support request 模板的 URL。
- 日志收集命令：
  - `docker compose logs --tail=300 vaultfleet`
  - `docker logs --tail=300 vaultfleet`
  - `journalctl -u vaultfleet-agent --since "24 hours ago" --no-pager`
  - `rc-service vaultfleet-agent status`
  - `tail -n 300 /var/log/vaultfleet-agent.log`
- 如何提供任务历史：
  - 如果 Web UI 可用，优先复制任务历史中的失败记录。
  - 或者在已有认证会话中调用 `GET /api/tasks`，复制相关失败任务的 `error_log`。
- 脱敏规则：
  - 不要上传 `master.key`。
  - 不要上传完整 `vaultfleet.db`。
  - 不要粘贴完整 `/etc/vaultfleet/agent.yaml`。
  - 必须脱敏 enrollment token、agent token、cookie、restic password、rclone access key、
    secret key、WebDAV 凭据、SFTP 凭据，以及敏感的私有 endpoint。

## README 更新

在 README 的“开发状态”或“参考”附近增加一个简短的“反馈问题 / Report an issue”
章节，指向：

- `docs/support.md`
- `https://github.com/momo-z/VaultFleet/issues/new/choose`

README 不重复完整排障指南，避免后续两处文档漂移。

## 非目标

本设计明确不做：

- GitHub OAuth 或 GitHub App 授权。
- 从 VaultFleet 内部创建 GitHub issue。
- 上传诊断 zip 到 GitHub。
- 通过 Master-Agent 协议收集 Agent 日志。
- 新增 Web UI 诊断页面。
- 改变运行时日志行为。

这些能力以后可以根据用户量和支持成本再做；第一版先保持安全、简单、可维护。

## 错误处理

这个改动只涉及仓库元数据和文档，没有运行时错误处理。需要注意：

- Issue form YAML 如果写错，会影响 GitHub issue 模板渲染，需要通过 review 和 GitHub
  页面验证发现。
- README 尽量使用仓库相对链接；支持文档中需要跳转 GitHub 新建 issue 的地方使用规范 URL。
- 文档里的命令必须覆盖不同部署方式，因为 Master 可能运行在 Docker Compose、Docker 或
  自定义进程里，Agent 也可能运行在 systemd、OpenRC 或 fallback `nohup` 模式下。

## 测试与验证

验证范围：

- 检查两个 Issue Forms 的 YAML 语法。
- 确认 `.github/ISSUE_TEMPLATE/config.yml` 指向支持文档。
- 确认 README 链接正确。
- 确认日志命令符合当前 `docker-compose.yml` 和 `build/install.sh` 的部署行为。
- 确认模板不会要求用户粘贴已知敏感文件或凭据。

该改动不影响 Go 运行时代码，因此不需要新增或运行 Go 测试。
