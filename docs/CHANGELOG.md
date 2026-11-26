# Changelog / 更新日志

本文件按版本记录面向用户与运维人员的主要变更。各版本的详细测试清单与构建哈希见对应的 Release 页面。

## 2.1.0 正式版（v2.1.0+20261122）

[更新说明及安装迁移指南](releases/2.1.0.md)

- Windows Setup、Portable ZIP、源码 ZIP 和无字体原生 Go 服务端统一打包与哈希校验。
- 采用 Go、Vue、Tauri 和 MySQL 8.4 单库生产架构；健康数据加密、前端性能和社区入口完成整合。
- 本轮按维护者要求跳过新 SHA 完整远端 CI，本机实际验收以发行清单为准。

## 2.0.7 历史预览归档（原发布 v2.1.0）

2026-10-22 的原始预览 Release 现归档于 [`v2.0.7`](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/tag/v2.0.7)，仍使用原源码 `e3cf064`、未重新编译的旧附件（程序内部版本 **2.1.0**）。旧 Git 标签 `v2.1.0` 保留用于源码追溯；与 2026-11-22 正式 `v2.1.0+20261122` 不是同一发布。

## 2.1.0 原始预览构建（历史记录）

[2.1.0 发布与升级说明](releases/2.1.0.md)

- 基于同一 `preview` 提交构建 Windows x64 安装版、免安装版与源码包，统一产品版本号为 2.1.0，并提供 SHA-256 校验清单。
- 合入 2.0.6 之后的 Go 后端修复，包括队列持续排空、诊断采样与按日缓存写入优化（PR #24）。
- 将 MySQL 业务连接池默认最大空闲连接数调整为 20（PR #25），并提供环境变量回退配置。
- 在打包流程中新增隔离目录安装、免安装版启动及卸载保留用户数据检查。

## 2.0.6：界面统一、照护闭环与双库架构

[v2.0.6 Release](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/tag/v2.0.6)

- 统一导航栏与页面标题，支持按当前路由自动展开菜单分组，补齐窄屏资料卡片、加载提示与失败重试交互，优化宽屏总览栅格布局。
- 修正建档与设备绑定先后依赖、设备归属一致性、监测阈值保留、饮食与用药字段对齐，以及未上报指标误显示为正常的问题。
- 打通 SOS 紧急求助从接单、处置登记、另一名主管独立复核到关联告警关闭、总览刷新及分页履历查询的完整闭环。
- 切换为 MySQL 业务主库加 PostgreSQL 异步分析库的双库架构，配套提供 Docker Compose 模板、旧版 PG 离线导入工具与分析投影回填命令，详见该版本对应的 Release 与源码标签。

## 2.0.2：切换至 Tauri / Go 与独立数据库

[Release: v2.0.2-preview.1](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/tag/v2.0.2-preview.1)

- 将桌面端架构从 Electron + Java 迁移至 Tauri 2 (Rust) + Go sidecar，前端保持 Vue 3 + Vite。
- 数据库改为外部独立部署；支持在未连接数据库时打开桌面界面并提示连接状态，后台自动重连。
- 在数据库离线时仍允许执行本地登出，同时保留会话与 CSRF 校验。
- 提供轻量级 Windows Setup 安装包、Portable 免安装归档与对应提交的源码包。

## 2.0.1：早期 Windows 预览版加固

[Release: v2.0.1-preview.1](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/tag/v2.0.1-preview.1)

- 加固设备密钥存储、外键关联完整性与删除处理逻辑，扩充回归测试覆盖。
- 重构数据库连接管理、密码哈希与日志输出。

## 2.0.0：桌面端基础架构

[Release: v2.0.0-preview.1](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/tag/v2.0.0-preview.1)

- 建立 Tauri 2 桌面壳、Vue Web 界面与 Go sidecar 的基础工程结构。
- 实现基础长者档案管理、手表登记、位置告警与首版发布文档。
