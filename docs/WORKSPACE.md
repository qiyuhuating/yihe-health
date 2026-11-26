# 工作区与职责边界

本仓库当前使用 Tauri 2 / Rust、Go、Vue 3 和 MySQL 8.4。当前唯一持久化业务数据库是 **MySQL**。PostgreSQL/S3 仅是历史版本迁移来源，不是运行时依赖。

| 目录 | 责任范围 | 关键入口 |
| --- | --- | --- |
| `../backend` | HTTP API、设备鉴权、会话/权限、照护事务、MySQL 迁移与加密图片 | `cmd/server`、`internal/handlers`、`internal/db` |
| `../frontend` | Vue 路由、页面、同源 HTTP 客户端、交互与无障碍 | `src`、`tests`、`qa` |
| `../desktop` | Tauri 生命周期、受限初始化 IPC、本地 sidecar 启动与校验 | `src/main.rs`、`build.ps1` |
| `../YiheWatch` | HarmonyOS 手表应用与协议验证 | `entry`、`scripts` |
| `../watch-sim` | Rust 手表上报/断网模拟 | `Cargo.toml`、`src` |
| `../deploy` | MySQL 单库 Compose、网关及监控 | `platform.compose.yml`、`Dockerfile` |
| `../scripts` | 部署、API 契约生成、CI 和回归脚本 | `generate-openapi.py`、`reliability` |

## 接口与数据边界

浏览器通过同源 `/api/v1/*` 访问 Go 服务，不能直接操作数据库或暴露桌面初始化令牌。设备上报和 SOS 走独立设备鉴权。事务、会话和加密图片全部由 MySQL 负责。数据迁移期间必须先备份旧 MySQL 和原始图片；迁移服务未准备就绪时业务路由拒绝请求。历史分析状态响应字段为兼容旧客户端保留，不代表第二数据库仍在运行。

## 开发验证

PowerShell 7 中分别从相应目录执行：

```powershell
# 仓库根目录
python scripts/check-api-boundary.py
python scripts/check-go-module-boundaries.py
python scripts/generate-openapi.py --check

# Go 服务
Push-Location backend
go test ./...
go vet ./...
Pop-Location

# Vue 客户端
Push-Location frontend
npm ci
npm run lint
npm run typecheck
npm test
npm run build
Pop-Location
```

真实 MySQL 集成测试必须使用隔离的 MySQL 8.4 测试库及 `TEST_MYSQL_DSN`，不能连接生产数据库。Windows 本机构建运行 `./desktop/build.ps1`，HarmonyOS 原生 SDK 和实体设备验收需另行执行。不得把模拟器通过描述为真实设备验收完成。

详情：[MySQL 迁移与回滚](MYSQL_ONLY_MIGRATION.md)、[架构历史与边界](ARCHITECTURE.md)、[照护运行部署](CARE_SERVICE_DEPLOYMENT.md)、[发布门禁](reliability/release-governance.md)、[仓库 README](../README.md)。
