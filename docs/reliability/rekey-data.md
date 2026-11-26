# 已加密 MySQL 字段的分批轮换

最新运行架构为单 MySQL。下文的 PG/outbox 消费路径仅指历史部署，当前没有在线 PG 消费者。
`image_payload.payload` 是 MySQL 内的加密图片，尚不属于这 33 个文本/JSON 列目标；
图片使用独立的 [rekey-images](mysql-image-rotation.md) 扫描和切换密文版本。
历史 PG/S3/本地图片及备份恢复验收继续保留，不能据此授权删除旧 key。

`backend/cmd/rekey-data` 只处理现有 AEAD 字段，不会把姓名、电话或健康 JSON 等历史明文字段自动变成密文，也不改变 HMAC 索引。它是 P0-07 的部分实现，不能作为全库保护或旧密钥退役的证明。

## 覆盖和前置条件

固定清单位于 `internal/rekey/targets.go`，覆盖 33 个列目标，包括照护备注与事件、告警复核理由、两类通知收件人、设备最近坐标、位置历史、两张告警表的坐标、已存在的位置元数据、数据库会话、按角色/账号认证的 TOTP、规则证据的 JSON 密文包装、遥测 inbox、MySQL `analytics_outbox.payload` 中设备位置事件的坐标、已保护的身份资料，以及健康原始样本、同步变化流、最新样本和日统计副本。清单包含复合主键与字符串主键，不接受命令行拼接表名或字段名。

管理员 `users.user_phone_cipher` 使用与生产 `userphone.Protect/Reveal` 相同的 `account-phone:admin:<user_id>` scope，不能把两个账号的密文互换。轮换只修改密文，不改变 user_phone_lookup、lookup_verifier、user_phone_protected、登录密码或账号状态；历史未迁移手机号和已移除的 NULL 手机号保持原值。缺少此目标的旧版本轮换工具可能遗漏仍依赖旧密钥的管理员账号，必须使用本版本重新扫描；不能根据旧扫描结果删除 reader。历史明文回填仍使用独立 protect-user-phones 工具及停写/备份流程，本工具不代替该迁移。

家属 `family.family_phone_cipher` 使用 `account-phone:family:<family_id>`，不与管理员或其他家属账号互通。第 22 项同样只重封装密文，保留 family_phone_lookup、保护标记与账号关系。历史明文回填使用独立 [protect-family-phones](../security/family-phone-protection.md)；该范围不包括社交表中的手机号副本。`TestFamilyPhoneBackfillMySQL` 额外验证回填后轮换、仅新 reader 的实际家属登录和原索引不变。

评论 `said.said_phone_cipher` 为第 23 项，使用 `social-comment-phone:<said_id>`，与实际评论读写保持一致。它不改点赞 HMAC、社交 lookup key binding、评论内容或计数。历史明文使用独立 [protect-social-phones](social-phone-protection.md)；旧版 22 项扫描不能证明评论已无旧密文。`TestSocialPhoneRekeyMySQL` 通过实际评论/点赞读写检查轮换后仅新 reader 仍能显示掩码并取消原投票；仍不授权全资产旧密钥退役。

长者 `oldperson.care_profile_cipher` 为第 24 项，使用 `resident-care-profile:<old_id>`。只重封装现有七字段档案，保持 phone_lookup、resident lookupKey 指纹、归属及设备关系不变；未回填的原始列不被自动改写。历史回填另用 [protect-resident-profiles](resident-care-profile-protection.md)。`TestResidentProfileRotationMySQL` 通过实际档案、设备、家属关联和编辑 API 验证仅新 reader 的行为；旧版 23 项扫描不能排除这项残留。

监护人 `customer.contact_cipher` 为第 25 项，使用 `guardian-contact:<customer_id>`；加密 reader marker `guardian_contact_key_state.key_verifier` 为第 26 项，使用固定 `guardian-contact-key-state` scope。两者都须重封装，lookupKey 指纹与监护人/长者关系保持。只处理联系人而遗漏 marker 的旧工具会保留 A reader 依赖；历史明文回填另用 [protect-guardian-contacts](guardian-contact-protection.md)。实际仅 B reader 的列表/编辑和 key-state 读取纳入对应回归，仍不授权全资产退役。

位置触发器会把当时的坐标密文复制进分析事件；只更新位置表不能消除历史 A 密文。第 20 项仅匹配 `entity=device_location`，在保留事件 ID、归属、时间、source、其他属性及顺序的同时，按原 `location` scope 重封装 `lng`/`lat`。任一个坐标认证失败便不更新整条载荷；删除事件的 NULL 载荷保持原样。其他实体的载荷不被当作坐标处理，也不自动加密原有健康 JSON。

先备份数据库与旧密钥，并验证备份可恢复。所有读实例和 worker 必须先能读 A+B，再把各写实例切到 active B；旧密钥 A 与原有 lookupKey C 继续保留。命令比较当前配置和 `--previous-key-file` 的 lookupKey，发生变化立即拒绝。单 key 旧配置的 lookupKey 实际就是旧 key；该部署仍须沿用它，独立 lookupKey 的索引迁移另行实施。

