# 颐和 · 智慧康养

[![Frontend checks](https://github.com/qiyuhuating/yihe-health/actions/workflows/frontend-checks.yml/badge.svg?branch=main)](https://github.com/qiyuhuating/yihe-health/actions/workflows/frontend-checks.yml)

面向社区照护场景的原生 HTML、CSS、JavaScript 前端项目，包含公开站点、居民端和工作人员管理端。项目提供本地演示模式与面向同源服务的 HTTP 适配层。

**在线体验：** [GitHub Pages 演示站](https://qiyuhuating.github.io/yihe-health/) · [工作人员管理端](https://qiyuhuating.github.io/yihe-health/管理端.html) · [v0.1.0 发布说明](https://github.com/qiyuhuating/yihe-health/releases/tag/v0.1.0)

## 项目亮点

- **双端业务流程：** 居民健康信息、用药反馈、工作人员预警工作台与事件处置。
- **风险事件状态流转：** 覆盖接单、处理中、解决、误报和升级，并保留操作历史。
- **并发与本地一致性：** 演示数据通过 Web Locks 串行写入、Revision 检查和跨标签同步；冲突或存储失败不会静默覆盖。
- **后端接入准备：** 页面通过稳定调用面访问 HTTP 适配层，统一处理会话、CSRF、超时、错误状态和快照校验。
- **可验证的交付：** 静态规则检查、Node 领域测试、46 项浏览器回归测试，以及 Chromium、Firefox、WebKit 三浏览器 CI。
- **独立生产构建：** CI 构建并检查 HTTP-only 的 `dist/` 静态包；GitHub Pages 当前展示演示模式。

## 架构

```mermaid
flowchart LR
  A[公开页面] --> D[CareAPI 稳定调用面]
  B[居民端] --> D
  C[工作人员端] --> D
  D --> E[演示适配器<br/>本地存储与并发保护]
  D --> F[HTTP 适配器<br/>同源 /api/v1]
  F --> G[待接入的后端服务]
  H[静态与领域检查] --> I[GitHub Actions]
  J[Chromium / Firefox / WebKit 回归] --> I
  I --> K[生产 dist 构建与校验]
```

## 技术特点

- 无前端框架迁移或第三方运行时依赖。
- 演示模式使用虚构数据；写入前后校验数据结构，存储失败时保留原记录。
- HTTP 模式不会在网络失败时回退到演示数据，也不在浏览器中保存服务端密钥。
- 前端提供 API 契约与适配层；认证授权、持久化、事务和通知仍由后端负责。

## 本地运行与检查

需要 Node.js 18+ 和 Python 3。在仓库根目录执行：

```sh
npm --prefix docs run check
npm --prefix docs run test:domain
npm --prefix docs run build
```

开发演示可通过本地静态 HTTP 服务打开 `docs/`。生产构建生成到 `docs/dist/`。

## 项目结构

| 路径 | 内容 |
| --- | --- |
| `docs/` | 页面、脚本、样式、虚构数据与静态资源 |
| `docs/脚本/` | 页面逻辑、演示 API、HTTP 适配层与会话处理 |
| `docs/测试/` | 静态检查、领域测试和浏览器回归 |
| `docs/工具/` | 构建、质量检查和测试环境工具 |
| `docs/文档/` | 架构、前端交接、后端接入和发布说明 |
| `.github/workflows/` | GitHub Actions 检查与 Pages 部署流程 |

## 当前边界

这是**前端 Demo 与单社区 HTTP/SQLite 后端基线**。仓库已包含可运行的服务端与合成数据联调测试；生产数据、正式设备、短信投递、TLS 与运营部署仍需配置和验收。

## 后续路线

1. 按[后端接入说明](docs/文档/handoff/backend-integration.md)实现服务端契约并完成联调。
2. 在真实接口环境验证认证、权限、版本冲突与异常恢复。
3. 按[发布手册](docs/文档/handoff/release.md)部署并验证生产构建。

更多细节见[系统架构](docs/文档/architecture.md)和[前端交接说明](docs/文档/handoff/frontend.md)。


## 最小后端

现提供单社区 Python/SQLite API，包含服务端会话、居民版本比较、信号与事件状态机、设备输入、用药、审计及通知假实现。运行、测试与部署边界见 [backend/README.md](backend/README.md)。静态 GitHub Pages 仍是 Demo；运行真实 HTTP 需要同源后端。
