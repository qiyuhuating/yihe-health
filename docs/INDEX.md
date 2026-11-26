# 项目文档索引

本目录按需要完成的工作组织，而不是按文件添加时间分类。建议从 [中文 README](../README.md) 或 [English README](../README.en.md) 了解用途，再选择对应指南。

## 使用与安装

| 目的 | 文档 |
| --- | --- |
| 下载 Windows 安装版、免安装 ZIP、源码 ZIP | [正式 2.1.0 发行说明](releases/2.1.0.md) · [GitHub 最新发布](https://github.com/WatchTestCs/smart-elderly-watch-management/releases/latest) |
| 首次创建管理员、绑定手表、查看健康数据和告警 | [使用指南](USER_GUIDE.md) |
| 无法启动、无法登录、设备未上报、如何反馈 | [支持与排查](SUPPORT.md) |
| 无障碍设计与反馈范围 | [无障碍说明](ACCESSIBILITY.md) |

## 部署与运维

| 目的 | 文档 |
| --- | --- |
| MySQL 8.4 单库部署、停止旧系统、迁移与恢复 | [MySQL 单库迁移](MYSQL_ONLY_MIGRATION.md) |
| 独立常驻服务与真实照护告警要求 | [照护服务部署](CARE_SERVICE_DEPLOYMENT.md) |
| 业务数据库、加密密钥、图片 BLOB 的备份恢复 | [备份与恢复](runbooks/backup-restore.md) · [图片存储运维](reliability/storage-operations.md) |
| 事故响应与告警演练 | [协调恢复](reliability/coordinated-recovery.md) · [SOS 故障演练](reliability/sos-fault-drill.md) |
| 密钥与数据保护 | [数据保护](reliability/data-protection.md) · [密钥轮换](reliability/rekey-data.md) |

## 开发与贡献

| 目的 | 文档 |
| --- | --- |
| 本机安装 Go、Node、Rust 与 Windows 打包工具 | [构建指南](BUILD.md) |
| 系统模块、数据库、API 和权限边界 | [架构](ARCHITECTURE.md) · [工作区](WORKSPACE.md) · [API 契约](API_CONTRACTS.md) |
| 测试范围、真实 MySQL / 手表 / Windows 验收 | [测试指南](TESTING.md) |
| 接入手表或运行模拟器 | [手表接入](WATCH_INTEGRATION.md) |
| 界面及无障碍设计 | [UI/UX](UI_UX.md) |
| 如何提交 PR、合规和行为规范 | [贡献指南](CONTRIBUTING.md) · [行为准则](CODE_OF_CONDUCT.md) |
| 安全漏洞的私密报告入口 | [安全政策](../.github/SECURITY.md) |

## 仓库维护与发布

| 目的 | 文档 |
| --- | --- |
| 查询版本变更 | [Changelog](CHANGELOG.md) · [发行资产](https://github.com/WatchTestCs/smart-elderly-watch-management/releases) |
| 理解 `main`、`preview` 和过时分支清理准则 | [分支生命周期](reliability/branch-lifecycle.md) |
| PR 文档快速通道与源码必需检查 | [分级验收](reliability/pr-verification.md) |
| 发布规则、CodeQL、发布证据与例外 | [仓库发布保护](reliability/release-governance.md) |
| 正式版候选的 SHA 检查与晋升 | [main 晋升手册](reliability/main-promotion.md) |

旧架构的 PostgreSQL/S3 说明及历史 PR 证据保留在对应源码标签和必要的可靠性记录中，不能将其当成当前部署指南。任何 CI 绿色状态、模拟器成功或自动检查都不代表已完成真实手表、短信网关和生产迁移验收。
