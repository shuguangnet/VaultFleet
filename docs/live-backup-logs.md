# 实时备份日志

VaultFleet 支持在任务历史中查看备份和备份验证的实时日志。实时日志用于排查正在执行或刚完成的任务，例如 restic 初始化、备份上传、保留策略清理、快照统计、Docker 来源解析、验证步骤和 pre/post hook 输出。

## 行为

- Agent 在备份相关任务执行期间发送 `task_log` 事件，日志按任务消息 ID 关联。
- Master 只在内存中保留近期日志，不写入 SQLite；Master 重启后日志缓冲会清空。
- 默认每个任务最多保留最近 2,000 行或 512 KiB，超过限制时丢弃最旧日志。
- 任务完成后，日志缓冲默认最多保留 24 小时，之后查询会返回过期或空状态。
- Web UI 通过轮询 `after=latest_sequence` 增量拉取日志，不使用浏览器 WebSocket/SSE。

## 脱敏边界

Agent 会在日志离开节点前执行脱敏，Master 在写入缓冲前再次执行防御性脱敏。当前规则会处理常见的 password、token、secret、access key、Authorization 头等键值格式。

实时日志仍可能包含业务程序主动输出的非结构化敏感数据。运维上应避免在备份 hook 中打印数据库内容、完整环境变量、应用配置或私有 URL。

## API

```text
GET /api/tasks/:id/logs?after=<seq>&limit=<n>
GET /api/commands/:id/logs?after=<seq>&limit=<n>
```

响应包含：

- `status`: `available`、`empty`、`missing_message_id`、`expired` 或 `unsupported_agent`
- `lines`: 按序列号排序的日志行
- `latest_sequence`: 当前缓冲中的最高序列号
- `truncated` / `dropped_lines`: 是否因缓冲限制丢弃过旧日志

查看实时日志需要 `read:operational` 权限。API Token 也必须包含该 scope。

## 排障建议

- 如果 UI 显示“节点版本暂不支持实时任务日志”，升级对应 Agent。
- 如果任务没有日志但仍在运行，先确认 Agent 已连接并且任务是新版本 Master 下发的备份或验证任务。
- 如果日志已过期，使用任务最终错误、Agent 日志采集或系统日志继续排查。
- 如果 hook 输出被截断或丢弃，减少 hook 输出量，或将详细诊断写到业务侧文件后按需单独收集。
