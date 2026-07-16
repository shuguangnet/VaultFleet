## Why

Docker 快照可能同时包含数据库、应用、网关等多个相互依赖的容器，但当前恢复向导一次只能选择一个 Docker source。操作人员必须重复执行多次恢复，无法在执行前统一识别路径、容器名称、Compose 服务和网络冲突，也无法获得一次恢复任务的整体结果。

## What Changes

- Docker 恢复向导支持从同一快照中多选容器或 Compose 服务，并显示已选数量与恢复范围摘要。
- 单次预检覆盖全部已选容器，按容器归组返回冲突、路径可写性、Docker 可用性及共享资源风险。
- 单个恢复命令携带稳定排序的多个 Docker source，由 Agent 串行恢复并持续上报当前容器和总体进度。
- 批量恢复采用“继续执行并汇总”语义：一个容器失败不会阻止尚未执行的容器，最终任务区分全部成功、部分成功和全部失败。
- 任务详情持久化每个容器的状态、错误和可重试标识；操作人员可以仅重试失败项。
- 保持现有单容器恢复请求兼容，旧 Agent 或不支持批量恢复能力的目标节点继续使用单选模式。

## Capabilities

### New Capabilities

- `multi-container-restore`: 定义多容器选择、批量预检、串行恢复、逐项进度、部分失败和失败项重试的端到端行为。

### Modified Capabilities

- 无。

## Impact

- 协议：`pkg/protocol` 中的恢复请求、进度和任务结果载荷，以及 Agent capability 声明。
- Master：恢复请求解析与校验、Docker source 解析、预检聚合、命令和任务结果持久化。
- Agent：Docker 批量恢复执行器、逐项错误隔离、进度上报和取消处理。
- Web：快照浏览器的多选恢复向导、批量预检展示、确认摘要、任务详情和失败项重试。
- 测试：协议兼容、Master handler、Agent executor、任务持久化和 React 工作流测试。
