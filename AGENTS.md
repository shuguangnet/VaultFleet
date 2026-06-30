# Repository Guidelines

## 项目结构与模块组织
`cmd/master` 和 `cmd/agent` 是两个 Go 程序入口。后端核心代码位于 `internal/agent` 与 `internal/master`，协议与通用工具放在 `pkg/`。集成测试位于 `test/`，构建脚本和安装脚本位于 `build/`，补充文档在 `docs/` 与 `openspec/`。前端集中在 `web/`：页面在 `web/src/pages`，通用组件在 `web/src/components`，接口调用在 `web/src/services`，测试初始化在 `web/src/test`。

## 构建、测试与开发命令
运行 `make test` 执行 Go 全量测试，默认带 `-race`。运行 `make build-master`、`make build-agent` 或 `make build-all` 生成二进制到 `bin/`。前端首次开发先执行 `cd web && npm install`，本地启动用 `npm run dev`，构建产物用 `npm run build`，前端测试用 `npm run test`。需要验证容器镜像时，使用 `make docker-build`，对应 `build/Dockerfile`。

## 代码风格与命名约定
Go 代码必须保持 `gofmt` 格式。命名遵循 Go 习惯：导出标识符使用 `CamelCase`，包内辅助函数使用 `camelCase`，包名保持简短明确。TypeScript/React 代码遵循现有风格：优先函数式组件，使用 `@/` 路径别名，文件名采用 kebab-case，例如 `tasks-page.tsx`、`status-badge.tsx`。仅在代码意图不够直接时添加简短注释。

## 测试指南
Go 测试文件与源码同目录，命名为 `*_test.go`；前端测试通常与源码相邻，命名为 `*.test.ts` 或 `*.test.tsx`。新增功能优先补充针对 handler、service、scheduler 等模块的单元测试；只有跨模块行为变更时才修改 `test/integration_test.go`。提交前至少运行与改动直接相关的最小测试集。

## 提交与 Pull Request 规范
Git 历史使用 Conventional Commits 风格，例如 `feat:`、`fix:`；提交标题保持简短、直接、使用祈使语气。PR 应聚焦单一问题；若涉及备份、恢复、存储、注册或升级流程，需要说明运维影响、迁移步骤和回滚方式。有用户可见变化时，同步更新 `README.md`、`README.en.md` 或 `docs/`。不要在提交、截图、日志或测试数据中包含真实凭据、令牌、数据库文件或私有地址。

## 安全与配置提示
该仓库涉及凭据、加密密钥、恢复链路和远程执行，默认按高敏感项目处理。安全漏洞请遵循 [SECURITY.md](SECURITY.md) 报告，不要公开披露细节。编写文档时，公网部署示例优先使用 HTTPS，并在代码评审说明中明确任何信任边界变化。
