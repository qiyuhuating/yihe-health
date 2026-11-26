# 家属地址保护

迁移 `0038_family_address_protection.sql` 增加密文列和保护标记。配置数据密钥后，管理员新增和修改资料、家属自行修改资料、邀请注册和激活已有账号都以 `family-address:<family_id>` 为认证范围写入地址，原地址列为 NULL。查询仍按原权限和手机号索引筛选，在返回家属列表、个人资料和长者详情时解密。没有关联家属的长者继续返回 NULL。

更新前在原事务内锁定账号并认证已有地址，坏密文或缺少对应 reader 时终止更新。邀请、账号、住养老人关联和会话撤销仍用原业务事务。地址不参与搜索或唯一约束，回填保留 NULL、空字符串、末尾空格及重复地址。旧版回退脚本保留密文和迁移记录。若必须回退应用，先从匹配旧版的备份恢复，并核对切换后的变更。

在已迁移 schema 的 MySQL 上使用后端目录构建 `go build -o protect-family-addresses ./cmd/protect-family-addresses`。通过受限的 `MYSQL_DSN_FILE` 和 `DATA_ENCRYPTION_KEY_FILE` 提供与部署匹配的连接及密钥。

```text
protect-family-addresses
protect-family-addresses --apply --limit 100
protect-family-addresses
```

默认扫描只读，仅输出汇总。执行 apply 前暂停相关写入并验证备份可恢复。回填在完整预检后按行加锁、读取和提交，每次 1–1000 条，两分钟期限。部分完成返回非零及剩余量，可继续运行。若库里还没有任何地址密文，预检没有地址密文可用于验证 reader，操作前应核对密钥与部署配置一致。

`rekey-data` 的第 27 个目标是家属地址密文。先部署旧、新 reader，再切换新写 key，完成重封装和复查。历史备份仍须使用匹配的 reader 做恢复验收。
