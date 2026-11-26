# 手表前台自动监测策略

自动监测由用户认证、绑定和授权后开启。`AutoMonitor` 调用与手动操作共用的 `WatchSynchronizer`，不创建另一个 Preferences 写入进程。默认立即执行一次，然后在完成后等待 60 秒；慢请求不重叠，不承诺固定墙钟时刻触发。停止设置和已验证归属保存于现有 HUKS 加密快照。

## 生命周期与优先级

`EntryAbility.onForeground/onBackground/onDestroy` 将系统可见性传给页面；页面隐藏也暂停。退到后台撤销定时器、传感器监听和 HTTP 句柄，世代编号阻止旧请求完成后再采样或清空队列。返回前台或重启只恢复已保存的开启意图；停止后不会自动重启。后台期间不补造读数，计步重新建立基线。

页面以同一个串行操作入口执行手动操作、自动周期和存储。自动工作期间仍可停止或确认 SOS；它们先取消当前同步，再排队执行，不能并发覆盖密文快照。SOS 始终先保存稳定请求 ID 再上报；若排队期间已退到后台，只保存经验证归属的求助，返回后重试原 ID。收到后台确认不代表通知送达或人员到场。

## 离线、权限和恢复

- 自动读取只检查已有授权；授权请求由用户点击“授权并读取传感器”发起。权限拒绝、设备无对应传感器、超时均不产生模拟数据。
- 联网先认证并核对绑定；网络错误或 5xx 时，仅允许与加密凭据、服务器和设备号完全一致的已验证归属缓存采样。未经认证的新设备、修改后的密钥不能离线采样。
- 网络退避从 5 秒递增至 5 分钟，周期仍按一分钟尝试本地采样；认证成功但上传持续失败也保留退避。系统时间回拨会重新认证。未处理的周期错误按 2、4、8、15 分钟退避；页面忙碌只等待 1 秒。
- 队列上限 200 条；空间不足两个指标时暂停传感器，保留全部旧记录。联网先补传，再采集。无 ACK、ACK 数量不符或写入状态不确定都不丢弃原记录 ID；存储不确定封锁后续网络及传感器操作，须重新打开恢复。
- 401/403（包括登录后上传前才撤销的情况）清除离线归属并停止自动策略，保留健康队列与 SOS。明确未绑定不沿用旧归属；核实为同一设备更高绑定纪元时清理旧采样，保留 SOS 历史。不同服务器、设备、长者或纪元不能混装批次。

## 平台边界

该功能是前台自动监测，不是常驻后台服务。目标 SDK 的普通 WATCH 应用不能借 `TASK_KEEPING` 获取无限后台权限，也不应借音频、定位或传输类型伪装健康采样。WorkScheduler 的任务间隔及运行时长受系统调度限制，Extension 还涉及独立进程；当前 Preferences 不能直接作为多个进程的并发队列。本轮不增加缺少权限或跨进程一致性验证的后台 Extension。

平台依据：[BackgroundTaskManager](https://developer.huawei.com/consumer/en/doc/harmonyos-references/js-apis-resourceschedule-backgroundtaskmanager/)、[WorkScheduler](https://developer.huawei.com/consumer/en/doc/harmonyos-guides/work-scheduler/)、[Stage 进程模型](https://developer.huawei.com/consumer/cn/doc/harmonyos-guides-V13/process-model-stage-V13/)。长期后台采样需单独证明目标设备/API 的合法调度、进程间原子存储、耗电及重启恢复，不能从本功能推导已完成。

## 验证层次

`npm test` 使用生产调度器、同步服务、存储与传感器适配类，覆盖隐藏/恢复、快速启停、单次串行、退避、断网重启、认证撤销、满队列、丢 ACK、绑定变化及授权撤销。测试时只替换 HarmonyOS SDK 边界，计时器按受控时钟推进。

`tests/backend.e2e.cjs` 在新建的独立 Go/MySQL 测试后端运行生产 `AutoMonitor`、`WatchSynchronizer` 和 `WatchApi`，由受控传感器提供两个周期，核对增量接口中的真实记录。仅此测试将间隔缩短为 1 秒；生产默认仍为 60 秒。加密边界采用 Node AES-GCM，不能当作原生 HUKS 或真实硬件证据。原生 ArkTS 编译、模拟器生命周期/UI、真实设备采样准确度分别验收。
