## Context

当前快照元数据可以包含多个 `DockerResolvedSource`，Agent 的 `DockerRestoreRequest.Sources` 和底层 `docker.Restore` 也已经接受多个 source，但 Master API 与 Web 恢复向导只暴露一个 `docker_source_id`。此外，当前执行器在第一个 source 失败时立即返回，任务结果只有整体成功或失败，无法表达哪些容器已恢复、哪些失败，也不能安全地只重试失败项。

批量恢复跨越 Web 选择状态、Master 元数据解析、Agent 文件恢复、Docker 重建、实时进度和任务持久化。恢复属于高风险操作，必须保持目标节点、快照和选择范围明确，并继续经过恢复预检。

## Goals / Non-Goals

**Goals:**

- 一次恢复同一快照中的多个 Docker 容器或 Compose 服务。
- 对整个选择集进行一次预检，并能定位到具体 source。
- 先恢复所有选中 source 的数据路径，再串行重建容器，避免并发写入共享路径或 Docker 资源。
- 在单项失败后继续执行剩余项，持久化逐项状态并支持只重试失败项。
- 保持旧单容器 API、旧任务记录和旧 Agent 的兼容行为。
- 让任务进度、日志和最终结果使用同一个 message ID。

**Non-Goals:**

- 不并行恢复多个容器。
- 不自动推断任意容器间的启动依赖顺序。
- 不备份或恢复镜像层、外部 Secret、外部网络和注册表凭据。
- 不跨多个快照或多个目标 Agent 组成一个批量恢复任务。
- 不承诺应用级一致性或恢复后的业务健康检查。

## Decisions

### 1. 扩展选择字段并保持单项兼容

Master/Web 的恢复请求新增 `docker_source_ids: string[]`。如果数组为空则读取旧字段 `docker_source_id` 并转换为单元素选择；如果两者同时存在，以数组为准。Master 去重后按照快照 Docker 元数据中的原始顺序生成 `DockerRestoreRequest.Sources`，拒绝空选择、未知 ID、重复解析结果和超过 50 项的请求。

备选方案是在前端循环调用现有单容器端点。该方案会产生多个命令与 message ID，无法统一预检、取消、展示总体进度或处理共享路径，因此不采用。

### 2. 复用现有多 source 协议并新增能力声明

`DockerRestoreRequest.Sources` 已能表达多个恢复项，不再增加平行的批量请求结构。新增 `docker_multi_container_restore` Agent capability；Master 只有在目标 Agent 声明该能力时才发送多 source 请求。单 source 请求继续兼容 `docker_container_restore` 能力。

备选方案是无条件发送多个 source。旧 Agent 虽可能反序列化成功，但缺少逐项结果和继续执行语义，行为不可验证。

### 3. 数据恢复一次执行，容器重建串行执行

Agent 对所有 source 的 `ResolvedPaths` 做规范化与去重，并通过现有 restore runner 一次恢复所需文件。数据恢复成功后，按照请求顺序逐项重建 Docker source。这样可避免同一 Compose 文件、卷目录或 bind mount 被重复从远端复制。

容器重建保持串行，因为 Docker 名称、网络、Compose 项目和共享卷可能互相影响。取消信号在数据恢复期间传递给 runner，并在每个 source 开始前检查。

### 4. 单项失败继续执行并汇总结果

Docker 执行器返回批量结果，而不是在首个错误处退出。每个 item 至少包含稳定 source ID、显示名称、状态、开始/结束时间、错误摘要和是否可重试。任务整体状态规则为：全部成功为 `success`，成功与失败并存为 `partial_success`，全部失败为 `failed`，用户取消为 `canceled`。

数据文件恢复失败属于批次级失败，所有尚未开始的 Docker item 标记为 `skipped`，因为没有可靠数据可用于重建。单个容器重建失败只影响该 item。

### 5. 进度和日志同时表达批次与当前项

