# 滥用控制与独立监控

## 限流边界

进程内 32 shard，每请求只锁所属 shard；全进程默认总计 8192 key、262144 时间戳槽，配置上限
100 万 key/1600 万槽不是推荐默认值。固定端点集合、规范 IP、有限字符串长度共同约束保留内存。
每个 shard 按预算拒绝新增/扩容，不清除活跃配额；最多检查 16 项过期 entry，后台每分钟回收。
配额仍为精确滑动窗口。高基数攻击可能占满额度并拒绝新正常来源，因此需边缘控制与容量监控。

时间戳在 amd64 的 time.Time 为 24 字节，默认最多约 6 MiB；加上有界 map/window/IP/path 和
分配器开销，保留内存量级由 K、S 上限决定，为 O(K+S)，不会随新增来源无限增长。
Go runtime、在途 HTTP 请求、请求 header、其他缓存不属于这项 limiter 预算，不能把整个应用
RSS 宣称固定 6 MiB。需在反代限制请求/并发，在运行容器设置合理内存预算并监测高水位。
历史微压测记录已从当前分支清理，可从 Git 历史查阅；当前内存峰值需按部署负载重新测量。
NAT 共享、高频单 IP、伪造 header、trusted proxy、关闭生命周期与容量拒绝另有 race 回归测试。

TRUSTED_PROXY_CIDRS 最多 64 项，只有 TCP peer 在其中才解析 Forwarded/XFF，最多 4096 字节
和 32 hops，从右向左跳过可信代理。无效链回退 peer，mapped IPv4 规范化、拒绝 zone；直接
客户端伪造 XFF 无效。Nginx 覆盖 XFF 并清除 Forwarded，默认独立网段 172.30.73/24，应用仅
信任 gateway /32。网段冲突时调整 PLATFORM_SUBNET 和 GATEWAY_IPV4。默认可信代理随网关地址更新；显式设置 TRUSTED_PROXY_CIDRS 时使用该覆盖值。
不能用整个办公网/0.0.0.0/0 当可信代理，也不能让客户端绕过 ingress 直连公开后端。

Nginx shared zones 在同一 ingress 内跨 worker/应用副本统一控制登录与硬件洪水，内存区固定。
多 ingress/多宿主需要上游统一网关/WAF 的共享配额，当前进程 limiter 仍不是分布式总配额。
登录失败审计使用主库 shared 账号+IP 与账号级窗口，两实例可见，防止轮换 IP 分摊撞库；
只有登录写审计，不为每条普通请求增加共享限流 MySQL 写入。并发在途尝试仍需要边缘并发控制。

## 监控链

应用 → Prometheus → Alertmanager → 独立 webhook 接收端。Compose 的 paging profile 读取
oncall-webhook-url/oncall-token 私有文件；未配置不意味着已接入外部值班渠道。
业务启动依赖 MySQL，PG 与监控互不阻塞；/healthz 只是进程活性。

Prometheus 之外运行 `scripts/reliability/watchdog.py`，应由另一宿主/外部 supervisor 管理。
配置 WATCHDOG_STATE（持久 SQLite）、WATCHDOG_PROMETHEUS_URL、WATCHDOG_ALERTMANAGER_URL、
ONCALL_WEBHOOK_URL_FILE、ONCALL_TOKEN_FILE。生产默认 15 秒两次失败，接收端 HTTPS、禁止
redirect；loopback HTTP 仅用于本地演练。token 至少 24 字符。
失败告警保留重试，进程重启仍可续发；同故障 resolved 不越过尚未交付的 firing。测试真实
503 接收端后重启 spool 并核对顺序。接收端 2xx 只记录 receiver_accepted，不记录人工响应。

数据库离线、app 死亡、worker 心跳陈旧、queue 长期未完成、queue 采样失败、TLS 配置无效、
网关 DNS/timeout/network/HTTP 反复失败、可选 PG 故障分别有指标/规则。
本机演练使用正式规则的原始 for 时长，只将抓取/评估/分组间隔缩短为本地等待；不会手动把
任务改为失败、关闭审计、删除失败记录或以 healthz 200 代替 SOS 可用。

同机进程独立只能证明 app/Prometheus 退出时其他进程仍运行，不能证明整机停电/断网可见。
生产必须在异地实际停主机、断网络、验证接收端送达及值班人员接手，并演练备用救援流程。

## 可重复的独立进程演练

`scripts/reliability/watchdog-process-drill.py` 启动自身的真实 Prometheus、Alertmanager 和生产 watchdog.py，全部仅监听回环地址，配置、TSDB 和 SQLite spool 使用新目录。脚本不停止已有服务，不连接外部通知渠道；执行完毕只停止它创建的进程，保留证据目录。

```powershell
python scripts/reliability/watchdog-process-drill.py --prometheus '<本机 prometheus 可执行文件>' --alertmanager '<本机 alertmanager 可执行文件>' --report-dir '<私有证据目录>'
```

正式演练要求干净已提交源码；修改中可使用 `--allow-dirty`，摘要保留 dirty=true。演练先确认两个真实监控进程冷启动就绪、无误报，再终止 Prometheus，令本机受控通知接收端返回 503。它核对 pending 告警实际落盘且不能算已接收；强制停止并重启 Watchdog、重启 Prometheus后，恢复事件必须排在未送达的故障事件之后。接收端恢复才接受两条顺序事件，随后终止/重启 Alertmanager，验证另一组 firing/resolved。最终断言四个稳定事件 ID、无重复业务事件、spool 无积压以及所有自有进程已退出。

Deployment contract CI 复用现有 Prometheus 3.5.0 规则工具版本，并从官方 Prometheus/Alertmanager 0.28.1 容器提取真实 Linux 程序，记录镜像 digest、程序版本及 SHA-256，运行同一脚本并保存摘要。这里验证跨进程故障、接收端失败和 spool 重启恢复，不代表正式告警规则全链路、跨宿主故障、外部网关送达或人工接手；规则仍由独立 promtool 步骤检查。接收端 2xx 仅记 receiver_accepted。

## 心跳缓冲刷盘失败

设备在线心跳属于可合并的内存状态，和持久化 SOS、遥测队列分开处理。MySQL 写入失败时，缓冲区会重新排队并等待下次刷盘；进程最终退出且刷盘失败时，未写入的心跳可能丢失，不可将其视为持久化确认。

后端在刷盘失败时增加 `yihe_heartbeat_flush_errors_total`，定时失败记录 `heartbeat_flush_failed phase=periodic`，停机最终刷盘失败记录 `phase=shutdown`。Prometheus 在 5 分钟内出现至少 3 次失败并持续 1 分钟时触发 `YiheHeartbeatFlushRepeatedFailures`（warning）。排查 MySQL 连接和服务关闭日志；该告警不替代 SOS 业务队列告警。
