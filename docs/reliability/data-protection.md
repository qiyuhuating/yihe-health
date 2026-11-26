# 数据分类与轮换设计

历史字段元数据保存在 `evidence/reliability-20261026/schema-bf33177.tsv`，来自完成迁移的 MySQL
`information_schema`。字段分类与迁移设计可从 Git 历史查阅；当前盘点应以实际部署 schema 为准。S2 为内部元数据，S3 为人员与
联系方式，S4 为健康、位置、凭据及可关联的敏感载荷。此分级是工程设计，不是合规认证。
表的其他列也可能通过 ID 关联到自然人，不能当作匿名数据发布。
历史 PG 的 7 个基表字段、12 个视图输出及非字段资产另列于
[projection-and-assets.csv](projection-and-assets.csv)，原 analytics schema 可从 Git 历史查阅。
最新运行架构以 [单 MySQL 迁移](../MYSQL_ONLY_MIGRATION.md) 为准；历史资产仍需恢复验收。

## 当前边界

已有加密只覆盖代码明确调用 securestore 的字段。当前实现包含收件人/坐标保护，并保留已有
session、TOTP、照护备注、复核理由、规则证据。新增私有图片 AEAD 与哈希校验。
管理员和家属主账号手机号的新写入已使用按角色/账号认证的 AEAD 与 HMAC 查询索引，历史回填工具分别为 protect-user-phones 和 [protect-family-phones](../security/family-phone-protection.md)；已保护的手机号也纳入 rekey-data 的分批轮换清单，保持查询索引及共用 key binding 不变。家属登录、资料与通知已适配；该项不代表其他人员联系方式及手机号副本已保护。
评论作者手机号新增按评论 ID 认证的 AEAD，点赞身份新增独立域 HMAC；原列在配置密钥的新写入中为 NULL。详见[社交手机号保护](social-phone-protection.md)，历史副本须显式运行 protect-social-phones 并核对聚合/重复数据，评论密文纳入 rekey-data，点赞摘要与独立 key binding 保持不变。
长者联系方式、房间、病情、注意事项、饮食喜恶和用药七列新增按长者 ID 认证的 v1 AEAD 档案封装，原列在配置密钥的新写入中清空；电话以独立域 HMAC 保留既有家属关联查找，非唯一索引保留重复号码歧义。详见[长者照护档案保护](resident-care-profile-protection.md)，历史迁移使用 protect-resident-profiles，密文列加入 rekey-data。该封装不处理遥测统计 JSON。
监护人电话/地址新增按监护人 ID 认证的 v1 AEAD 联系人封装及 reader 校验记录，原联系方式在有 key 新写中清空；姓名查询、ID 关联和年龄校验保留。历史值须显式运行 [protect-guardian-contacts](guardian-contact-protection.md)，联系人密文与加密的 reader marker 都纳入轮换。
家属地址的新写入使用按家属 ID 认证的 AEAD，授权读取保持原字段，历史回填和轮换见[家属地址保护](family-address-protection.md)。
长者出生日期在配置数据密钥后按长者 ID 认证加密，仅授权档案查询解密；历史回填和年龄语义见[长者档案保护](resident-care-profile-protection.md#出生日期保护)。
健康原始样本、同步变化流、最新样本和日统计结果已有[加密写入与回填](health-projection-protection.md)。原始样本密文化后，测量值生成列随原载荷置空；用于归属、时间窗和指标选择的索引元数据继续保留。
姓名等其他身份资料、旧数值上报、围栏/事件、社交文本及未回填的字段和历史副本仍需纳入迁移盘点。
不宣称全库加密。AEAD 只在配置正确密钥的新写路径生效；兼容旧明文读取不是迁移完成。

MySQL 是唯一在线持久化业务存储，图片密文在 `image_payload`，目录及引用继续由 MySQL 管理。
旧 PG 分析投影的 JSON/健康数值、备份、WAL、导出与死信数据仍至少采用源数据同级控制。
生产环境需要配置 MySQL 加密卷、受控快照及 TLS，并保留历史 PG 资产的访问及加密控制；
本机隔离测试的数据库文件没有据此宣称已加密。AEAD 不能防止持有应用密钥的在线攻击者。

另有历史私有对象 bucket、本地 IMAGE_DIR，以及 SQL dump、备份对象、访问日志、故障日志、
CI artifacts、配置 secret、会话 cookie 和密钥备份。备份应独立 AEAD 加密、独立访问控制，
密钥放在外部受控密钥库；不得与备份同包。日志禁止手机号、设备密钥、session、明文健康载荷、
网关凭据。配置只通过受限文件/secret 注入；生产密钥不进入仓库或验收证据。

## 版本与轮换

`DATA_ENCRYPTION_KEY` 支持原有单 key 和新 keyring JSON：`active`、最多 8 个 `keys`、
独立 `lookupKey`。每个 key 为 32 随机字节的 base64，key ID 为受限 ASCII。
新格式为 `sne1:v2:<key-id>:<nonce+ciphertext+tag>`，key ID 与字段 scope 一起认证。
旧无版本密文仅在有限 reader 集合中尝试解密。未知版本、错误 key、错误 scope、缺失 key
均返回错误，不自动生成替代 key。数据库凭据、TLS 私钥、lookupKey 与 AEAD key 分开保管。

新部署使用独立 lookupKey C，轮换步骤：

1. 备份并恢复验证 A 与 C；所有实例先配置 readers A+B，active A，检查版本一致。
2. 全实例改 active B，保持 C 不变。新写使用 B，旧数据仍使用 A reader。
3. 对每个加密 scope 分批读、校验、以 B 重封装；用版本/CAS 避免覆盖并发修改。核对总数、
   解密失败数、残留 A/无版本/明文数量，并记录审计，不把明文写入日志。
4. 对单 MySQL 图片使用 [rekey-images](mysql-image-rotation.md)。每张图片写入新的不可变密文版本，
   在同一事务内检查快照、切换指针并移除旧版本。业务图片名和引用保持原值，图片 GC 使用同一备份锁。
   历史 PG/S3 及本地文件的移交、归档和恢复仍按迁移手册执行。
5. 在隔离恢复环境恢复最旧保留备份并逐项解密。确认所有在线副本、回滚版本、备份、outbox
   都不再需要 A，才从在线 readers 移除 A；备份仍依赖时在离线密钥托管保留 A。

**旧部署前置条件**：原单 key 同时用于 HMAC，旧 recipient_key 也可能由无 key SHA 回填。
直接换为独立 C 会改变去重与登录审计摘要。必须先停写迁移或实现有期限的双摘要读取，重建
recipient 唯一约束并核对冲突，登录限流摘要需保留旧窗口/完成双读。当前没有自动执行此迁移，
不得把 keyring 原语测试当作生产历史数据轮换已完成。lookupKey 自身轮换同样需独立索引迁移。

测试覆盖 A 数据 → B 写入 → 旧 A 可读 → 重加密 → 移除 A 后可读，以及错误/丢失 key、scope、
ID 篡改失败。坐标解密失败会返回失败，图片/通知也不会把密文当明文送出。生产批准需要追加
真实历史全量盘点、并发迁移、跨实例切换和最旧备份恢复记录。

## 明文字段后续迁移

手机号采用规范化明文 → 加密列 + scoped HMAC 精确索引；不能使用可枚举的裸 SHA-256。
姓名/地址/病史用随机 AEAD，需要的搜索应明确设计授权搜索范围，不使用确定性密文泄露模式。
关系 ID、状态、lease、deadline 保留结构化，保证事务/重试/升级可运行。
健康 JSON 参与统计、生成列和范围索引，不能直接加密整个 JSON 后让算法失效；先定义最小
授权投影和计算边界，再扩列、双跑、回填与完整性核对。旧列、change 日志、归档、PG、历史备份
必须一起列入删除/保留计划。此阶段交付分类和迁移设计，没有在未经验证时批量改生产数据。
