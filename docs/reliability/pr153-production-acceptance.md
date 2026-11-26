# PR #153 生产验收记录（2026-11-17）

**状态：阻断上线（NO-GO）；保持 Draft，禁止合并到 preview/main。**
范围：Go 1.27.4 后端、MySQL 8.4 单库存储与迁移、Vue 前端、Tauri Windows 交付、HarmonyOS 手表、SOS 与通知工作流、风控授权、监控与恢复。基线 PR #153，修复提交 `301742c3f4dfe9d34ee3184922a763e1450ccffe`。本记录仅对**确实运行或查证**的项宣称通过。

风险分级：**P0** 上线阻断或可能导致核心功能缺失，**P1** 存在数据/安全/可靠性风险或关键证据缺失，**P2** 改善项。

| 验收项 | 状态 | 风险 | 复现方法或原始证据 | 影响范围 | 建议与处置 |
|---|---|---|---|---|---|
| Go 编译、静态分析 | 已通过（本机及 CI） | 低 | `go build ./...`、`go vet ./...`；[Go static checks](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096503409) | Go 全模块 | 保持门禁 |
| Go 与 MySQL 8.4 全量 race 回归 | **未完成**（最新 HEAD 的 CI 执行中） | **P0** | [MySQL 8.4/backend](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468/job/134084907394)：`go test -json -race -count=1 -timeout 45m ./...` | 全部持久化业务、并发一致性 | 核对完整 JSONL、跳过项、迁移恢复及失败日志。未得到成功结论禁止合并 |
| MySQL 迁移结构与检查点 | 单库迁移、旧迁移回滚与校验测试通过（MySQL 8.0），8.4 待确认 | P1 | 独立本机 MySQL 8.0.46：32 个已登记迁移；`TestCapabilitiesRollbackAndReapply` 和 `TestMySQLMigrationLineEndingsAndTampering` 均通过（18.068s）；8.4 CI 见上 | 新装、旧库升级 | 在 MySQL 8.4 强制重跑 DDL 中断/恢复、schema checksum 与重复执行测试 |
| 历史 MySQL 数据保留 | 代码已验证不主动清除旧 outbox，**真实历史数据未演练** | **P0** | 0033 仅停用 15 个 PostgreSQL 投影触发器；`analytics_outbox`、`analytics_source` 表继续存在 | 统计历史、事件审计、设备健康 | 使用脱敏的真实旧版快照做行数、主键、索引、外键及历史报告对账 |
| 图片原子性、重复写入、污染 | **已通过**（隔离 MySQL 8.0.46） | 低 | `TestMySQLImageStorageAndLegacyImport`；对已有目录写入相同键必须返回 `ErrExists` 且不得留下新密文；引入统一数据库事务 | 头像、社区图片及上传接口 | 新增事务回滚回归；CI 8.4 继续校验 |
| 图片密文、无引用对象及校验 | **已通过**（隔离 MySQL 8.0.46） | 低 | `VerifyReadyCatalog` 检查所有 `ready` 目录条目；篡改密文后应得到 `ErrIntegrity`；定制 `kai.jpg` 不可漏迁 | 图片完整性、恢复与导出 | 已补齐测试与启动迁移数量校验；需要真实图片库复核 |
| 旧图片迁移及幂等重放 | **已通过**（合成样例） | P1（真实数据缺失） | 独立 `pr153_release_canary`：dry-run inspected=1、首次 imported=1、重放 imported=0、missing_references=0 | 旧本地目录/S3 导出 | 真实数据迁移需离线备份、保持旧密钥，完整哈希核验后方可切换 |
| 完整备份与恢复 | **合成恢复通过，生产数据未验证** | **P0** | MySQL 8.0.46：`mysqldump --single-transaction --routines --triggers --hex-blob` → 新库 `pr153_restore_canary`；密文 SHA-256 前后均为 `b6da801fa61c5ab0deccfedeead2b00457fec365833d3dc889cb4d1fec89daec`，再次 `--apply` imported=0 | 灾备、升级失败回滚 | 在目标 MySQL 8.4 使用真实数据、密钥、并发负载与断电场景验证完整恢复，不把合成样例等同于正式验收 |
| 数据密钥轮换、历史投影 | **已通过**（隔离 MySQL 8.0.46 修订用例） | P1（目标 8.4 待确认） | `go test ./internal/rekey -run '^TestRekeyMySQL$'`；旧测试依赖已退役触发器，已改为显式播种历史投影；核对 HMAC 索引、错密钥阻断写入和重放 | 密钥轮换、手机号索引、历史坐标 | CI 8.4 + 实际旧密钥集验证，严禁直接销毁旧密钥 |
| SOS 告警、照护、签名、坐标接口 | **通过指定真实数据库回归，全部功能路径待全量 CI** | P1 | `TestIntegrationCoordinatesAndSOS`：缺失坐标可 SOS、数值经纬度经加密存储并认证解密、错误经纬度 HTTP 400；[协议/重放 CI](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468) | 紧急告警、定位、设备端协议 | 全量并发、超时、Webhook 实际送达及人工回执须补真实闭环 |
| 登录、会话、RBAC、2FA、并发撤权 | 部分单元检查与 CodeQL 通过，**生产授权竞争测试未全部确认** | **P1** | 后端对应测试在 MySQL 8.4 CI；[CodeQL Go/JS](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505048) | 管理端、监护人、护理岗位及健康位置数据 | 逐角色越权、事务内撤权与重放、会话过期/CSRF/限流需要保留单独门禁结果 |
| Vue/Web UI 构建与交互 | CI 通过，真实多浏览器长期运行未验收 | P1 | [Frontend、UI Quality](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468) | 管理端全部页面 | 真浏览器完整 E2E：登录、表单、图片、告警、断网重连、长时间值守 |
| HarmonyOS 协议与模拟器 | **模拟器和协议通过，SDK/真机缺失** | **P0** | [Watch simulator 与协议测试](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468)；`HarmonyOS SDK compile (no hardware)` 明确 skipped | 手表上报、离线队列、告警与恢复 | 真 SDK 编译、签名、设备安装、丢包/弱网/SOS 端到端验收 |
| Windows Tauri 原生安装/卸载 | 仅 sidecar 协议检查通过，原生验收跳过 | **P0** | [Windows sidecar job](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468)；`Tauri build and installation` 明确 skipped | Windows 正式桌面版 | Windows 原生打包、安装卸载、升级、WebView2、密钥存取与重启恢复 |
| 发布、Compose、Nginx、Prometheus | 静态部署契约通过，真实容器生产启动未验收 | **P1** | [Deployment and alerts CI](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468)，验证 compose/Nginx/promtool，独立 Windows Docker 引擎不可用 | 部署环境、监控、告警 | 生产等价环境进行从零启动、依赖故障、故障告警和滚动/回滚演练 |
| 安全、依赖审查、数据暴露 | 静态 CodeQL/依赖扫描通过，渗透/运行时审计不完整 | P1 | [CodeQL 及依赖门禁](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468)；仓库默认分支仍报告两个中危 Dependabot 提醒（未核定与本 PR 的关联） | 所有外部 API、密钥、设备与用户隐私 | 核查实际依赖路径、最小权限、密钥注入、上传滥用、注入与风控 |
| 容量、吞吐、P95/P99 延迟 | **未完成容量基准** | P1 | 现有 CI 为功能/构建门禁，不提供与上版可比的生产流量分布、磁盘 I/O 与故障注入曲线 | MySQL 大 BLOB、备份速度、SOS 尾延迟 | 压测混合上报+SOS+图片上传，测查询计划、CPU/IO、磁盘膨胀、P95/P99、重启恢复；禁止编造性能提升百分比 |
| 实际通知网关及人工闭环 | 模拟/测试通过，外部真实网关与回执缺失 | **P0** | [Watch/后端闭环模拟](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44096505468)，没有真实发送方到接收人的签收证明 | SOS 救援、护理值守 | 验证真实提供商超时、重试、幂等、漏通知、回执与人工升级 |

