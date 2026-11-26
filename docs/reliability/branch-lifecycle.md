# 分支生命周期、保护与清理

`main` 是稳定发布通道；`preview` 是日常集成和 PR 的目标分支。两者当前使用明确的 GitHub 保护规则：`preview` 必须经过相应的 PR 检查，`main` 禁止删除和非快进历史改写；正式晋升按 [main 晋升手册](main-promotion.md) 进行。

## 分支什么时候可以删除

- 已合并的同仓库 PR Head：合并后自动删除，CI 和发布引用以不可变 SHA 与 tag 为依据，不依赖工作分支持续存在。
- 已关闭但未合并的 PR Head：确认其改动已在其他提交中实现，或经维护者决定明确废弃后才删除；不要仅凭名称或日期删除。
- 不存在 PR 的工作分支：先核对远端完整提交与 `main`/`preview` 是否重合，以及是否被标签、发布任务、部署或其他工作依赖。存在尚未整合的唯一改动时保留。
- 对应仍开放 PR、发布候选、迁移演练或需要回复维护者的分支：保留至最终处理，不因为它们“看起来旧”而删除。
- `main`、`preview` 与 Git 标签：不得作为常规清理对象。已发布的 tag 尽量保持不可变，避免源码与二进制无法一一对应。

## 新分支命名与维护

`docs/` 用于文档，`fix/` 用于缺陷，`ci/` 用于仓库工作流，`chore/` 用于维护；依赖更新可由 Dependabot 创建。不要求为了命名规范而重命名正在被 PR 引用的分支。新 PR 的目标默认是 `preview`；功能或依赖大版本升级先说明目标工具链、兼容性和实际验证计划。

## 稳定发布与异常操作

正式发行前固定候选 40 位 SHA，发布附件、`release-manifest.json` 和 `SHA256SUMS.txt` 必须对应同一源码版本。`main` 原则上仅快进至已验收候选，不用 squash 改写经过验证的发布源码。管理权限带来的临时规则绕过必须明确记录原因、前后状态与恢复结果，完成后重新读取 GitHub 规则确认保护有效。

不能把 PR 合并 CI 的通过视作完整生产验收。若维护者明确要求跳过部分验收，发布记录需列出实际运行、跳过项、影响和风险；仓库徽章继续显示 GitHub 的真实状态。

## 日常核对

对已经使用的仓库管理员，可按需从 GitHub 查看：

- [分支](https://github.com/WatchTestCs/smart-elderly-watch-management/branches)和[开放 PR](https://github.com/WatchTestCs/smart-elderly-watch-management/pulls)；确认分支没有依赖关系后才删除
- [规则设置](https://github.com/WatchTestCs/smart-elderly-watch-management/settings/rules)和[Actions](https://github.com/WatchTestCs/smart-elderly-watch-management/actions)；核对生效配置与实际任务状态
- [版本发布](https://github.com/WatchTestCs/smart-elderly-watch-management/releases)；确认 SHA、安装包、签名状态和校验清单
- [Dependabot 告警](https://github.com/WatchTestCs/smart-elderly-watch-management/security/dependabot)；不将没有修复或没有证据的中危告警标记为已解决

保护优先级是：不遗失尚未整合的工作、不重写已发布源码、不削弱代码变更安全验证；清理以可核查事实为依据。
