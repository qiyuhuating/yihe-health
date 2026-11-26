# 颐和守护 · HUAWEI WATCH 5 穿戴端工程

本目录是颐和配套的 HarmonyOS 原生穿戴端应用，基于 ArkTS、ArkUI 与 Stage 模型开发，目标设备为 HUAWEI WATCH 5。支持前台心率与计步传感器接入及系统权限申请；模拟采样保留在“记录”页供调试，两种来源分开保存。构建、协议、模拟器与真机证据分别核对，详见 [开发与验收边界](../docs/WATCH_INTEGRATION.md#开发与验收)。

## 主要功能

- 适配圆形表盘的深色界面，包含“首页”、“记录”、“设置”三个标签页，支持滚动访问各项操作。
- 支持设备密钥认证、通过后台生成的设备绑定码关联长者，以及发送设备心跳。
- 所有设备请求携带系统 CryptoArchitectureKit 生成的 HMAC-SHA256 签名、秒级时间戳和 128 位安全随机 nonce，可连接启用 `YIHE_REQUIRE_SIGNED_DEVICE=1` 的后端。重试生成新 nonce，仍复用原采样/SOS ID；签名能力异常时不发送请求、不降级成无签名，详见[设备签名协议](../docs/reliability/watch-device-signatures.md)。
- 首页“授权并读取传感器”申请心率、活动权限；计步累计值先建立基线，再转换成区间增量，通过 `healthBatch` 上报，不冒充每日总步数。传感器不支持或权限拒绝时明确提示。
- 认证并绑定后可开启自动监测：前台立即运行一次，随后每次完成后间隔一分钟采样并同步；离开前台取消读取/网络请求，返回后自动恢复。授权仅由手动按钮发起，自动周期不弹授权框，也不生成模拟读数。
- 离线仅沿用加密保存、与当前凭据一致的已验证归属；队列满时暂停新增采样，联网先核实绑定并补传原记录。认证撤销停止自动监测，停止按钮和确认 SOS 可取消正在运行的自动请求，全部存储操作串行执行。详见[自动监测策略](../docs/reliability/watch-auto-monitor.md)。
- 本机最多缓存 200 条待上传采样，每条采样带有固定记录 ID 供服务端幂等去重。上传前会校验服务器地址、设备号、长者 ID 与绑定版本号（`bindingVersion`）；若长者绑定关系已发生变更，客户端会拒绝补传旧批次数据。
- SOS 求助具备二次确认弹窗。在未收到明确响应前重试同一次求助会复用相同的 `requestId`，避免服务端重复建单，并在表盘上明确区分“后台已接收”与“通知已送达”状态。
- 后端地址、设备号、待上传采样、求助记录与认证成功的设备凭据以 AES-256-GCM 密文快照写入私有 `Preferences`；AES 密钥由 HUKS 管理，不导出至应用文件。重启后恢复同一设备/服务器的凭据，“忘记本机设备密钥”保留采样和求助历史。
- 首次升级先持久化旧记录的密文副本，再清除明文键；解密、密钥访问或写入失败时停止操作并保留恢复数据。不得通过清除应用数据或降级到旧版本处理存储故障，详见[安全存储设计](../docs/reliability/watch-secure-storage.md)。

## 开发环境要求

当前工程要求 Command Line Tools 26.0.0.865（Hvigor 6.26.9、SDK 26.0.0.105 / API 26）。
`toolchain.json` 固定版本与官方 Windows 归档摘要；构建前会核对 SDK、插件和工程模型版本。
6.1.1 工具链不再满足当前工程配置。下载完整归档后可用以下方式校验并解压，避免 Windows 长路径导致组件缺失：

```powershell
python ./scripts/extract-toolchain.py '<下载的 zip>' 'E:\yihe-sdk26'
./scripts/build.ps1 -CommandLineHome 'E:\yihe-sdk26\command-line-tools' -DevEcoHome ''
```

本工程已在 DevEco Studio 6.1.1.280（内置 SDK 6.1.1 API 24、兼容目标 HarmonyOS 5.1 API 18）与 Node.js 18 环境下完成编译，并在 API 24 Wearable 模拟器中完成命令行安装与启动；这些历史记录不代表当前分支完整原生验收通过。请在 DevEco Studio 中选择 `Open`，直接打开仓库根目录下的 `YiheWatch` 文件夹（切勿仅打开子目录 `entry`）。

## 构建与模拟器运行

原生构建现支持 `scripts/build.ps1 -CommandLineHome <command-line-tools根目录>`，
也支持原有 `-DevEcoHome`。脚本生成带状态、源码 SHA 和 HAP 哈希的构建报告；
CI 强制执行真实 Go/MySQL 断网恢复；独立 Windows SDK 编译入口需提供工具包地址与预期哈希，结果须查看对应运行。
详见[开发与验收说明](../docs/WATCH_INTEGRATION.md#开发与验收)。

在 PowerShell 7 中执行构建脚本：

```powershell
cd '<仓库目录>\YiheWatch'
./scripts/build.ps1
```

该脚本直接调用 DevEco Studio 自带的构建工具链（若 IDE 安装在非默认路径，可通过 `-DevEcoHome` 参数指定）。构建完成后会生成未签名的调试包 `entry/build/default/outputs/default/entry-default-unsigned.hap`；如需安装至物理手表真机，需先配置个人或企业开发者签名证书。

本机开发默认 `-BuildMode debug`；发布产品使用 `-BuildMode release`，原生 CI 也按 release 编译产品、按 debug 编译测试模块。构建报告分别记录 target 和 buildMode。调试采样按钮只在 SDK 生成的 DEBUG=true 时显示，采样操作本身也先检查该值；release 拒绝生成模拟采样，不受本机配置影响。模式依据见[官方 BuildProfile 参数说明](https://developer.huawei.com/consumer/en/doc/harmonyos-guides-V13/ide-hvigor-get-build-profile-para-guide-V13/)。

在 DevEco Studio 的 Device Manager 中启动 Wearable 模拟器后，运行以下命令安装并启动应用：

```powershell
./scripts/run-emulator.ps1 -Target '127.0.0.1:5555' -BackendPort 8796
```

启动脚本会通过 HDC 将模拟器内的 `127.0.0.1:18796` 端口转发至宿主机的 `127.0.0.1:8796`，无需将本机后端或数据库暴露到局域网。

## 与管理后台联调步骤

1. 启动 Go 后端服务（监听 `8796` 端口），访问 `http://127.0.0.1:8796/api/v1/service/status` 确认 `databaseReady` 为 `true`。
2. 在管理端网页创建一名测试长者档案，随后在“设备管理”中登记一块模拟手表，并妥善保存创建时仅展示一次的设备密钥。
3. 在手表端打开“设置”页，填入后端地址 `http://127.0.0.1:18796`、设备号以及刚才保存的设备密钥（注意区分设备密钥与后台管理员密码）。
4. 点击“保存地址并认证”，认证通过后输入后台生成的设备绑定码（注意不要误填家属注册用的邀请码）完成长者关联。
5. 真机使用可访问的 HTTPS 后台地址，首页点击“授权并读取传感器”；模拟器没有对应硬件时可在“记录”页点击“生成模拟采样（调试）”。随后点击“上传记录”，在家属首页“最近健康采样”核对来源与时间；旧版兼容的心率血压列表未必展示相同记录。
6. 完成传感器授权后点击“开启自动监测”，保持应用在前台，核对同步状态与管理端增量记录；不必每轮点击采样或上传。退到后台暂停，重新进入恢复；“停止自动监测”会保存停止设置并保留队列。模拟器无传感器时提示缺失，不以调试读数代替自动采样。
7. 点击“SOS 求助”并确认发送，管理端运营总览将生成对应的紧急照护工单。若后端已挂载真实通知网关，此操作会触发外部通知，请仅在隔离测试环境中执行。

## 协议测试

```powershell
npm ci
npm test
```

单元测试基于 `package-lock.json` 固定的 TypeScript 5.9.3 转译生产模型及服务，验证调度串行/暂停恢复/退避、绑定隔离、重试幂等、计步转换、权限边界、凭据与队列加密恢复。真实 Go/MySQL E2E 还运行生产调度器与同步服务的两个自动周期，传感器在 SDK 边界提供受控数据。HUKS 边界适配器使用真实 Node AES-GCM；这些适配器不等价于 HarmonyOS HUKS 运行结果、ArkTS 编译或传感器硬件测试。

如需针对真实后端运行端到端协议联调脚本 `tests/backend.e2e.cjs`，请先准备全新空库的独立测试后端实例，为该实例设置 `YIHE_REQUIRE_SIGNED_DEVICE=1`，并设置测试进程的环境变量 `WATCH_TEST_URL`（仅限本机 HTTP 地址）与 `WATCH_TEST_SETUP_TOKEN`。脚本会先验证无签名请求被拒绝，未开启严格模式不能通过。详细的集成验收记录见 [手表端同分支整合说明](../docs/WATCH_INTEGRATION.md)。

## 原生系统测试

实际界面和系统生命周期使用独立 `quality` 产品（`com.yihe.watch.quality`），运行同一份页面代码并隔离原应用的数据；`npm run test:native-ui` 的环境、双包构建及验收边界见 [原生界面验收](../docs/reliability/watch-native-ui.md)。普通协议测试、SDK 编译和该界面用例分别记录，不能相互替代。

独立 `ohosTest` 目标使用官方 Hypium 1.0.25（版本与完整性摘要已锁定）。它调用实际系统 HMAC/HUKS，并在单独名称的 Preferences 中验证 200 条采样和凭据/SOS 重载，不用已有生产数据当测试夹具。测试模块没有启动器入口，不打进默认产品 HAP。

```powershell
./scripts/build.ps1 -CommandLineHome 'E:\yihe-sdk26\command-line-tools' -DevEcoHome '' -BuildTarget ohosTest -ReportPath './artifacts/native-test-build.json'
./scripts/run-native-tests.ps1 -CommandLineHome 'E:\yihe-sdk26\command-line-tools' -BuildReport './artifacts/native-test-build.json' -Target '127.0.0.1:5555'
```

正式运行要求源码干净、HAP 摘要与报告一致、指定目标已连接；无目标时不会新建/重置模拟器。三个必需用例必须全部通过且无 skip，主机还会独立验证原生签名结果。调试可显式使用 `-AllowDirty`，报告仍保留 dirty 标记，不作为正式源码交付证据。目标切换的 `clean` 可能清除另一个目标产物，请分别保存产品及测试包。详见[原生系统验收](../docs/reliability/watch-native-tests.md)。

真实原生网络与 Go/MySQL 联调使用 `npm run test:native-backend`，需独立空库、严格设备签名、已连接模拟器及私有证据目录。配置变量、受控传感器和生命周期验证边界见上述验收说明；这套运行结果与三个系统密码/存储用例分别记录，不以编译结果替代。

## 当前已知限制

- 自动监测限应用前台，不提供息屏/后台常驻采样保证，也不补造后台期间的读数。普通手表应用不能滥用音频、定位或任务保持权限绕过系统限制；后台方案需另行验证合法 API、跨进程存储和恢复边界。
- 尚未完成新增传感器代码的真表验收；没有 GPS、睡眠阶段检测或持续后台采集。HUKS 与安全随机数不可用时阻止敏感存储，不回退明文；目标设备兼容性须实际验证。
- 重启后可恢复自动监测意图和已验证归属；联网时重新认证并校验绑定版本。离线只能为此前认证的同一服务器/设备缓存，不得为新设备或编辑过的密钥编造归属；明确解绑/撤销后不继续离线采样。

- 后台接收 SOS 不代表外部通知已送达或人员已到场，仍需人工跟进与独立复核。
- Node 协议测试转译生产模型及服务，不能代替 DevEco 原生编译、模拟器交互或真机验证。WATCH 5 真机安装、签名、传感器权限与圆屏视觉验收仍未完成。
