# HarmonyOS 手表接入

手表工程位于 [YiheWatch](../YiheWatch/README.md)，网页在设备管理中提供登记、绑定码和接入引导，Go 后端负责设备认证、归属校验、健康采样入库和 SOS 工单。

## 通信流程

1. 管理员先创建长者档案，再登记设备，保存创建时展示的设备密钥。
2. 手表配置可访问的 HTTPS 后端地址、设备号和密钥，调用 `device/login`，携带 `platform=harmonyos`、`protocolVersion=1`。所有设备请求使用现有后端 HMAC 签名合同，签名、时间戳及安全随机 nonce 见[签名说明](reliability/watch-device-signatures.md)。
3. 后端返回协议版本、批次上限、长者归属和 `bindingVersion`。输入设备绑定码调用 `device/bind` 后重新认证。
4. 手表申请心率和活动权限；心率按瞬时样本保存，累计计步先建立基线，再转换为区间增量。调试采样单独标记 `simulated`。
5. `device/healthBatch` 携带原绑定版本和稳定采样 ID。重试复用记录 ID；未经核实的绑定变化保留旧队列并阻止补传，认证确认同一设备的新纪元后清除过期采样，SOS 历史保留待核查。
6. SOS 使用稳定 `requestId` 和原绑定版本。后台接收、通知回执、人工处置分别记录。

全部接口的响应与错误格式见 [接口合同](API_CONTRACTS.md)。设备密钥错误返回 401，设备停用返回 403，绑定变化返回 409；网页与手表展示后端消息和追踪编号。

## 开发与验收

- [构建、模拟器端口转发和联调命令](../YiheWatch/README.md)

当前实现包含前台原生传感器读取、每分钟自动采样/补传与独立的调试模拟采样。自动监测在系统 UIAbility 退到后台时取消工作，回到前台后恢复；离线缓存必须具有已验证的凭据和绑定，自动周期不弹权限框。调度策略、受控适配测试与后台边界见[自动监测设计](reliability/watch-auto-monitor.md)。原生 SDK 编译、签名安装和实表传感器验收需要对应工具及设备。历史构建记录可从 Git 历史查阅。

手表认证成功的设备凭据、自动监测意图、已验证归属与采样/SOS 队列通过 HUKS AES-256-GCM 加密为一个 Preferences 快照；密钥不导出，凭据按服务器/设备隔离，异常时拒绝明文回退。迁移与恢复边界见[安全存储设计](reliability/watch-secure-storage.md)。持续后台采样、GPS 与睡眠阶段检测仍未实现。
