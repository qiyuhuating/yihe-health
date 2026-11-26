# 监护人联系方式保护

## 数据与接口边界

`0037_guardian_contact_protection.sql` 扩展 `customer`，不自动回填原数据。
配置有效数据密钥后，新建/编辑将电话和地址按 `guardian-contact:<customer_id>` scope
封装为随机 AEAD，原 `customer_phone` / `customer_address` 为 NULL。
保护标记、密文列及 CHECK 阻止受保护行混入明文。v1 载荷固定 phone/address 两字段，
保留 NULL/空字符串和 UTF-8。电话仍最多 12 字符，支持固定电话；地址最多 200 字符。

授权列表按真实记录 ID 认证并恢复原接口字段，不输出保护元数据。
姓名模糊查询、分页/总数、年龄校验、职责权限、监护人 ID 及长者关联/删除锁保持原逻辑。
电话和地址不参与查询或唯一约束，不新增 HMAC 电话索引。姓名/性别/年龄和其他身份资料
仍在原保护边界内；本项不等于整个 P0-06 完成。
新建先在原事务中预留 ID，再直接存密文，不临时插入明文联系人；编辑先锁 key-state、
锁监护人行并认证旧联系人，再改任何字段。失败回滚原事务，不存在的 ID 不提交新 marker。

## 密钥校验和轮换

独立 `guardian_contact_key_state` 保存 lookupKey 域隔离指纹及加密的 reader 校验记录。
后者 scope 为 `guardian-contact-key-state`，只认证版本和指纹，不含联系人。
缺失/错误 AEAD reader、错误 binding key、损坏或丢失 key-state 拒绝读写，包括空列表
和创建新监护人，不能通过覆盖资料掩盖错误密钥。
仅在受保护行与 key-state 均不存在的旧部署中保留无密钥行为；建立保护后不能退回明文写入。

先配置 A+B readers、active B，再运行新版 `rekey-data`。第 25 项
`customer.contact_cipher` 保持每行 scope，第 26 项 `guardian_contact_key_state.key_verifier`
保持固定 marker scope，两者都必须重封装。lookupKey 指纹、ID、姓名、年龄、性别和长者
关系不变；遗漏 marker 的旧工具不能证明可以移除 A。
`TestGuardianContactRotationMySQL` 覆盖仅 B 的真实列表和编辑；全资产最终退役另需证明，
`retirement_authorized` 始终 false。

## 历史回填

后端目录构建 `go build -o protect-guardian-contacts ./cmd/protect-guardian-contacts`。
通过受限 `MYSQL_DSN_FILE` / `DATA_ENCRYPTION_KEY_FILE` 选择明确数据库与现有密钥，
schema 迁移须已完成，不把配置值放到命令行或证据中。

```text
protect-guardian-contacts > scan.json
protect-guardian-contacts --apply --limit 100 > apply.json
protect-guardian-contacts > verify.json
```

默认 scan 只读、不建立 key-state；apply 前须停相关写入并验证匹配备份可恢复。
完整预检认证全部新/旧行和 key-state，再逐行事务重读、加密、清空旧列并提交。
每次 1–1000 行，CLI 总期限两分钟。合法重复值、NULL/空值和记录数保持；NULL-only
旧行不制造联系人。部分完成返回非零和 remaining，保留写屏障/readers 后续跑；
失败不回滚已提交的前序行。报告仅聚合计数，不输出联系人、行 ID、密文、DSN 或 key。

回填和应用使用同一锁顺序，锁内重读保留并发更新，但不代替正式停写屏障。
错误 key、损坏密文/marker、丢失 marker、不一致元数据及预检取消不得改写候选行。
回退迁移保留扩展列和 key-state，不自动导出明文或让旧程序覆盖受保护资料。
回归覆盖实际 API、固定电话/长地址、错 key 拒绝、事务回滚、scan/分批/幂等/预检/取消、
并发行锁、CLI 及 26 项轮换。独立测试库的验证不代表业务库已 apply、历史备份处置完成或商业验收。
