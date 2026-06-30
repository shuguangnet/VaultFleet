## Why

VaultFleet 目前适合直接备份主机目录，但缺少面向 Docker 工作负载的明确指导与一致性控制，用户需要自行推断哪些挂载路径、编排文件和导出步骤应纳入策略。随着容器化部署成为常见场景，需要把这类备份路径、限制和安全边界变成产品内可见能力，而不是仅靠文档外经验。

## What Changes

- 为备份策略增加 Docker 场景说明，明确推荐备份对象为容器挂载数据、`docker-compose.yml`、`.env` 及其他编排相关文件。
- 为备份策略增加可选备份钩子，允许在备份前后执行受控命令，用于数据库导出、短暂停容器或刷新应用一致性数据。
- 在策略校验、Agent 执行和任务结果中支持钩子执行结果的记录与失败处理。
- 在前端策略页面补充 Docker 工作负载备份的表单说明、风险提示和示例。
- 明确排除镜像层备份、`docker commit` / `docker save` 类工作流，以及自动重建容器或编排恢复。

## Capabilities

### New Capabilities
- `docker-workload-backup`: 定义 Docker 工作负载的备份范围、可选钩子、失败语义以及产品内提示要求。

### Modified Capabilities

None.

## Impact

- 后端：`internal/master/api`、`internal/master/db`、`internal/agent`、`pkg/protocol`
- 前端：`web/src/pages/policies`、相关类型与服务层
- 文档：`README.md`、`README.en.md`、必要时补充 `docs/`
- 运维与安全：新增受控命令执行面，需要明确权限、超时、日志脱敏和失败回滚边界
