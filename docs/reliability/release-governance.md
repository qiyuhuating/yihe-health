# 仓库和发布保护

本文记录实际分支规则、发布验证政策及历史配置记录。GitHub 仓库设置是最终生效来源；策略变化需重新读取对应 ruleset 并在这里更新。

## 当前分支保护（2026-11-22 已核对）

- `preview` 的 ruleset `26444520` 处于 `active`：禁止删除与非快进推送，要求通过 PR、解决审查线程并保持线性历史。批准人数为 **0**，不强制基于最新 `preview` 重跑无关 CI。
- 仅要求两个汇总状态：`Core CI / required` 和 `Security gate / required`。前者按文件变更范围检查相应任务；后者对源码变更要求 Go 与 JavaScript/TypeScript CodeQL。纯文档 PR 允许通过分类器跳过无关构建，具体见 [PR 分级验证](pr-verification.md)。
- `main` 只允许经过明确验证的发布快进。单独的稳定分支规则禁止删除及非快进更新，不要求为了快速前移 `main` 创建多余的 PR。
- 目前没有强制独立审批人；涉及权限、加密、迁移及发布的改动仍需维护者主动开展相应复核。不可把 `0` 人批准误写为已经过独立代码审查。

详见 [分支生命周期与清理准则](branch-lifecycle.md)，不要用 11 月 13 日或 21 日的历史快照推断当前检查数量。

