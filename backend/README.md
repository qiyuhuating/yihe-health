# 颐和 Go 后端

Go 服务端负责业务 API、设备鉴权与遥测、会话/权限、照护任务、通知作业及加密图片，使用 MySQL 8.4 作为唯一持久化业务数据库。Windows 桌面端由 Tauri 托管 Go 进程；全天候手表上报需要独立常驻的 Go 服务。

[当前工作区](../docs/WORKSPACE.md) · [MySQL 迁移与恢复](../docs/MYSQL_ONLY_MIGRATION.md) · [照护服务部署](../docs/CARE_SERVICE_DEPLOYMENT.md) · [源码与构建](../README.md#源码与构建)

## 技术栈与代码结构

Go 模块名为 `yihe-care`，使用 Go 1.27.4；HTTP 层基于标准库 `net/http`，MySQL 使用 `database/sql` 和 `go-sql-driver/mysql`。不依赖 PostgreSQL、S3 或外部队列。

| 目录 / 入口 | 职责说明 |
| --- | --- |
| `cmd/server` | HTTP 服务启动、后台数据库重连、自动迁移、桌面端就绪握手与父进程存活监测 |
| `internal/handlers` | 账号、档案、设备、健康、公告、照护与诊断 API，以及图片读写和内嵌静态资源服务 |
| `internal/auth` / `session` / `csrf` / `ratelimit` | 角色权限校验、BCrypt 密码哈希、会话管理（默认存数据库，可选单实例内存模式）、CSRF 防护与请求限流 |
| `internal/care` | 告警通知投递、超时升级与照护工单后台作业 |
| `internal/db` | MySQL 连接池、独立健康探针、物理连接语句缓存与数据库迁移；业务直接使用标准 `database/sql` 契约 |
| `cmd/migrate` | MySQL 业务库的独立迁移命令 |
| `migrations` / `VERSION` | 编译期通过 `embed` 内嵌的 SQL 迁移脚本与统一版本号文件 |

`internal/db` 管理 MySQL 连接、迁移与就绪检查；事务与 SQL 操作遵循 `database/sql` 的原生语义。MySQL 故障不会触发其它数据库的自动接管。

## 运行与环境变量配置

MySQL 驱动默认启用客户端参数插值（可通过在 DSN 中显式指定 `interpolateParams=false` 切回二进制预编译语句）。在 `backend` 目录下配置环境变量即可直接启动服务：

```powershell
$env:MYSQL_DSN='yihe:替换密码@tcp(127.0.0.1:13306)/yihe'
$env:ADDR='127.0.0.1:8795'
$env:DATA_ENCRYPTION_KEY_FILE='替换为已备份的持久化密钥文件路径'
go run ./cmd/server
```

| 环境变量 | 说明 |
| --- | --- |
| `MYSQL_DSN` | 包含数据库名的 MySQL 连接串，必填，无默认密码 |
| `MYSQL_MAX_IDLE_CONNS` | MySQL 业务连接池最大空闲连接数，默认 20（取值范围 0 至 20；若数据库连接数受限可设为 5） |
| `HEALTH_LATE_DEVICE_LOCK` | 实验性健康写入加锁顺序开关，默认关闭；仅可在所有写入实例停机排空后统一设为 `1`，切勿在滚动升级中混用 |
| `BLOB_BACKEND` | 当前仅支持 `mysql`，PostgreSQL、S3 和本地目录不可作为运行时图片存储 |
| `ADDR` | 监听地址，直接启动时默认为 `127.0.0.1:8080`，与前端 Vite 联调时建议设为 `127.0.0.1:8795` |
| `SESSION_TTL_HOURS` | 登录会话有效期，默认 12 小时 |
| `SESSION_BACKEND` | 会话存储后端，默认 `database`；`memory` 仅用于单实例本地开发 |
| `HEALTH_HOT_CACHE_MIB` | 健康统计进程内缓存上限，默认 16 MiB（范围 0 至 256，`0` 表示禁用） |
| `DATA_ENCRYPTION_KEY` / `DATA_ENCRYPTION_KEY_FILE` | 32 字节随机密钥的 Base64 字符串或密钥文件路径，多实例部署时必须共享同一密钥并持久保存 |
| `METRICS_TOKEN` / `METRICS_TOKEN_FILE` | 访问 `/metrics` 接口所需的 Bearer 令牌，长度至少 24 个字符 |
| `YIHE_DESKTOP_TOKEN` | 桌面端用于首次初始化与安全退出的随机令牌，仅在原生进程与 Go 之间传递 |

桌面模式中，Rust 从 `YIHE_MYSQL_DSN` 传递 MySQL 连接参数，注入本地进程端口和受限启动令牌；数据库密钥必须通过持久化配置提供。架构与权限边界见[工作区说明](../docs/WORKSPACE.md)。也可运行仓库根目录下的 `python scripts/start-platform.py` 启动单库容器环境。

执行桌面完整打包前，需先在 `frontend` 目录运行 `npm run build`，将生成的前端静态资源复制到 `internal/handlers/web` 供 Go 编译内嵌。

## 健康统计缓存与数据一致性

`POST /api/v1/health/statistics` 使用进程内 LRU 缓存加速重复查询，按 `(elder_id, date_from, date_to)` 隔离键空间，最多缓存 2,048 项（单项上限 64 KiB，TTL 为 30 秒）。

每次统计请求都会先校验权限，并从 MySQL 读取对应长者的最新数据修订号（revision）。一旦任何实例通过 `health.SaveBatch` 写入、修改或删除了健康样本，修订号即会递增，使旧缓存自然失效并重新计算，无需跨实例网络广播。可通过 `/metrics` 中的 `yihe_health_cache_hits_total` 与 `yihe_health_cache_misses_total` 观察缓存命中情况。

## 健康写入加锁顺序（实验性）

默认情况下，`healthBatch` 遵循 `device 行锁 -> health_owner_clock 行锁 -> 样本与历史事务提交` 的加锁顺序，确保单设备高频并发上报与告警规则判定时的数据一致性。

当启用 `HEALTH_LATE_DEVICE_LOCK=1` 时，加锁顺序反转会变为 `health_owner_clock -> device`。为避免死锁，切勿让新旧两种加锁顺序的实例同时运行，切换前必须先停机排空所有应用实例。

## Go 运行时内存与容器 CPU 调度

- 规则判定生成的证据 JSON 按单次设备上报调用复用 512 B 私有缓冲区，仅在 `tx.Exec` 同步返回后回收复用。
- 在 Linux cgroup v2 容器环境中，设置 `YIHE_RUNTIME_AUTOTUNE=1` 可让进程在启动时自动读取 `cpu.max` 与 `memory.max` 并调整运行参数（若管理员已显式设置 `GOMAXPROCS` 或 `GOMEMLIMIT`，则以显式配置为准）。

## 事务流转与存量数据迁移

业务持久化采用[原生 MySQL 架构](../docs/NATIVE_MYSQL_ARCHITECTURE.md)：仓储显式提供 SQL 与按出现顺序排列的 `?` 参数，工作流持有 `*sql.Tx` 并负责提交。执行路径不再包含 SQL 翻译、参数重排、全局查询计划缓存、自定义事务或结果对象。

建档、设备绑定、SOS 求助和照护处置由 MySQL 事务保证一致性。历史 analytics outbox 表仅作为只读迁移材料保留；新服务不向 PostgreSQL 异步投影。图片密文存放在 MySQL `image_payload` 中。

服务端启动时自动执行内嵌 MySQL 迁移；运维也可通过 `go run ./cmd/migrate` 单独执行。业务数据恢复使用 MySQL 备份与恢复流程。

## 本地测试与回归

```powershell
go test -count=1 ./...
go vet ./...
# 在独立测试库上运行完整竞态与集成测试（切勿指向生产库）
$env:TEST_MYSQL_DSN='测试MySQL原生DSN'
go test -race -count=1 -timeout 240s ./...
```

## 离线加密回填命令

从 `backend/` 目录执行 `go run ./cmd/yihe-ctl protect family-addresses --limit 100` 可只读盘点待迁移数据；追加 `--apply` 才会执行写入。可选目标还有 `family-phones`、`guardian-contacts`、`resident-birthdates`、`resident-profiles` 和 `social-phones`。原有 `protect-*` 命令仍可使用，统计 JSON 格式保持不变。

所有写入前必须停写、验证备份并配置数据加密密钥。密钥和数据库连接均支持 `DATA_ENCRYPTION_KEY_FILE` / `MYSQL_DSN_FILE`；已配置但无法读取的密钥文件会使服务配置验证失败。

`go run ./cmd/protect-notes --limit 100` 在最多处理指定条记录后停止，单批读取不超过 64 条、无全表 `FOR UPDATE`。它默认只读盘点，追加 `--apply` 执行加密；`--decrypt --apply` 会恢复明文，仅限经审批的离线回滚。单次操作最多 2 分钟，出现 `capped=true` 后需检查剩余记录并分批重跑。加密期间不得有业务写入。

运维性能参考[性能指南](../docs/reliability/performance.md)，迁移、回滚与数据恢复参考[MySQL 单库指南](../docs/MYSQL_ONLY_MIGRATION.md)。

## 接口路由与会话约定（破坏性变更）

服务端仅公开 `/api/v1/` 路由前缀，已全面移除无版本号的 `/api/` 路径且不设回退别名。浏览器登录会话统一使用 HttpOnly Cookie `YIHE_SESSION`，客户端需全局处理 `401`、`403` 与 `503` 状态码。
