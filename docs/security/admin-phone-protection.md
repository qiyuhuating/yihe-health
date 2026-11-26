# 管理员手机号保护

仅保护 `users` 表的管理员手机号。使用现有 AES-GCM 密钥封装，密文与 `user_id` 绑定；精确查找使用 HMAC 索引。原手机号列在迁移完成后必须为 NULL。

## 新部署

在服务启动前配置 `DATA_ENCRYPTION_KEY`，使用受控的 32 字节随机密钥或包含独立 `lookupKey` 的密钥环。首次初始化在同一事务中写入密文、查询索引和不可替换的密钥绑定指纹。未配置密钥时，首次初始化沿用原有明文写入路径，便于尚未启用保护的部署渐进升级；配置密钥后新建管理员一律加密存储，存量数据用下方回填流程迁移。

## 存量数据

1. 停止账号写入，确认 MySQL 备份和恢复方法可用。不要在未备份的生产库直接试运行。
2. 确认新增迁移 `mysql/0023_admin_phone_protection.sql` 已成功执行。
3. 在后端目录执行 `go run ./cmd/protect-user-phones`，仅返回未迁移行数。
4. 配置与服务端一致的 `MYSQL_DSN`、`DATA_ENCRYPTION_KEY`，执行 `go run ./cmd/protect-user-phones --apply --limit 100`。每条记录独立事务，重复执行可继续处理未迁移记录。
5. 复核 `user_phone_protected=false AND user_phone IS NOT NULL` 数量为零，确认管理员登录、家属手机号冲突检查和恢复测试正常。

密钥指纹必须与数据库绑定一致。加密密钥可按密钥环策略轮换；查询 HMAC 密钥不能在未迁移索引前直接更换。工具不打印手机号、索引和密文。

0031 家属手机号扩展后，两类账号共用原有 lookupKey 指纹。任一角色已有保护记录时，指纹丢失都必须拒绝查询/绑定，不会自动用当前配置重建；家属端迁移与范围见 [family-phone-protection](family-phone-protection.md)。

已保护的手机号通过 [rekey-data](../reliability/rekey-data.md) 按 `account-phone:admin:<user_id>` scope 重封装，HMAC 索引与指纹保持原值。旧版轮换清单遗漏该列，不能用旧扫描的“无残留”结果停用 reader；升级后重新扫描并验证登录。单 key 旧部署继续沿用原 key 作为 lookupKey，不能顺便换成新的独立查询密钥。

当前命令不自动验证跨实例停写屏障、历史备份中的明文清除、密钥退役或灾难恢复。未完成这些运维验收前，不应宣布完整的数据保护达标。
