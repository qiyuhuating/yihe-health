# HarmonyOS 设备请求签名

`WatchApi` 的登录、绑定、心跳、健康批次及 SOS 全部调用 `DeviceSigner`。实现对齐现有 Go `telemetry_signed.go` 和 Rust 模拟器，不改变后端合同或关闭防重放。后端设置 `YIHE_REQUIRE_SIGNED_DEVICE=1` 后拒绝无签名请求；兼容模式下也会验证已提供的签名。

## 字节与密钥

每次发送前只对请求体执行一次 `JSON.stringify`。发送同一串文本的 UTF-8 字节，并计算：

```text
digest    = lowercase_hex(SHA256(raw_request_bytes))
message   = uppercase(trim(deviceNo)) + ":" + timestamp + ":" + nonce + ":" + digest
signature = lowercase_hex(HMAC_SHA256(UTF8(deviceSecret), UTF8(message)))
```

请求头为 `X-Device-Timestamp`、`X-Device-Nonce`、`X-Device-Signature`。时间戳为 Unix 秒；nonce 为系统安全随机源生成的 16 字节（32 位十六进制）。后端时间窗为前后 5 分钟，使用共享数据库唯一键防重放。该既有合同未将路径/方法纳入 MAC，不能据此宣称端点域分离；若要扩展必须同步版本化两端协议。

HarmonyOS 采用 CryptoArchitectureKit 的 SHA256 摘要和 MAC、`createSymKeyGenerator('HMAC')` 导入原设备密钥。不能使用要求固定 32 字节的 `HMAC|SHA256` 导入器截断或改写合法的变长密钥。临时密钥对象用后调用 `clearMem`，可控字节缓冲清零；既有 ArkTS 字符串生命周期不因此保证立即擦除。设备凭据持久化仍由 HUKS 快照负责，网络签名不导出 HUKS AES 密钥。

签名或安全随机能力异常、输入超限、时间无效时，HTTP 请求尚未创建，队列保留，不降级无签名。签名期间停止/退后台后，即使原生异步操作稍后结束，也不会再创建 HTTP。签名不替代 HTTPS 和后台对设备密钥/绑定纪元的校验。

## 重试与业务幂等

WatchApi 每次主动调用生成新的 nonce，包括后台已提交但 ACK 丢失后的人工或自动重试。健康样本的 `recordId`、原绑定版本和未确认 SOS 的 `requestId` 保持不变，业务幂等仍由后端负责。WatchApi 不自行重发同一签名，但原生 HTTP 栈可能在空连接断开后透明重发；真实后端必须拒绝重复 nonce，客户端保留未确认记录，不将该拒绝当作业务 ACK。原生联调的受控丢 ACK 与重放核验见 [原生测试说明](watch-native-tests.md)。401/403 停止自动离线归属，时间窗错误需检查设备时间后重新认证，不放宽服务器校验。

## 验证

Node 测试执行生产签名类，只在系统加密边界使用真实 Node SHA256/HMAC/安全随机数；覆盖 UTF-8、大请求分段、变长密钥、随机数新鲜度、缓冲清理、能力失败及取消时序。这不等于原生系统密码服务的运行验证。

真实 Go/MySQL E2E 必须使用全新专用数据库并启用 `YIHE_REQUIRE_SIGNED_DEVICE=1`：先证明无签名、过期、篡改及 nonce 重放被拒绝，再通过生产 WatchApi 执行登录、绑定、心跳、上传与 SOS。丢 ACK 后新 nonce 携带原记录 ID，被数据库去重；自动受控传感器周期同样经过严格签名。CI 固定开启这一开关，未开启不能通过测试。原生 SDK 编译与目标系统运行分别记录，不能用 Node 适配器冒充真实设备证据。