恢复进度载荷增加 `items_total`、`items_completed`、`items_failed`、`current_source_id` 和 `current_source_name`。所有进度、日志和最终结果沿用恢复命令 message ID。Master 将 item 结果作为结构化 JSON 持久化到任务历史，旧记录没有 item 结果时仍按原有方式展示。

### 6. 预检结果按 source 归属并检测批次内冲突

预检继续返回稳定 code/severity/message/detail，并为 source 相关检查增加 `source_id` 和 `source_name`。Master/Agent 同时检查选择集内部的容器名称、Compose project/service、目标路径和端口冲突。公共检查（例如 Docker 不可用）不绑定 source；任一 error 阻止整个批量恢复。

### 7. 失败项重试创建新命令

任务详情中的“重试失败项”从已持久化结果中提取可重试 source ID，并使用原 source agent、目标 agent、快照和恢复模式创建新的恢复计划。重试必须重新预检，不复用旧预检结果，也不修改原任务历史。

### 8. 保留 Compose 项目环境上下文

Compose 容器元数据增加环境文件路径。Agent 优先读取 Docker Compose 的 project environment-file 标签，并同时探测项目工作目录下可读的 `.env`，将规范化、去重后的路径加入备份清单。只持久化路径，绝不解析、记录或输出环境文件内容。

恢复时显式传入 `--project-directory` 和每个 `--env-file`，避免 Agent 进程工作目录或 systemd 环境改变 Compose 变量解析。旧快照没有环境文件元数据时，恢复端仍探测工作目录 `.env`。若 Compose 配置包含 `${...}` 引用但没有可用环境文件，预检返回阻断错误；不允许 Compose 用空字符串完成替换。没有变量引用的旧 Compose 快照继续兼容原有恢复方式。

## Risks / Trade-offs

- [共享数据路径被多个 source 引用] -> Master 和 Agent 都对路径去重，数据只恢复一次，任务详情仍显示各 source 的路径关联。
- [部分成功使系统处于混合状态] -> UI 在确认前明确说明继续执行语义，最终状态使用 `partial_success` 并列出每项结果。
- [恢复顺序影响依赖服务] -> 默认使用快照元数据顺序，并允许 UI 调整已选项顺序；本变更不自动推断依赖。
- [旧 Agent 无法提供逐项结果] -> 多选 UI 由 capability 控制；旧 Agent 仅保留单选恢复。
- [结果 JSON 增大任务表] -> 限制单批最多 50 项，日志仍使用现有日志存储，不嵌入 item 结果。
- [重试可能覆盖已存在容器] -> 每次重试重新预检并显示冲突，继续沿用现有恢复确认与替换规则。
- [环境文件包含敏感信息] -> 元数据和日志只保存文件路径，环境文件内容仅作为备份数据传输，不进入结构化结果。
- [旧快照缺少环境文件] -> 恢复端探测 working directory 下的 `.env`；仍缺失且配置引用变量时由预检阻断并提示重新备份或补齐文件。

## Migration Plan

1. 先发布兼容读取旧数据的 Master 数据模型和 API。
2. 发布带新 capability、批量结果和进度载荷的 Agent。
3. Master 识别 capability 后再向目标 Agent 下发多 source 请求。
4. 最后启用 Web 多选、逐项结果和失败项重试入口。
5. 现有单容器恢复、旧任务历史和旧 API 调用无需迁移。

回滚时隐藏 Web 多选并停止发送 `docker_source_ids`；新 Agent 仍能处理单 source 请求，新增任务结果字段由旧 Master 忽略。

## Open Questions

- 首版是否需要允许用户拖拽调整恢复顺序，还是固定使用快照元数据顺序即可？
- Compose 同一项目的多个服务是否应聚合为一次 `docker compose up`，还是保留逐服务结果粒度？
- `partial_success` 是否需要触发独立通知事件，还是沿用失败通知并在正文中说明部分成功？
