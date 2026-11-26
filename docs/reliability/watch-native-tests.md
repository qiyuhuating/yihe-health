# 原生系统测试与证据

`YiheWatch/entry/src/ohosTest` 使用官方 Hypium 1.0.25 和 TestKit；版本与 SHA-512 固定在 oh-package-lock.json5。默认产品目标不导入测试代码。测试是独立 entry_test feature 模块，没有启动器 skill 或生产调试路由。构建前运行官方 ohpm，锁文件内容漂移即拒绝，Windows 行尾差异不算依赖变化。

## 执行与核验

先按照穿戴端 README 构建 `-BuildTarget ohosTest`，再运行 `scripts/run-native-tests.ps1`。运行脚本核对构建成功、源码 dirty 状态、HAP SHA-256、bundle/module/type 和 HDC 目标后安装测试模块；不卸载、重置或清空已有应用。使用 SDK 的 `aa test` 运行框架，日志必须显示三个指定用例全部通过、0 failure/error/ignore。仅 CLI 返回 0 不够：实际失败的 Hypium 测试也可能让 aa 显示 TestFinished-ResultCode: 0。

- native_signing：生产 DeviceSigner 调用真实 CryptoArchitectureKit SHA256、变长 HMAC 密钥及安全随机源。两次签名的 nonce 不同；只输出公开固定测试输入的签名证明，由主机 .NET HMAC 独立校验原始 UTF-8 大正文，避免只检查签名长度。
- native_vault_authentication：生产 WatchVault 调用真实 HUKS，多段 UTF-8 加密解密、篡改密文拒绝，失败后原密文仍可解密。不删除、导出或替换已有 HUKS AES 密钥。
- native_store_disk_reload：生产 WatchStore 以独立名称 yihe_watch_native_test_v1 保存 200 条合成采样、公开测试凭据、已验证归属、策略和 SOS，移除 Preferences 缓存后从系统持久化文件重载。移除凭据后采样与 SOS 仍保留。这是磁盘重载，不是操作系统重启。

## 原生网络与真实后端联调

`NativeBackendRunner` 是独立的 Hypium 入口，使用生产 AutoMonitor、WatchSynchronizer、WatchApi、系统 HMAC/HTTP、HUKS 和 Preferences；仅物理传感器边界提供受控读数。测试按顺序核对自动周期、隐藏暂停与恢复、断网队列及磁盘重载、丢 ACK 后原 ID 重试、重绑后的归属隔离和 SOS 幂等。测试间隔缩短为 1 秒，生产仍为 60 秒；重绑测试等待生产网络退避到期。这里主动调用服务的可见性方法，不能代替 ArkUI/系统真实前后台事件验收。

先构建 ohosTest，再为测试进程提供下列环境变量，执行 `npm run test:native-backend`：

| 变量 | 要求 |
| --- | --- |
| WATCH_TEST_URL | 全新空库的独立 Go/MySQL 后端，地址仅允许 `http://127.0.0.1:<端口>` |
| WATCH_TEST_SETUP_TOKEN | 该测试实例的桌面初始化令牌；实例必须开启 `YIHE_REQUIRE_SIGNED_DEVICE=1` |
| WATCH_NATIVE_COMMANDLINE_HOME | 已校验的官方 CLI 根目录 |
| WATCH_NATIVE_BUILD_REPORT | 当前 SHA 的 ohosTest 成功构建报告 |
| WATCH_NATIVE_REPORT_DIR | 本机私有证据目录 |
| WATCH_NATIVE_PWSH | PowerShell 7 可执行文件完整路径 |
| WATCH_NATIVE_TARGET | 已连接的 HDC 目标，默认 `127.0.0.1:5555` |

脚本在写测试数据库前检查目标连接，拒绝已有业务库；生成独立测试账号、设备、两名长者和随机 Preferences 名称，不配置真实通知接收人。主机独立校验原始请求的 HMAC、时间与 nonce，实际转发到 Go，按 14 个阶段查询健康增量和 SOS 响应。503 与提交后断开 ACK 由本机代理注入；后者必须通过真实后端去重。测试占用模拟器回环端口 18797/18798，已有映射时拒绝，结束只清理自己创建的映射。

`aa test -s fixtureToken <值>` 在系统参数表中使用 `-s fixtureToken` 键，测试按该键读取并严格校验一次性令牌，不输出令牌值。映射删除按 HDC 规定拆分两个端点参数；即使联调失败也尝试清理全部自有映射、记录失败摘要，并保留首个测试错误。命令结构依据 [OpenHarmony aa 实现](https://github.com/openharmony/ability_ability_runtime/blob/master/tools/aa/src/ability_command.cpp) 和 [HDC 文档](https://github.com/openharmony/docs/blob/master/zh-cn/application-dev/dfx/hdc.md)。

原生 HTTP 栈在空连接断开后可能透明重发同一个 POST。丢 ACK 故障窗口持续到手表确认本地队列仍保留：窗口内仅允许相同正文、时间和 nonce 的那一次底层重发，并实际转发至 Go 验证 `409 NONCE_REPLAY`，再继续丢弃响应。窗口外重放立即失败；后续应用主动补传必须换 nonce、保留原 recordId，并由真实主库返回业务去重。摘要单列底层重发拒绝数，不将它算成新签名请求或成功写入。

正式端到端验收始终要求源码与构建报告干净，不接受 `WATCH_NATIVE_ALLOW_DIRTY` 绕过；单独运行原生测试脚本时可显式使用 `-AllowDirty` 调试，但 dirty 结果不能作为正式验收证据。单独执行 `run-native-tests.ps1 -Suite backend` 的 Hypium 成功日志不足以证明联调通过，还必须有主机全部阶段、独立签名校验和真实数据断言的成功摘要。未实际执行不得把编译成功记为本套运行成功。

## 存储隔离

实际 Index/EntryAbility 与系统前后台、进程恢复另有独立 [quality 界面验收](watch-native-ui.md)。它安装不同包名的同源产品，使用真实一分钟调度和 SDK UI Driver，不将本节主动调用服务可见性方法的结果当作 ArkUI 生命周期通过。

API 24 实测 createModuleContext 不保证独立 Preferences 路径。初版测试因此触发隔离断言，在打开存储前停止；不能移除断言后直接使用生产文件。现通过显式 Preferences 名称隔离，生产默认名称 yihe_watch_sim_v1 和调用方式保持原值；名称仅允许 1–64 个字母、数字、下划线或连字符。测试不得将真实数据写入测试名称，也不得清除生产文件以使测试通过。

专用模拟器验收可在本机私有证据目录读取测试前后的生产文件并仅保存摘要，确认测试没有写入它；不得将业务 XML、凭据、私钥或屏幕背后的用户内容上传至 PR。

## 证据范围

这三项测试证明系统密码服务和实际 Preferences 持久化边界，不依赖 Node SDK 替身。它们不等于完整原生网络/自动采样端到端、ArkUI 按钮生命周期或真实硬件准确度验收。真实 Go/MySQL 严格签名联调、Node 调度回归、默认 HAP 编译和当前系统测试分别记录源码 SHA 与产物摘要。Windows 原生 CI 在 SDK 配置有效时构建产品和测试包；没有模拟器镜像时只声明编译，不声明原生运行。

平台入口：[官方单元测试指南](https://developer.huawei.com/consumer/cn/doc/harmonyos-guides/unittest-guidelines/)、[Ability Assistant](https://developer.huawei.com/consumer/en/doc/harmonyos-guides/aa-tool/)。
