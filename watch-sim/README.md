# 手表业务模拟器

Rust 单设备模拟器支持本地模拟和通过现有 HTTP 接口连接 preview。默认发送完整 `report`，现有心率、血压、步数、睡眠、位置和告警页面继续读取原有数据表。无需修改前后端接口，也无需兼容旧 Node 模拟器。

## 无网络本地模拟

发布包中双击 `本地模拟.cmd` 即可启动。数据保存在旁边的 `本地数据` 文件夹，打开终端显示的 `index.html` 查看当前指标、趋势、轨迹和事件。

也可指定保存路径：

```text
watch-sim --local "D:\手表模拟\本地数据" --scenario demo --health-samples
watch-sim --local ./local-data --once --scenario normal
```

本地模式无需网络、后端、账号或设备密钥；不会创建 HTTP 客户端或进行端口发现。每次运行生成独立的 `run-<ID>` 目录：

| 文件 | 内容 |
| --- | --- |
| `index.html` | 内嵌数据与图表的本地页面，自动刷新，无需互联网资源 |
| `latest.json` | 最新完整采样、当前场景、累计采样与事件数 |
| `records.jsonl` | 逐行保存采样；启用 `--health-samples` 时另存心率和步数样本 |
| `events.jsonl` | 模拟摔倒与求救事件 |

密钥不会写入本地文件。固定 SOS 场景可直接模拟本地事件；联机跌倒和 SOS 均要求 `--allow-sos`。本地模式也支持 `signal-loss` 场景。关闭程序后文件保留，下次运行使用新目录。

配置文件可用 `"local": "./local-data"` 指定路径；命令行 `--local` 覆盖它。保存路径相对于启动时的目录解析。

## 构建与运行

需要 Rust 1.89 及以上版本；锁定依赖见 `Cargo.lock`。Windows 使用 MSVC 工具链和 Windows SDK。

```text
cargo build --release --locked
watch-sim --device-no WATCH0001 --secret <设备密钥> --scenario normal
watch-sim --server http://127.0.0.1:8795 --bind <绑定码> --secret <设备密钥> --once
watch-sim --harmonyos --sign --health-samples --secret <设备密钥>
```

设备密钥也可通过 `WATCH_DEVICE_SECRET` 设置。命令行覆盖配置文件；配置文件中的空密钥从环境变量补充。使用 `--config watch-sim.config.json` 显式读取配置。参数详情见 `--help`。

未指定服务地址时读取桌面端 `runtime/server.json`：记录存在时核验大小、端口及两个进程，失效立即退出；记录不存在则连接 `127.0.0.1:8795`。`--server` 覆盖自动发现。

HTTPS 使用系统信任库；本地证书通过 `--ca-file <PEM>` 添加信任。请求不跟随重定向。`--sign` 为每个请求生成独立 nonce，并按后端公式签名原始 JSON 字节。

## 运行行为

登录、可选绑定和重新登录、启动心跳完成后开始采样。每 60 秒心跳同步绑定状态，解除绑定后停止上报。连接失败时登录每 10 秒重试；单次模式直接返回失败。连续模式的上报与 SOS 通信失败或业务拒绝在下一拍重试；认证失效、解除绑定及单次模式失败返回非零退出码。心跳发现绑定版本改变且仍有待确认事件时停止，避免把旧事件发给新归属。Ctrl+C 结束并打印计数。

`normal` 保持正常活动；`demo` 循环正常、心率升高、高血压、摔倒、求救、低电量及夜间睡眠。也可固定指定其中一个场景。睡眠状态按睡眠时长计算，夜间场景实际切换模型时钟。联机跌倒告警和求救均须显式启用 `--allow-sos`，固定场景在启动前校验，未授权的 `demo` 只发送普通遥测。每次进入跌倒场景将完整原始上报按 FIFO 保留到成功回执；求救调用独立 SOS 接口并保留待确认事件。重试沿用事件编号和现场坐标，每次签名使用新 nonce；成功后同一场景不重复创建事件。上报和 SOS 默认使用 HMAC 签名，`--sign` 另为登录、心跳和健康批次启用签名。联机上报携带绑定版本及 `positionSource=simulated`，时间由后端生成；本地导出保留本机时间。

`--health-samples` 在完整 report 之外另发心率和步数批次，供健康样本页面读取。样本采用 UTC 时间、UTC 日累计步数及“运行 UUID－序号－指标”记录号。批量失败会终止运行，不重复生成或自动重放记录。

此次替换采用交付包的设备会话、协议和生理模型，修正 HTTPS、错误码、配置、睡眠、绑定同步和记录号问题。交付包中的舰队、二进制入口、HTTP 流水线、假重绑及未经校验的本地面板不进入正式运行入口。后端继续使用原有 HTTP 鉴权、限流和数据处理。

## 文件与验证

| 文件 | 作用 |
| --- | --- |
| `src/main.rs` | 配置优先级、启动流程、周期采样和退出 |
| `src/local.rs`、`assets/local.html` | 本地数据保存、事件、离线页面与图表 |
| `本地模拟.cmd` | Windows 双击启动入口 |
| `src/device.rs` | 登录、绑定、心跳、上报、健康批次及 SOS |
| `src/encoding.rs` | 标准健康批次的共同字段编码复用与自定义字段回退 |
| `src/protocol.rs` | 字段验证、返回包络、签名、记录号复用及序列化 |
| `src/transport.rs` | HTTP/HTTPS、证书和响应大小限制 |
| `src/discovery.rs` | 本地桌面端口发现与进程校验 |
| `src/physio.rs`、`src/scenario.rs`、`src/rng.rs` | 连续生理数值、场景时间轴和可复现随机数 |

```text
cargo test --locked --all-targets
```

Windows 发布包需具备 Visual C++ 运行库；本项目未启用静态 CRT。模拟器产生的数据用于软件联调，真机上的传感器与授权验证另行开展。

性能基准和分轮测量记录属性能证据，未随仓库分发。
