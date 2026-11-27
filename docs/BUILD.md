# 构建指南

## Linux / WSL 开发

Linux VPS 与 Windows WSL 默认使用 Bash，安装 Git、Node.js 26.x、Go 1.27.4；按需安装 Rust 1.100.0 或更高版本、Python 3、Docker 和 MySQL 8.4 客户端。WSL 建议在 Linux 文件系统（如 `~/projects`）检出源码。仓库根目录运行：

```bash
(cd frontend && npm ci && npm run typecheck && npm run build)
(cd backend && go test -count=1 ./... && go vet ./...)
```

数据库集成测试使用独立测试实例。Linux/WSL 可运行通用开发、容器和协议测试，但不能替代 Windows 原生桌面安装验收。

## Windows 原生构建

准备 Windows x64、C++ Build Tools（MSVC + Windows SDK）、Rust stable（最低版本 1.100.0，见 `desktop/Cargo.toml`）、Go 1.27.4（与 `backend/go.mod` 和 CI 一致）、Node.js 26.x（LTS）、PowerShell 7。推荐在开发者 PowerShell 环境中构建。前端依赖由 `frontend/package-lock.json` 锁定，Tauri/Rust 依赖由 `desktop/Cargo.lock` 锁定。

```powershell
# Work from the repository root
./desktop/validate-version.ps1
./desktop/build.ps1
./desktop/package-release.ps1
# 可选：明确标注为本机自签，不是公开受信任证书
./desktop/build.ps1 -SelfSign
```

按顺序执行前端安装/构建、嵌入静态资源、Go 测试及编译、生成 sidecar 哈希、Tauri 原生编译、NSIS 打包。默认从干净源码构建，避免将旧生成资源带入安装包。

安装包产物位于 `outputs/` 对应构建目录，附校验和及可选签名记录。当前源码使用 `downloadBootstrapper`、`silent: true`：不附带 WebView2 运行时或引导程序。安装时检测已有组件，缺失时才联网下载微软引导程序并静默补装；下载或补装失败时安装器报错，不把缺少组件的状态当成成功。WebView2 安装为 Windows 共享组件，卸载颐和不会移除它。缺少组件且无法联网时不能完成安装。当前构建默认未签名；脚本支持 `CARGO_TARGET_DIR`。

数据库服务不需要为纯编译保持运行；需要验证数据库集成测试时，仅使用指向独立实例的 `TEST_MYSQL_DSN`。未配置时相关测试可能跳过，不能将跳过报告为数据库集成验收通过。直接 Go 开发可使用纯 API 模式；桌面打包必须嵌入真实前端。

Tauri 官方参考：[Windows 前置条件](https://v2.tauri.app/start/prerequisites/)、[Windows 安装包](https://v2.tauri.app/distribute/windows-installer/)、[sidecar](https://v2.tauri.app/develop/sidecar/)。

`package-release.ps1` 可在隔离临时目录检查便携包和安装后启动、内嵌页面、父进程终止清理及卸载保留资料；本地打包产物保存在 `outputs/` 目录下。版本专属发布说明（`docs/releases/<版本>.md`）为可选文件；未提供时，打包脚本自动生成仅包含版本、渠道和源码提交的说明。使用有 WebView2 的 Windows 构建环境。该检查不连接生产数据库，也不代表完整可视验收。

## 容器构建入口

Dockerfile 位于 `deploy/Dockerfile`，构建上下文仍是仓库根目录，`.dockerignore` 保留在根目录。从仓库根目录执行：

```bash
docker build -f deploy/Dockerfile -t yihe:local .
```

`deploy/platform.compose.yml` 已使用同一 Dockerfile 路径；完整平台配置与密钥准备见项目 README。

## 签名

默认不签名。`-SelfSign` 使用本机证书，不自动安装根证书，也不消除 SmartScreen。
正式证书通过 `YIHE_SIGNING_THUMBPRINT` 和 `YIHE_TIMESTAMP_SERVER` 配置；`sign.ps1` 校验证书用途、私钥、有效期和时间戳。私钥、PFX 与密码不得进入仓库或 Release。

顺序：签 sidecar → 计算内嵌哈希 → 签主程序 → bundler `--no-binary-patching` → 签 Setup → 生成校验和。