## 已修正缺陷（当前 PR #153）

1. **数据一致性 / P1**：图片先独立写 MySQL 密文、再写目录，两次事务之间崩溃可产生孤立密文。现在同一 MySQL 事务提交两项数据，目录冲突回滚不留下密文，添加单连接池引用锁回归。
2. **迁移完整性 / P1**：已登记但未引用图片、定制 `kai.jpg` 有漏迁风险；补齐数量校验及全 `ready` 目录哈希/密文认证。密文篡改会导致 `ErrIntegrity`。
3. **诊断准确性 / P2**：导入失败使用 `%d` 格式化 `*int64`；已修正。
4. **旧逻辑测试失效 / P1**：MySQL-only 停用 PostgreSQL 投影触发器后，重加密测试需显式建立历史 outbox 数据；旧测试按明文匹配 SOS 经纬度，而生产已加密。已调整用例核对真实数据模式。原始失败：[首次 MySQL CI](https://github.com/WatchTestCs/smart-elderly-watch-management/actions/runs/44089757679)。

## 验收证据边界

- 本机一次性 **MySQL 8.0.46**（端口 13369）属于真实数据库，但**不是**目标 8.4。来源为专用测试目录，未连接线上或既有数据库。
- 本机 `go test -json -count=1 -timeout 15m ./...` 曾在较早工作树上触发 900 秒 timeout，不能当作全量通过。初期测试 DSN 所引用的 `yihe_test` 尚未初始化，两个数据库测试报“MySQL unavailable”，属于测试环境准备不足，已经创建专用测试库。
- 最新提交 `301742c` 的 GitHub Go 1.27.4 + MySQL 8.4 全量测试尚无终态结果。在获得该次**确切 SHA** 的成功证据前，不升级验收状态。
- 生产历史库、SDK 真机、Windows 原生安装、真实供应商网关、生产性能曲线当前没有完成证据，因此总体结论仍为 **NO-GO**。

## 生产放行最小门槛

1. PR #153 精确 HEAD 的全部**必需 CI** 成功、无取消/跳过的关键数据库用例，实际 MySQL 8.4 上完整通过 `go test -race -count=1 -timeout 45m ./...`。
2. 完整旧库与密钥/图片对象备份，在**隔离副本**执行升级与恢复；迁移前后行数、密文哈希、关键索引、外键、触发器、审计历史一致。
3. Windows Tauri 原生安装、启动、卸载、重装及 WebView2；HarmonyOS SDK 原生编译、真机、离线重传和 SOS 实际闭环均验收。
4. 告警处理、外部通知/回执、RBAC 并发撤权、MySQL 故障恢复和 P95/P99 基线达到项目明确的 SLO，再由维护者审查并合并。

## 2026-11-17 continued verification on PR #153 (exact-source addendum)

**Decision: NO-GO pending exact-head CI and physical client acceptance.** This addendum distinguishes code changes from proven deployment outcomes. Source before this addendum: `2efd51143c28bb41ef1f13ab154f15a946f8601d`.

| Requirement | Result and raw evidence | Residual risk |
| --- | --- | --- |
| Real MySQL 8.4 deployment | Isolated Docker project `yihepr153qa`, app, mysql:8.4, Nginx and Prometheus all healthy; HTTP `127.0.0.1:18089/api/v1/service/status` returned `databaseReady:true` and `primaryEngine:mysql`. MySQL queried: 32 schema migrations, image_payload exists, zero retired projection triggers | Synthetic data only; no production-sized import or signed release |
| MySQL 8.4 dependency failure | Stop dedicated MySQL container: readiness false, business HTTP 503, Retry-After 3; restart: automatic MySQL recovery without restarting Go API | Crash consistency under sustained production writes/replicas remains untested |
| Native HarmonyOS compilation | SDK 26.0.0.105, Hvigor 6.26.9: default/debug `E5F69FE923FA75F299933EBF33A7FBCD31B56F97192E10BC599311E74AB4158A`, default/release `97DB2AC2FC8E7504EDE4E45DD2906B22E22DFBD6BE11BB1279A06FB4FF98E79C`, quality/release `F0BAD3BAB1573C7AE42D30CA8CC5492B3B0FD9DB9B10BC00EE3AC4477F125512`; `ohosTest` debug build `940AA69FC7434E2B78EA50DB4DE590C793CB90455AA59D398A2A54B5F5AA487C` | All HAPs unsigned, HDC returned no targets, no device installation or sensor/SOS E2E |
| Windows desktop native build | GitHub `44101522360`, actual Tauri build and package job succeeded, installer, Portable and source artifacts uploaded; local Windows `cargo check --locked` blocked by absent MSVC `link.exe` | Installer execution, upgrade/uninstall and WebView2 lifecycle remain unverified |
| Go 1.27.4 + MySQL 8.4 race suite | CI `44103294247`: 1,435 passes, six documented optional A/B/performance skips; no test-case failure, **gate rejected two obsolete S3/PG test names**. Replaced these names with meaningful MySQL image two-pool/restore + legacy import checks; historical rekey `TestRekeyMySQL` already mandatory. | New exact HEAD CI must pass the replacement test and evidence auditor; optional soak/P95/P99 not asserted |
| UI cancellation | Linux UI Quality `44103294247` failed on Playwright `requestfailed` timeout at `frontend/qa/lifecycle-acceptance.cjs:23`. Local Chromium passed all 15 checks. Replaced interception-dependent event with in-page observation of real `XMLHttpRequest.abort()`, matched by request URL and body, retaining five positive cancellation assertions. | Linux CI must rerun on exact HEAD |
| Mixed statistics performance | Local Ryzen AI 9 H 365, 10,000 health records: disk route 41.86 ms/op; hot-cache route 0.271 ms/op (single-machine microbenchmark, not P95/P99 or load-test SLA) | Need real traffic mix and explicit SLO |
| Risk audit recovery file | Readonly production container requires bounded spool on ephemeral `/tmp`; deployment config and contract updated in `2efd511` | Abrupt container termination during database outage can lose uncommitted audit events |

**Operational scope:** Tests and destructive fault injections targeted only independent temporary MySQL databases/containers; existing user business database was not changed. Full release acceptance still requires production-equivalent sanitized data restore, external notification delivery receipt, native device installation and validated final CI source SHA.
