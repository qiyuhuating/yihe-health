# CI 与合并策略

日常 PR 和分支推送只运行受影响的检查；`Core CI / required` 与
`Security gate / required` 始终存在，并核对所需检查成功、未选择的检查确实跳过。

| 变更范围 | 主要检查 |
| --- | --- |
| Markdown、Issue 表单 | PR 的文档链接与表单格式检查 |
| `frontend/`、`tests/ui/` | ESLint、类型、单测、构建；界面相关改动增加浏览器 UI 验收 |
| `backend/` | Go/MySQL、race、vet、构建和 Go 漏洞检查；Windows sidecar 跨平台构建 |
| 设备协议、`YiheWatch/` | 手表协议、真实 MySQL 联调和模拟器；涉及手表的分支推送执行原生编译 |
| `desktop/` | Windows sidecar、实际 Tauri 安装包构建/安装测试 |
| `deploy/` | Compose、监控及部署契约 |
| 依赖清单与锁文件 | PR 依赖审查 |
| CI 自身或未知配置 | 保守运行全部检查 |

仅运行相关语言的 CodeQL。文档混合代码变更时不扩大检查范围。
未知的文件类别、首次推送无法识别的 base SHA、手动 CI 运行均保守执行全量。
选择性 CI **不是正式发布验收**。

## 正式发布

1. 在目标 `preview` 或 `main` 提交上，分别手动执行 `Preview CI` 和 `Security`
   的 `workflow_dispatch`，要求全套检查和 SDK/Windows 构建证据。
2. 所有检查通过后，再手动执行对应的 `Tauri preview candidate` 或
   `Stable release candidate`，提供相同的完整 `source_sha`。
3. 发布门禁核对同一 SHA 的全量检查、构建物及哈希；常规选择性推送的结果不能替代正式发布证据。

## 合并权限设置（需要仓库组织 Owner 在 GitHub Settings 中完成）

`preview` 已采用 0 人审批，仍保留必须通过 `Core CI / required` 和
`Security gate / required`、必须经 PR、禁止强推、要求线性历史和解决讨论。
无需启用 Code Owner 审批或任何独立审批人。

需要**额外**在 GitHub Settings → Rules → Rulesets 为 `preview` 和
`main` 增加一条独立的分支规则：选中 **Restrict updates**，在 Bypass
中**只允许 Organization admins (组织 Owner)**。

当前 `main` 规则仅禁止删除和非快进更新，尚未要求 PR 或 CI。为主分支补充
**Require a pull request**（0 人审批）及上述两项必需检查，且不要给任何
非 Owner 角色设置 bypass，才能做到主分支同样先检查后合并。不要放行 Repository admin、
Maintainer、Write 角色、其他团队或自动化 GitHub App，并确认 Owner 仍须满足
原有 PR 和必需 CI 规则；不要将 Owner 添加到现有 CI/PR 规则的 bypass。

这项规则限制的是目标分支更新，包括 PR 合并，而不是禁止其他人发起 PR。
注意组织 Owner 身份与仓库 Admin 权限不同。规则需在网页实际保存后，用非 Owner
的有写权限账号测试无法合并，再用 Owner 确认可以合并已通过门禁的 PR。
