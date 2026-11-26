# PR 验收分级

`preview` 使用基于文件变更类型的分级验证。规则的目标是在不弱化代码安全边界的前提下，避免文档修改等待与其无关的编译和数据库测试。

## 纯文档 PR

只有以下文件发生变更时才进入快速通道：

- Markdown 文档（`.md`、`.mdx`、`.markdown`）。
- `.github/ISSUE_TEMPLATE/` 下的 `.yml` 或 `.yaml` Issue 表单及配置。

必须检查改变的 Markdown 中的本地链接、GitHub Issue 表单 YAML 结构，以及 PR 差异本身。由于没有可执行代码变更，不运行 Go 后端、前端构建、手表模拟器、安装包检查及 CodeQL 分析。分类失败、空变更或异常路径不会被视为纯文档。

## 源码与配置 PR

任何 Go、Vue、TypeScript、Rust、ArkTS、脚本、依赖清单、Docker、部署配置或 `.github/workflows/` 变更，均执行完整 Core CI 和 Go、JavaScript/TypeScript CodeQL 扫描。不能仅凭 PR 标题包含 `docs` 或改动很少就绕过测试。

针对代码的局部验收应在对应 PR 中保留适用的测试和复现证据。本分级规则不改变真实 MySQL 事务验证、设备协议、Windows 安装验收或生产发布门禁的要求。

## 合并边界

GitHub 分支规则仍禁止强制推送，要求通过 PR、保持线性历史，并解决所有审查线程。`Core CI / required` 对代码变更汇总原有门禁，对纯文档检查独立的快速通道。`Security gate / required` 对源码变更要求 CodeQL 成功，对仅含文档的变更允许跳过分析。任何分类、检查或安全门禁失败，都不能以“纯文档”为由强制合并。

风险分类脚本：`scripts/reliability/pr_change_scope.py`；文档链接检查：`scripts/reliability/check_pr_docs.py`；安全与 CI 汇总分别位于 `.github/workflows/security.yml` 和 `.github/workflows/ci.yml`。
