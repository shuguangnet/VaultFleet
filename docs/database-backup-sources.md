# 数据库备份源

VaultFleet 支持在备份策略中添加 PostgreSQL 和 MySQL 数据库备份源。数据库源会在备份任务开始时生成逻辑 dump 文件，然后把 dump 文件纳入普通 restic 快照或压缩包归档。

## 支持范围

- PostgreSQL：`pg_dump` 单库；全库模式会先用 `psql` 列库，再为每个数据库分别生成一个 `pg_dump` 文件。
- MySQL：`mysqldump` 单库；全库模式会先用 `mysql` 列库，再为每个数据库分别生成一个 `mysqldump` 文件。
- 执行位置：Agent 主机，或选定 Docker 容器内。
- 输出：`.sql` 或 `.sql.gz`。
- 密码：Master 端加密保存，API 响应和任务日志不返回明文。
- 配置体验：可以直接选择“全部数据库”，也可以用当前连接信息加载数据库列表后选择单个数据库。

当前版本不自动恢复数据库。恢复时先恢复 dump 文件，再按数据库自身工具执行导入，例如 `psql`、`mysql`。

## Agent 依赖

主机执行模式要求 Agent 主机已经安装对应客户端工具：

```bash
pg_dump --version
psql --version
mysqldump --version
mysql --version
```

Docker 执行模式要求目标容器内存在对应 dump 工具。大多数官方 PostgreSQL/MySQL 镜像已包含这些工具。

## PostgreSQL 示例

单库备份：

```text
类型: PostgreSQL
执行位置: Agent 主机
主机: 127.0.0.1
端口: 5432
用户: postgres
数据库: app
gzip: 开启
```

最小权限取决于数据对象。常见做法是创建只读备份用户，并授予需要导出的 schema/table 权限。全库备份通常需要更高权限。

## MySQL 示例

单库备份：

```text
类型: MySQL
执行位置: Docker 容器
容器: mysql
用户: root
数据库: app
gzip: 开启
```

建议按最小权限创建备份用户，至少需要读取目标库表结构和数据的权限。全库备份通常需要额外权限，并且会跳过 MySQL 的 `information_schema` 和 `performance_schema`。

## 与备份模式的关系

数据库源只负责生成 dump 文件：

- 快照仓库模式：dump 文件作为 restic 路径上传。
- 压缩包归档模式：dump 文件进入生成的 `.tar.gz` 或 `.zip`。
- 全库模式：每个数据库会生成一个独立 dump 文件；如果配置了输出文件名，它会作为前缀展开，例如 `full.sql` 生成 `full-app.sql`、`full-logs.sql`。

如果业务需要更强一致性，例如导出前暂停写入、刷新缓存或锁表，仍可以配合策略的 pre/post hook 使用。

## 安全注意事项

- 不要把数据库密码写进 pre/post hook。
- 任务日志会做脱敏，但仍建议避免让 dump 工具输出敏感连接串。
- Docker socket 权限较高，仅在可信 Agent 主机上启用 Docker 执行模式。
- 大数据库会先写入 Agent 本地 staging 目录，确保 Agent 磁盘空间足够。
