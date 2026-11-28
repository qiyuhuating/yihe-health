# 遥测工作器资源占用

在一次空闲的持久化队列 drain 后，仅探测是否存在到期 pending 记录。
使用现有 (status,next_retry_at,id) 索引，不增加表或迁移。
探测只是提示；原有事务仍负责 worker guard、记录锁、最早待处理记录、
保存点、失败重试与提交。较晚到期的记录不得越过较早待重试记录。
250ms 轮询间隔、已提交数据恢复、ACK 和跨进程领取机制保持不变。
存在积压时直接走原 drain 路径。

隔离 MySQL 8.4.13 / Go 1.27.4 / Linux amd64 / Xeon Platinum 8259CL，
innodb_flush_log_at_trx_commit=1，sync_binlog=1，GOMAXPROCS=2。
对空队列每轮预热 8 次，测量 512 次，以 ABBA 交替调用原 drain 和空闲探测。
两轮均值如下：

| 指标 | 原实现 | 新实现 |
|---|---:|---:|
| SELECT / 512 polls | 1024 | 512 |
| BEGIN / 512 polls | 512 | 0 |
| ROLLBACK / 512 polls | 512 | 0 |
| 每次空轮询耗时 | 489.82 µs | 167.55 µs |

心跳批量更新 SQL 的单条和 128 条满批次按需初始化并复用，
其余长度按精确字符串长度预分配。缓存仅两条固定语句，不缓存所有批次形状。
新旧 SQL 全部 128 种有效长度逐字节一致。构建函数微基准（四次测量中位数）：

| 批次长度 | 原耗时 | 新耗时 | 原分配 | 新分配 |
|---|---:|---:|---:|---:|
| 1 | 340.25 ns | 7.10 ns | 960 B/op | 0 B/op |
| 32 | 2512.50 ns | 1212.50 ns | 9472 B/op | 4096 B/op |
| 128 | 8063.50 ns | 6.35 ns | 38144 B/op | 0 B/op |

复现候选 SQL 构建基准：在 backend 目录运行
`GOMAXPROCS=2 go test -run '^$' -bench '^BenchmarkHeartbeatBatchSQL$' -benchmem ./internal/telemetryworkflow`。
空闲对照与逐字节验证使用原始实现保留的同一二进制实验。
已运行真实 MySQL 保序、ACK 持久化与损坏密文恢复竞态回归，
以及心跳错误重入队、绑定版本和容量测试。Go vet 通过。

这些测量不包含整个服务 CPU、RSS、设备端延迟或磁盘吞吐。
探测为正但最早任务仍在等待重试时，仍需原领取事务判断，不改变顺序。
