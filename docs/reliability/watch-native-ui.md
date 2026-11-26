# 原生界面与系统生命周期验收

`quality` 是独立的验收产品，包名固定为 `com.yihe.watch.quality`，构建同一份 Index/EntryAbility 与生产服务。默认产品仍为 `com.yihe.watch`；没有在默认产品加入测试页、远程注入或凭据接口。包名隔离保证 Preferences 与 HUKS 属于另一应用，不改原应用的凭据、队列或 SOS。

分别在独立源码导出中运行：

```powershell
./scripts/build.ps1 -CommandLineHome <官方CLI目录> -Product quality -BuildTarget default -BuildMode debug -ReportPath <产品报告>
./scripts/build.ps1 -CommandLineHome <官方CLI目录> -Product quality -BuildTarget ohosTest -BuildMode debug -ReportPath <测试报告>
```

两个目标构建均执行 clean，应使用不同导出目录保存产物。质量产品是验收包，不是默认产品的发布件。默认 release HAP 仍需单独构建，核对 DEBUG=false 及测试代码排除。

`npm run test:native-ui` 需要全新隔离 Go/MySQL 服务且启用严格设备签名。沿用原生联调的 `WATCH_TEST_URL`、`WATCH_TEST_SETUP_TOKEN`、`WATCH_NATIVE_COMMANDLINE_HOME`、`WATCH_NATIVE_BUILD_REPORT`、`WATCH_NATIVE_REPORT_DIR`、`WATCH_NATIVE_PWSH`，另设置 `WATCH_NATIVE_APP_REPORT` 为 quality 产品报告。始终要求当前 SHA、干净工作树及两份成功 quality 报告；环境变量 `WATCH_NATIVE_ALLOW_DIRTY` 不能绕过端到端验收。单独运行原生 UI 测试脚本仍可用 `-AllowDirty` 调试，但结果不可作为正式验收证据。

主机先检查 HDC 连接和新库初始化状态，创建合成账号、设备与长者。只在 quality 的实际 EntryAbility 上下文预置界面输入，再关闭该 Ability 后由页面重载；实际认证仍由页面按钮执行。API 24 的模块上下文与 UIAbility 可解析到不同 Preferences 目录，不能把应用根目录夹具当作页面已恢复。

安装器校验两份 HAP 的摘要、源码、product 和实际包名，拒绝生产包。首轮仅清理固定 `com.yihe.watch.quality` 的自有测试数据，以确保夹具可重复；第二轮 restart 不清理，验证持久化。原 `com.yihe.watch` 不卸载、不清空、不重置，主机对其原偏好文件做前后摘要核对。18799/18800 是设备回环测试端口，已占用则拒绝，结束只移除自己建立的映射。

验收由两次独立 Hypium 执行组成：

- 使用 SDK UI Driver 点击真正的设置/首页按钮，触发生产 WatchApi 认证；主机独立验证原始 HMAC、nonce 与时间后转发真实 Go。
- 开启生产一分钟调度，至少跨越完整周期验证自动运行。通过系统 AbilityDelegator 的前后台操作触发真实 EntryAbility 回调，不直接调用页面或服务可见性方法。后台跨越一分钟无请求，返回前台恢复。
- 页面停止后持久化策略，并跨越完整一分钟确认无后续周期。另将真实认证响应悬挂，切后台验证 HTTP 取消和 busy 状态恢复，再前台重新认证。
- 保留开启策略后确认 quality 进程结束：若测试框架已退出则记录该事实，否则 force-stop 自有进程。第二次测试核对不同 PID 并重新启动 EntryAbility，检查加密凭据、策略和自动运行恢复，再次停止并验证安静窗口。框架退出和强制终止分别记录，前者不冒充崩溃恢复。

本套不替换物理传感器，不自动申请或处理权限提示，不按“授权并读取传感器”。传感器缺失/未授权时必须明确显示状态，不生成假读数。这证明真实 UI/系统调度与取消边界；实际健康数据、队列和重绑的受控采样另由 `NativeBackendRunner` 的 14 阶段用例验证，硬件准确度仍不在范围内。

报告必须同时包含两次 Hypium 完整通过、主机全部阶段、真实网络与进程证据、持久化断言、原生产文件摘要不变及端口清理。任一失败或跳过均失败；SDK 编译成功、aa 返回零或只有按钮截图都不足以证明完成。截图仅通过设备 SDK 获取独立验收应用界面，不捕获 Windows 背景或真实凭据。

圆屏上的“记录”标签切换后，待上传说明可能仍在视口之外。重启用例先用真实 Scroll 的 `scrollSearch` 定位并展示“条采样待上传”，再核对文字、执行可见组件断言并截图；不以标签切换成功替代页面内容验证。主机 18 阶段到达而上述断言或第二张截图失败时，整套结果仍为失败。

主机的周期、安静窗口和取消耗时使用 `performance.now()` 单调时钟计量，保留不含凭据的请求路径与墙钟时间用于核对。设备/主机校时不能缩短或延长验收门槛；周期及三个安静窗口仍至少 60000ms。取消必须在后台操作后、原请求 10 秒网络超时前实际关闭连接，不能把自然超时当作生命周期取消。

API 24 系统实测中，旧 `http.HttpRequest.destroy()` 路径的连接在约 9965ms 后关闭，未通过超时前取消断言。生产 WatchApi 改用系统 [RCP Session.cancel/close](https://developer.huawei.com/consumer/cn/doc/harmonyos-references/remote-communication-rcp/)，每次签名后创建有界会话，保持原始 JSON/HMAC/业务 ID，禁止自动重定向及缓存。取消清理全部自有会话；不支持 RemoteCommunication 系统能力时明确拒绝发送。Node 边界回归验证取消及资源清理；是否真正中断设备连接仍必须由本套真实系统测试证明。
