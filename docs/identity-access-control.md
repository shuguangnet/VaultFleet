# 多用户、RBAC 与 API Token

VaultFleet 支持多用户访问、内置角色权限和用于自动化的 API Token。Agent 注册 token 和 Agent 长期 token 与用户登录/API Token 是两套独立凭据，不能互相替代。

## 角色

| 角色 | 用途 | 权限范围 |
| --- | --- | --- |
| `admin` | 系统管理员 | 完整控制台权限，包括用户、API Token、系统导入导出和所有运维操作 |
| `operator` | 日常运维 | 查看运维数据，管理节点/存储/策略/通知，执行备份、验证、恢复、取消任务和诊断 |
| `viewer` | 只读审计 | 查看仪表盘、节点、存储摘要、策略、任务、快照、通知、系统状态和审计日志 |

`viewer` 不能执行新增、编辑、删除、备份、验证、恢复、取消、诊断、导入、导出或 Token 创建等操作。

## API Token

管理员可以在系统管理页创建 API Token。Token 创建后只显示一次，之后只能看到名称、角色、scope、状态和最近使用时间。

请求示例：

```bash
curl -H "Authorization: Bearer vf_xxx_xxx" https://MASTER_HOST/api/agents
```

API Token 同时受角色和 scope 限制。即使创建请求包含更高权限 scope，最终有效权限也不能超过 Token 指定角色。

建议：

- 为 CI、巡检脚本和外部自动化单独创建 Token。
- 为 Token 设置最小 scope。
- 定期吊销不再使用的 Token。
- 不要把 Token 写入公开日志、Issue 或截图。

## 审计日志

系统会记录登录、用户管理、Token 管理、存储/策略/通知变更、备份、验证、恢复、取消任务、诊断和系统导入导出等敏感操作。审计日志包含操作者、动作、目标、结果、IP、User-Agent 和时间，不保存请求体或密码、secret、token 等敏感值。

## 迁移

升级到支持 RBAC 的版本后，已有用户会自动迁移为启用状态的 `admin`。新部署的首个初始化用户也是 `admin`。

## 回滚

旧版本二进制会忽略新增的用户角色、API Token 和审计表。回滚后，多用户权限、API Token 和审计功能不可用；已有用户名和密码哈希仍保留在用户表中。