建议保护规则至少包括：禁止未经审查的直接推送、force push 和分支删除；要求独立 reviewer；
最后推送者不能自行批准；新提交使旧批准失效；合并前必须通过当前 SHA 对应的必需检查。
字段说明见 [GitHub rules REST 文档](https://docs.github.com/en/rest/repos/rules#create-a-repository-ruleset)。
GitHub Actions 检查来源与名称应以仓库实际 check-runs 为准，不应硬编码未经复核的 integration ID。
CodeQL 或依赖审查因 fork 权限、配置缺失而未运行时，应由维护者在可信分支重新执行，不能把跳过当成通过。

Security 使用普通 `pull_request` 事件扫描 fork 提交，不因来源仓库不同而跳过。
该事件的 fork token 由 GitHub 限制为只读，工作流不引用 secrets；不得改为在
`pull_request_target` 中检出并执行不可信源码。GitHub 对普通 PR 事件允许上传 CodeQL
结果，详见 [上传权限说明](https://docs.github.com/en/code-security/reference/code-scanning/troubleshoot-analysis-errors/resource-not-accessible)。
检出固定 head SHA 且不持久化凭据，分析前核对实际 HEAD 与预期 40 位 SHA，
并在 `codeql-<language>-<SHA>` 制品中同时保存 `codeql-source.txt` 和 SARIF。
源码核对失败或扫描失败仍使 job 失败；上传步骤成功不能代替分析成功，也不能将 PR 扫描作为正式发布认证。

## 历史规则快照（仅供审计，不是当前配置）

2026-11-13 的实际配置快照：上游既有 ruleset `26444520` 保持 active，仅匹配
`refs/heads/preview`，无 bypass，保留原有 11 项必需检查、禁止删除和非快进等规则。
将原先的 0 人批准修正为至少 1 人批准，开启新推送撤销旧批准与最后推送需他人批准。
保存后重新读取规则核对；GitHub 对 #146 返回 `REVIEW_REQUIRED` 和 `BLOCKED`。
这证明当前规则及服务端评估结果，不表示已有独立批准，也不冒充失败/缺失检查下的真实合并调用测试。
按本次授权没有尝试自合并、直接推上游或绕过保护；完整发布门禁和独立审查仍须另行完成。

2026-11-21 重新读取同一规则：管理员已将名称改为
`Preview CI-gated PRs (no required approval)`，要求批准数为 0，撤销旧批准与最后推送需他人批准均关闭；
active、preview 范围、无 bypass、11 项必需检查及禁止删除/非快进仍保留。
其中必需 `CodeQL / go` 必须实际执行。尊重管理员后续配置，没有自动恢复旧审批设置；
原验收要求中的独立审批仍未满足，不能沿用 11 月 13 日快照声称当前仍强制审批。

## 预览交付

预发布只面向 `preview`，不自动更新 `main`。附件、源码快照和校验清单必须对应同一版本与提交；旧 Release 不包含后续分支改动。普通 PR/CI 不读取发布私钥，签名与发布只在明确的 release 流程执行。

## 分支通道与 main 晋升

`preview` 是预发布集成通道，允许经过评审的功能、架构、依赖与破坏性变更；`main` 是稳定通道，不接收日常开发 PR。正式晋升必须绑定一个明确的 40 位 `preview` HEAD SHA，并在候选冻结后只接受缺陷、安全、构建或发布阻断修复。

晋升前先在候选 SHA 上运行完整 Preview CI、Security 与候选打包，确认 release gate 证据完整。晋升时不得用 squash 或 rebase 生成未经候选验证的新源码提交；稳定主线应直接采用已经验证的候选 SHA。首次从历史 `main` 切换到新架构前，必须先保存旧 `main` 的不可变标签并记录其 SHA。首次历史分叉已于 2026-11-12 按 [晋升手册](main-promotion.md) 完成一次性世代迁移；之后要求 `main` 始终是 `preview` 的祖先，常规晋升只允许 fast-forward。

候选 SHA 提升到 `main` 后，必须在 `main` 上重新运行同一套分支敏感 CI 与 Security，并通过 Stable release candidate 的 exact-source 检查后才能创建正式版本标签。若 `main` 出现独立提交或两条分支失去祖先关系，应停止常规晋升，先显式解决历史关系，不能用强制合并掩盖分叉。

详细步骤见 [main 晋升手册](main-promotion.md)。

## 当前发布门槛

`Core CI / required` 是日常合并的基础检查，不表示已经通过正式发布验收。
UI Quality 由 Preview CI 在每个候选提交上调用，不再按前端路径过滤；复用工作流明确
检出 PR head SHA，UI 日志与图片归入 `ui-quality-<SHA>`，不自动更新视觉基线。

`preview` 与 `main` 的分支推送自动启用完整原生 Preview CI，并启动对应通道的候选工作流；
候选工作流等待同一 SHA 的 CI 与 Security 成功，再执行来源与产物门禁。手动复验时仍需启用
`require_native` 与 `build_desktop`。原生构建按显式 SDK URL、仓库 Secret `HARMONY_SDK_DOWNLOAD_URL`、仓库变量 `HARMONY_SDK_URL` 的顺序选择下载地址。SHA-256 优先读取显式输入与仓库变量 `HARMONY_SDK_DOWNLOAD_SHA256`，再读取
`HARMONY_SDK_SHA256`。缺少配置必须失败；普通 PR 的可选原生 job 被跳过时，只能声明
基础检查通过。SDK 工具链固定为 Command Line Tools 26.0.0.865 / Hvigor 6.26.9 /
HarmonyOS SDK 26.0.0.105（API 26），版本与归档 SHA-256 见 `YiheWatch/toolchain.json`。
URL 必须为 HTTPS；传入摘要须与版本清单一致，并在解压和执行前核对实际归档 SHA-256。
切换归档时应同步核对下载地址和摘要。华为签名下载地址应通过 `HARMONY_SDK_DOWNLOAD_URL` 配置，失效后更新 Secret，
不得把签名查询参数写入源码或证据。解压脚本支持 Windows 长路径，并校验全部文件 CRC；
中断后保留目录供排查，重新执行应选用新的空目标目录。

`scripts/reliability/release-gate.py` 对正式候选独立检查 UI、Go 1.27.4、手表协议与真实
后端联调、原生 HAP、模拟器协议、Windows sidecar、实际 Tauri 构建和安装/卸载测试、
部署合同及漏洞扫描。另查两项 CodeQL 结果。每项必需 job 必须在最新运行 attempt 中
明确完成且成功，不能由聚合 job 的绿色状态代替，也不接受 skipped/cancelled。
源码相同但来自其他分支或 PR 事件的运行不用于正式发布认证。

必需 artifact 包括 `ui-quality-`、`watch-native-`、`watch-integration-`、
`windows-desktop-` 等 SHA 命名前缀。运行、job 与 artifact 均遍历分页；缺失、重复、
过期、空归档、摘要不合法或来源不一致都会阻断。普通 CI 绿色但没有完整软件证据时，
`preview-candidate` 仍会在打包前被拒绝。这是阻断状态，不是发布已通过。

Preview CI 与 Security 工作流分别验证当前提交的后端、数据库、race、迁移与恢复、S3、
OpenAPI、前端 test/typecheck/build/audit、Windows sidecar 及漏洞扫描。具体执行范围以当前
workflow 文件和对应 commit 的 job 结果为准；未配置的数据库测试、跳过的原生 SDK 编译、
未进行的真实设备或生产环境演练不得报告为通过。

Go 漏洞扫描同时保存 JSON 与文本报告。`govulncheck -json` 即使发现漏洞也返回 0；
必须再用工具自身的文本转换器检查同一份 JSON，并通过 Bash pipefail 保留漏洞退出码。

发布门禁应校验明确的 40 位 commit SHA 与目标分支 head 一致，防止验证期间分支移动；选择
该 SHA 对应的最新工作流运行，拒绝用旧的成功运行覆盖新提交的失败/取消结果；同时核对聚合
job、SHA 命名 artifact、有效期和 SHA-256 digest。Release preparation 与 Windows installer
入口必须固定 checkout 到已验证 SHA，产物上传前再次核对来源。没有完整证据不得进入打包。

这些自动化不能代替管理员保护：有写权限的人在未保护分支仍可能修改工作流或另开发布入口。
仓库管理员必须独立启用分支规则、限制发布权限，并定期审查工作流来源和配置。

## 来源和依赖

CI artifact 应保存 source SHA、Go 版本与平台、原始测试日志、逐项 skip、构建/vet、前端 audit
和原生 sidecar SHA-256。GitHub artifact digest 是归档完整性证据，sidecar 文件 hash 是二进制
完整性证据，两者用途不同。正式安装包还需发布清单、发布 SHA 和构建 run 链接。

签名密钥不进入仓库；未签名提交或附件不得描述为已签名。生产发布还需维护者配置受托签名
tag、代码签名证书、时间戳服务、证书轮换、构建证明和留存策略。CI action、容器基础镜像、
SDK 与依赖来源应固定版本或摘要，并按补丁策略持续审核。
