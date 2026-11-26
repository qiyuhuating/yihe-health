# 图片存储与多实例运维

当前 `BLOB_BACKEND=mysql`，图片密文由 MySQL `image_payload` 保存，引用目录位于 `image_blob`。上传须经过大小、图像解码、尺寸及重新编码检查，不能信任扩展名。图片读写遵循服务端权限、认证加密及完整性验证；多实例必须使用同一业务库及匹配的长期加密密钥和 keyring。

- 当前配置、历史图片导入及回滚条件：[MySQL 单库迁移](../MYSQL_ONLY_MIGRATION.md)。
- 事务一致备份、密钥独立托管、隔离恢复及切换：[备份与恢复](../runbooks/backup-restore.md)。
- 可重复的真实数据库恢复测试：[单 MySQL 协调恢复回归](coordinated-recovery.md)。
- 密钥轮换与保留历史 reader：[密钥轮换](rekey-data.md)。

## 现行维护命令

在 `backend` 目录、明确选择数据库和匹配加密密钥后执行：

```powershell
go run ./cmd/reliability --action verify-images
go run ./cmd/reliability --action gc-images --grace 24h
```

GC 默认只报告候选项，至少保留 24 小时宽限期；实际删除需显式 `--apply` 并先确认备份及引用状态。备份期间可使用 `backup-lock` / `backup-unlock` 和相同的 `--owner`，租约为一小时。不要绕过目录和引用保护。

备份必须包含 BLOB、目录、引用、迁移账本和相应密钥。恢复前隔离真实通知；验证授权下载及完整性后才允许切换。不要把系统进程重启或数据库备份成功等同于生产高可用和已验证恢复能力。

旧本地目录和对象存储只属于历史部署迁移材料，不能作为当前运行时图片后端。历史备份、迁移及回滚脚本不得因更新本文而删除。
