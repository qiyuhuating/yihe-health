# 测试分层 · 当前 preview

通用命令默认在 Linux VPS 或 Windows WSL 的 Bash 中执行；Windows 原生桌面和 DevEco 构建仍使用 PowerShell 7。

## Go 后端

在 `backend` 目录执行：

```bash
go test -count=1 ./...
go vet ./...
```

需要数据库集成测试时，配置 `TEST_MYSQL_DSN` 指向**独立、可销毁**的 MySQL 8.4 测试实例；测试账号需有创建和删除随机测试库的权限。竞态测试：

```bash
go test -race -count=1 -timeout 600s ./...
```

未配置测试数据库时，相关集成用例可能跳过；不得将跳过报告为真实数据库验收通过。禁止对生产数据库运行自动测试。

### 测试组织与快照实验

普通测试默认创建独立随机 schema，执行完整迁移并清理。设置 `TEST_MYSQL_FIXTURE_SNAPSHOT=1` 可让已接入 `dbtest.Fixture` 的普通业务测试在各自进程内复用 pristine SQL 快照。每个测试仍恢复到新建空 schema，并验证迁移账本及结构指纹。恢复后的只读校验一次读取全部迁移记录，一次检查检查点及账本引擎，并复用生产迁移器的完整结构指纹算法扫描一轮。遇到缺失、未知、校验值不符或结构漂移直接失败，不尝试重放迁移或修补账本。显式迁移、回滚、备份恢复和连接级测试继续使用完整迁移。

跨包实验使用以下命令（仓库根目录，已配置隔离 MySQL 8.4 实例及匹配客户端或测试容器）：

```bash
python3 scripts/reliability/run-mysql-fixture-tests.py ./internal/db/dbtest ./internal/handlers ./internal/session ./internal/notify ./internal/operationsrepo ./internal/telemetryrepo ./internal/careworkflow
```

该脚本仅从新建并完整迁移的空业务库导出一次归档，验证服务器 UUID、嵌入迁移文件内容摘要及归档哈希；所有消费者使用各自独立的新建数据库，不共享测试数据。每次运行使用专用临时目录，结束后清除归档，不跨作业缓存。省略包参数可运行整个后端套件；未接入的测试仍走完整迁移。共享模式不得用于生产数据库。 空库快照导出省略冗余 DROP TABLE、导入表锁和非唯一索引禁用/启用指令，仍保留全部表、数据、触发器及例程。快照模式复用有界管理连接池执行 CREATE/DROP DATABASE，业务连接仍按测试独立创建和关闭。每个恢复的 schema 仍须执行完整结构校验；协调备份恢复演练不使用精简导出选项。

在同一实例及相同包集合上，以完整迁移模式的 `go test -race -count=1 -timeout 45m` 作为对照；比较总耗时、准备耗时及失败和跳过记录。准备耗时也必须计入共享模式的总成本。脚本默认不改变 CI 或生产配置。 可使用脚本参数 `-v -run TestMySQLSnapshotIsolationAndCost ./internal/db/dbtest` 查看创建/连接、归档准备、导入及只读校验分段耗时。

仅在行为一致且真实耗时对照证明有收益后考虑默认启用。不要把静态减少初始化次数当作实际 CI 加速。相同输入与断言已有真实行为覆盖时，不再重复检查 SQL 拼写；保留独立授权、事务、竞态、恢复、加密与边界断言。复用同一测试 fixture 时必须保留状态隔离及所有关键断言。


## Vue 前端

在 `frontend` 目录执行：

```bash
npm ci
npm test
npm run typecheck
npm run lint
npm run api:check
npm run test:api-drift
npm run build
```

在仓库**根目录**运行浏览器端 Playwright 验收：`npm --prefix frontend/qa ci`，随后执行 `npm --prefix frontend/qa test`。在 `frontend/` 内运行时则使用 `npm --prefix qa ...`。根目录 `tests/ui/` 是独立的 UI 质量检查，具体启动方式以 `.github/workflows/ui-quality.yml` 为准。模拟接口测试与截图不能替代真实后端、WebView2 及硬件验收。

## HarmonyOS 手表端

在 `YiheWatch` 目录执行：

```bash
npm ci
npm test
```

协议测试基于适配器转译生产 ArkTS 模型，不等价于 DevEco 原生编译、模拟器交互、传感器权限验证或真机验收。原生构建要求与当前验证边界见 [手表工程说明](../YiheWatch/README.md)。

## Windows 桌面端

按 [Windows 构建说明](BUILD.md)准备 MSVC、Windows SDK、Rust、Go、Node.js 与 PowerShell。执行 Tauri 构建、sidecar 生命周期、安装/卸载及 WebView2 缺失补装检查。编译成功不能替代真实安装环境和可视交互验收。

## 生产安全边界

- 不对生产数据库执行自动化测试或迁移演练。
- 未确认成功的用户写入不得自动重放；通知 outbox 或其他明确设计为幂等的后台任务可按各自协议重试。
- 手表协议测试不代表真实硬件、SIM/网络或通知网关验收。
- 网关接受请求不等于通知送达；送达和现场处置必须分别验证。
- 当前提交的 CI 状态以 GitHub Actions 对应提交的完整检查结果为准。
