# Fork PR 的 CodeQL 核验

## 基线与门禁

2026-11-21，源码 `7d7723b95a17e7ebef76a29be7550a49a0881d25` 的个人 fork
Security push run `44867336464` 实际完成 Go 与 JavaScript/TypeScript 的 security-extended
分析。下载的两个 SARIF 制品分别为 `14692657046` / `14692557205`，ZIP SHA-256：

- Go：`dfd9e51fefcafb611006cc6ca3c8fc6407bbba869da70647d6b6ef7572e30b1d`
- JS/TS：`f9c2797bd9c3f079e6ba375e25c470f018d5cf64e4f1b5ca30050f3e3a68c045`

该运行成功，但原始报告含 6 个结果，不能称为零告警。上游同源码 PR 的 Security
因 fork 条件跳过，不能用个人 fork 的运行替代上游必需检查。
本次移除该跳过条件，保留普通 `pull_request` 事件、固定 head SHA、不持久化检出凭据，
增加实际 HEAD 核验和 `codeql-source.txt`。错 SHA / 非法 SHA 必须在分析前失败。
事件和发布边界见 [发布保护](release-governance.md)。

## 六条结果的处理

下表位置均指上述基线，不冒充新 head 的扫描结果；新扫描及独立复核仍须核对。

| 规则 / 基线位置 | 判断及处理 |
| --- | --- |
| `js/polynomial-redos`，`frontend/vendor/secured-picomatch/lib/utils.js:31` | 确认未闭合方括号导致重复扫描。250,000 个 `[` 的原实现超过两秒期限；改用线性去转义，保留字符类和换行语义。受限子进程回归进入前端必需单测。 |
| `js/bad-tag-filter`，`frontend/scripts/check-module-boundaries.mjs:43` | 正则不能覆盖 Vue 接受的结束标签空白，可能遗漏边界检查；改为既有 `vue/compiler-sfc` 的真实解析结果。 |
| `js/bad-tag-filter`，`frontend/scripts/check-vue36-readiness.ts:59` | 同类语法遗漏；与模块门禁共用官方解析器，检查普通/setup 两个脚本块，拒绝损坏或外部脚本。注释不当作脚本。 |
| `go/log-injection`，`backend/internal/observability/metrics.go:189` | 结构化 `slog.Info` 的属性值，不是拼接日志消息；标准 JSON/Text handler 会引用控制字符。新增实际 HTTP 中间件日志回归，验证 CR/LF/引号只能出现在一个记录内，查询内容不进入日志。若部署替换自定义 handler，仍须单独复核其编码行为。 |
| `js/insufficient-password-hash`，`YiheWatch/tests/backend.e2e.cjs:42` | 测试参考签名的请求体 SHA-256，随后用设备 secret 做 HMAC；不是账号密码落库或密码验证。调用只对含设备编号/secret 的设备请求签名，账号 setup/login 不走这条签名路径。保留与产品协议一致的摘要算法，不以替换成密码 KDF 消警告。 |
| `js/user-controlled-bypass`，`YiheWatch/tests/native-ui.e2e.cjs:124` | 只在显式独立测试中启动的本机控制路由；所有分派之前检查 POST 和随机 Bearer token，capture 分支另有 quality bundle、设备文件路径、标签白名单及重复检查。后续阶段由主机状态决定，客户端只能确认预期阶段；URL 分派不授予未认证访问。保留这些约束和真实 UI 验收，须独立审阅该测试边界。 |

末三项是基于调用语义的审查判断，没有在 GitHub 自动 dismiss，没有配置查询排除或降低
扫描级别。不能以本表宣称远端告警已清零、独立安全评审已完成或正式发布获准。

## 验证范围

前端变更使用既有 Vue 编译器，无新增依赖；vendor 变更已在其 README 标明，不再声称
所有 JS 与上游逐字一致。差分探针用旧函数对 399,593 个穷举/固定随机输入比较，0 差异；
长输入另由有超时的子进程检查，避免把测试 runner 永久阻塞。
完整前端安装、类型检查、单测、构建和依赖审计的结果应绑定实际验证的 Git tree/提交。
原生 `7d7723b` 两次 Hypium/18 阶段的证据仍保持原 SHA，不改写成后续工作流/测试提交的全套重跑。
