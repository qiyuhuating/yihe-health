# main 晋升手册

`preview` 是预发布集成通道，`main` 是稳定通道。晋升的目标是让 `main` 采用已经在 `preview` 上完成完整证据验证的同一提交，而不是在晋升过程中生成新的源码提交。

## 候选冻结

1. 记录当前 `preview` 的完整 40 位 SHA。
2. 冻结该候选。候选期只接受缺陷、安全、构建或发布阻断修复；新功能、架构重构和依赖大升级进入下一候选。
3. 将同一候选 SHA 引入 `preview` 后，分支推送自动运行完整 Preview CI，启用原生手表与 Windows 桌面构建。手动复验时也必须启用这两项。
4. Security 必须在同一 SHA 上成功。
5. 分支推送同时启动 Tauri preview candidate；它等待同一 SHA 的 CI 与 Security 成功后运行 release gate，确认当前 `preview` HEAD、必需 job、artifact digest 与来源全部一致。手动复验时输入同一 SHA。

## 首次切换（已执行，保留供恢复与审计）

2026-11-12 已完成首次世代迁移：旧 `main` 提交 `ac5f2d5655fdb5caf772f80ba1f72ba999e2bd76` 保存于 `history/main-before-preview-20261112-ac5f2d5`，随后使用 expected-SHA 更新将 `main` 指向已在 `preview` 验证的 `e1e268fdb6efde2e72c9ee7c9eb7f3fc431b0423`。两条分支均保留，后续晋升使用下节的常规流程。以下步骤为这次迁移的历史操作规范，不应再次执行。

1. 再次确认候选 SHA 仍是 `preview` HEAD。
2. 保存旧 `main` 的不可变标签，并记录旧 `main` SHA。标签用于历史恢复，不继续维护旧架构。
3. 使用带 expected SHA 的 ref 更新，让 `main` 直接指向候选 SHA。不要 squash、rebase 或创建人工 merge commit。
4. 立即确认 `main` 与候选 SHA 完全一致。
5. 等待 `main` 触发的 Core CI、Security、Deployment contract 与 Desktop Contract 完成。
6. 在 `main` 上运行 Stable release candidate，输入同一 SHA。
7. 所有稳定通道证据通过后再创建正式版本标签和 Release。

## 后续常规晋升

首次切换后必须保持 `main` 是 `preview` 的祖先。每次晋升前验证：

- 候选 SHA 是当前 `preview` HEAD。
- `main...preview` 不存在 `preview` 缺失的 `main` 独立提交。
- 候选 exact-source release gate 已通过。
- 晋升只做 fast-forward ref 更新，并使用旧 `main` SHA 作为 expected SHA。
- 晋升后在 `main` 上重新完成分支敏感 CI 与 Stable release candidate。

如果 `main` 出现独立 hotfix 或历史不再满足祖先关系，停止常规晋升。先明确同步策略并恢复单向历史，再继续发布；不要用 force push 或一次性大 merge 掩盖分叉。

## 完成条件

一次晋升只有在下面条件全部成立时才算完成：

- `main` 指向计划中的候选 SHA。
- `main` 上 Core CI 与 Security 全部成功。
- Stable release candidate 对同一 SHA 验证成功。
- 正式 tag 与 Release 指向同一 SHA。
- `preview` 在下一轮开发前仍包含已发布 `main` 的历史。
