# 单 MySQL 协调恢复回归

当前在线系统只依赖 MySQL 8.4；图片密文、会话、健康记录与通知队列都在同一数据库内。生产备份与恢复流程见[备份与恢复](../runbooks/backup-restore.md)。

## 运行隔离演练

配置专用的 `TEST_MYSQL_DSN`，测试账户需要创建和删除随机测试库的权限。安装与测试实例匹配的 `mysql` 和 `mysqldump`；CI 使用同一作业的 MySQL 服务容器及 `TEST_MYSQL_CONTAINER_ID`。测试验证客户端与 Go 连接的服务器 UUID 一致。

在仓库根目录执行：

```powershell
python scripts/reliability/run-mysql-restore-drill.py --output recovery.jsonl
```

脚本要求新建输出文件，并验证 `TestMySQLCoordinatedBackupRestore` 明确通过；跳过、失败或退出异常均不算验收通过。

## 验证范围

演练为合成工单写入待发通知、访问审计、会话及已轮换的图片，执行包含二进制列的事务一致备份。破坏测试源库中的一条密文后，将 dump 恢复到新建空库，比较迁移记录、审计、工单、队列、会话、图片和版本摘要。恢复后的 outbox 外键必须拒绝孤立记录；相同 SOS 请求重试必须返回原工单并保持事件及通知队列。

测试仅使用独立生成的数据库和合成数据，不向证据文件输出 SQL dump、原始密钥或敏感记录。不得连接生产数据库或触发真实通知。

小规模自动化回归不能替代生产容量下的 RPO/RTO、异地备份、密钥托管和真实网关送达验收。旧版 PostgreSQL、对象存储及本地图片迁移见[单库迁移](../MYSQL_ONLY_MIGRATION.md)；旧版演练代码与证据应在对应历史提交中查阅。