当前 `DATA_ENCRYPTION_KEY_FILE` 指向私有的新 keyring 文件，`MYSQL_DSN_FILE` 指向明确选择的数据库。命令不会生成密钥，不会运行 schema migration，不会切换正在运行的服务配置，也不会删除 reader。不要把配置内容写入命令行或验收制品。

家属地址 family.family_address_cipher 为第 27 项，按 family-address:<family_id> 认证。轮换不改手机号索引、家属 ID 或长者关联。历史明文使用 [protect-family-addresses](family-address-protection.md) 单独回填。

长者 `oldperson.birthdate_cipher` 为第 28 项，按 `resident-birthdate:<old_id>` 认证。历史日期回填使用独立的 `protect-resident-birthdates`，操作及年龄读取见[长者档案](resident-care-profile-protection.md#出生日期保护)。

第 29 到 31 项覆盖三个[健康副本密文列](health-projection-protection.md)，分别使用原写入路径的记录标识、序列键和版本认证范围。轮换不会消除各表残留的旧明文，回填与复验须单独运行。

第 32 项是 `health_sample.payload_cipher`，用样本标识、长者 ID 和版本游标认证。使用 `protect-health-projections --samples` 核查原载荷列的明文残留。

## 执行与恢复

在后端模块目录构建 `go build -o rekey-data ./cmd/rekey-data`，然后执行：

```text
rekey-data --previous-key-file <私有旧配置文件> > before.json
rekey-data --previous-key-file <私有旧配置文件> --apply --batch-size 100 > apply.json
rekey-data --previous-key-file <私有旧配置文件> > after.json
```

默认只读。`--apply` 先完整验证清单内所有密文，包括已经使用 active key 的密文；缺失 reader、错误 scope、损坏密文、异常 JSON 包装或超大字段都会阻止首次写入。旧明文仅计数，不自动转换或重建索引。

实际处理使用主键游标和每列高水位，默认每页 100 行、最多 1000 行，并限制页面/单字段字节数、总行数及总时间。读取页面后释放结果集，再逐行提交字节级 compare-and-swap；不长时间锁住整张表。若原值已被修改或行已被删除，保留并发结果并计入冲突。TOTP scope、规则 JSON 的其他属性、recipient_key、业务状态与绑定版本保持原值；现有数据库触发器照常执行，因此轮换可能新增不含备注原文的分析事件。

程序中断不会回滚已经提交的其他行。保留 A+B 后再次从头执行即可：active 密文先认证再跳过，旧密文继续处理，失败值不被替换。遇到任一冲突、认证错误、残留明文、超时或预算耗尽，apply 返回非零。CLI stdout 始终只输出聚合计数，stderr 使用固定错误说明，不输出行主键、密文、明文、DSN 或 key 内容。

`before`、`applied`、`after` 分别记录预检、实际更新和新扫描。`scanned` 与 `rewritten` 按数据库行计数；新增的 `values` 按检查的值计数，一个位置事件可以包含两个坐标值。`complete` 表示该列完成了本次高水位扫描；`encrypted_columns_clean` 仅表示本次扫描未发现清单内残留。分析消费者可能已经删除源事件并将 A 密文写入 PG，因此处理 MySQL outbox 不代表 PG 也已轮换。扫描期间的插入、旧实例继续写 A、对象、binlog、副本、WAL、历史备份和回滚程序也不能由此推断安全，`retirement_authorized` 始终为 `false`。最终停用 A 仍需独立的写版本屏障、全部资产残留盘点、最旧备份恢复和审查。

## 验证边界

`TestRekeyMySQL` 在全新迁移的真实 InnoDB 库中覆盖全部 33 个列目标。场景包括只读预检、单行分页/数字顺序/字符串与复合主键、直接字段历史密文及分析事件内的坐标 A→B、只保留 B 后读取、HMAC 与业务状态不变、规则 JSON 保留、损坏密文阻止前列写入、legacy 恢复重跑、行数耗尽、明文残留、超大字段、取消、并发更新和删除。测试核对历史位置事件不再含 A，并保留无关实体载荷。最终退役仍须单独验证。即使列使用忽略大小写及尾空格的 collation，CAS 也不得覆盖并发值。

`TestAdministratorPhoneRekeyMySQL` 使用生产手机号保护与认证代码验证旧密钥写入→新密钥重封装→仅保留新 reader 后两账号仍可登录，精确 HMAC 索引和 key binding 不变；另验证无版本单 key 旧格式在保留原 lookupKey 后可完成同一登录迁移。账号间密文互换阻止所有列的首次写入，CAS 保留并发手机号替换与索引。两个真实 MySQL 测试均列入必需结果审计，缺失、失败或意外 skip 不能通过门禁。

这不包含对象重写、历史索引迁移、PG 重建或协调备份恢复；内存保留的旧密文读取断言不等于真实备份恢复测试。原 `protect-notes` 仍是需要停写的独立明文迁移/离线回退工具，不能替代上述流程。
