# Contributing / 参与贡献

[中文项目介绍](../README.md) · [English overview](../README.en.md) · [Support / 问题反馈](SUPPORT.md) · [无障碍说明](ACCESSIBILITY.md) · [社区行为准则](CODE_OF_CONDUCT.md)

We welcome pull requests that improve documentation, translations, accessibility, reproducible bug reports, or targeted code fixes in either Chinese or English. Please keep discussions focused on technical behavior and user impact.

欢迎提交文档改进、双语翻译、无障碍优化、可复现的缺陷报告或聚焦的代码修复。中英文交流均可，讨论请聚焦于具体功能行为与技术方案。

## Target branch / 目标分支

Base your work on `preview` and open pull requests against `preview`. The `main` branch is the stable channel and receives only validated promotions from `preview`; ordinary feature, refactor, dependency, and documentation PRs do not target `main`. Release tags and binary attachments are managed separately by maintainers during formal releases.

日常开发与维护均在 `preview` 分支进行，提交 PR 时请将目标分支选为 `preview`。`main` 是稳定通道，只接收已经完成候选验证的 `preview` 提升，不直接承接日常功能、重构、依赖升级或文档 PR。

Merged head branches are deleted automatically when possible. Keep active PR branches until their changes are integrated or explicitly superseded; see [branch lifecycle](reliability/branch-lifecycle.md). Planned upgrades across Node.js, TypeScript or Vue Router major versions require explicit compatibility checks rather than unreviewed automatic major-version PRs.

## Before changing code / 开始前准备

1. Read the [architecture](ARCHITECTURE.md), the relevant module README, and the [UI requirements](UI_UX.md) or [release gates](reliability/release-governance.md) for the affected area.
2. Check existing issues and use the [issue templates](../.github/ISSUE_TEMPLATE/config.yml). See [support](SUPPORT.md) for troubleshooting and [community conduct](CODE_OF_CONDUCT.md) before posting. If you plan to change deployment topology, database responsibilities, or authentication boundaries, open an issue to discuss the design first.
3. Keep each pull request focused. Avoid bundling unrelated refactors, dependency upgrades, or formatting sweeps.

Please preserve the current `preview` architecture: MySQL 8.4 is the sole authoritative store for business transactions, sessions, audit, and image BLOBs; PostgreSQL production projections and S3/local image fallbacks are retired. The desktop UI must remain diagnosable when MySQL is unavailable. Never silently replay user writes after an uncertain result, expose database credentials or desktop bootstrap tokens to page JavaScript, or bypass server-side authorization. For a11y changes, use the [accessibility guidance](ACCESSIBILITY.md) and the `--care-target-*` design tokens; verify actual control sizes and keyboard behavior instead of assuming a token proves compliance.

当前 `preview` 已收敛为 MySQL 8.4 单库，不得重新引入 PostgreSQL 生产投影或 S3/本地图片兜底。写入结果不确定时先核对状态，不能静默重放；数据库凭据与桌面控制令牌不得暴露给前端，权限校验必须由服务端执行。无障碍改动参照 [无障碍说明](ACCESSIBILITY.md) 并验证真实键盘与视口交互。

## Verification requirements / 验证要求

| Change scope | Expected verification |
| --- | --- |
| Documentation or translation | Verify local markdown links, command accuracy, and version/platform descriptions |
| Frontend UI | Run a production build (`npm run build`) and relevant Playwright checks, covering empty/error states and narrow viewports |
| Go backend | Run relevant unit tests and `go vet`; verify transactional changes against isolated MySQL 8.4. PostgreSQL tools apply only to explicit legacy migration work. |
| Desktop shell or packaging | Verify native window launch, graceful shutdown, and installer behavior in addition to compiling |

See [BUILD](BUILD.md) and [TESTING](TESTING.md) for environment setup. Always run automated tests against disposable test databases rather than production data.

验证范围应与改动影响面相匹配：涉及数据库事务的改动需在真实测试库上验证，涉及桌面生命周期的改动需验证原生窗口启动与退出。

## Pull request checklist / 提交清单

- State the problem clearly and describe the updated behavior.
- Link the related issue or reproduction steps, and note any design trade-offs.
- Attach test output, and include screenshots for visual or layout changes.
- Update both `README.md` and `README.en.md` whenever changing user-facing behavior, configuration variables, or build steps.
- Never commit database dumps, real resident records, tokens, connection strings, signing keys, build caches, or compiled binaries.

请在 PR 中说明问题背景、修改方案与验证结果，切勿在代码或测试截图中包含真实人员资料与敏感凭据。

## Security and licensing / 安全与开源许可

Report exploitable security vulnerabilities privately according to [.github/SECURITY.md](../.github/SECURITY.md) rather than in public issues.

All contributions are licensed under the [MIT License](../LICENSE). If you introduce third-party assets or libraries, ensure their licenses are compatible and properly attributed.
