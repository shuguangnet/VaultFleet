# 备份产物命名策略

VaultFleet 支持为备份策略设置业务/站点名称，并为压缩包备份配置远端目录模板和文件名模板。这样在对象存储、WebDAV 或本地目录中可以直接识别备份内容。

## 默认行为

旧策略没有配置命名字段时，保持兼容：

```text
artifacts/backup-{{datetime}}.{{ext}}
```

新建压缩包策略建议使用可读模板：

```text
archives/{{agent_name}}/{{context_name}}/{{date}}/{{context_name}}_{{agent_name}}_{{datetime}}.{{ext}}
```

快照模式不会改变 restic 仓库布局，但会把业务/站点名称写入任务历史和 `VAULTFLEET-MANIFEST.json`。

## 支持变量

| 变量 | 含义 |
| --- | --- |
| `{{date}}` | 日期，格式 `YYYY-MM-DD` |
| `{{time}}` | 时间，格式 `HHmmss` |
| `{{datetime}}` | 日期时间，格式 `YYYYMMDD-HHmmss` |
| `{{agent_id}}` | 节点 ID |
| `{{agent_name}}` | 节点名称 |
| `{{policy_id}}` | 策略 ID |
| `{{policy_name}}` | 策略名称，当前等同策略 ID |
| `{{context_name}}` | 业务/站点名称 |
| `{{site_name}}` | `context_name` 的别名 |
| `{{source_type}}` | `path`、`docker`、`database` 或 `mixed` |
| `{{container_name}}` | Docker 容器名称 |
| `{{compose_project}}` | Docker Compose 项目 |
| `{{compose_service}}` | Docker Compose 服务 |
| `{{database_engine}}` | 数据库类型 |
| `{{database_name}}` | 数据库名或 `all-databases` |
| `{{format}}` | 压缩格式 |
| `{{ext}}` | 文件扩展名 |

未知变量会被拒绝。变量值中的空格、斜杠和不适合作为对象 key 的字符会被替换为 `_`。

## 示例

网站目录：

```text
远端目录: websites/{{site_name}}/{{date}}
文件名: {{site_name}}_{{agent_name}}_{{datetime}}.{{ext}}
```

Docker Compose 应用：

```text
远端目录: docker/{{compose_project}}/{{date}}
文件名: {{compose_project}}_{{compose_service}}_{{datetime}}.{{ext}}
```

数据库：

```text
远端目录: databases/{{database_engine}}/{{database_name}}/{{date}}
文件名: {{database_name}}_{{datetime}}.{{ext}}
```

## 冲突提示

文件名模板建议包含 `{{datetime}}`、`{{date}}` 或 `{{time}}`。如果没有时间变量，后续备份可能和旧文件重名，取决于远端存储后端的覆盖行为。

## 安全规则

VaultFleet 会拒绝：

- 绝对路径。
- `.` 或 `..` 路径段。
- 空文件名。
- 文件名中的 `/` 或 `\`。
- 控制字符。

这些规则用于避免对象 key 在不同存储后端中被解释成不安全路径。
