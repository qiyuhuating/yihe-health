# 居民快照隐私契约 · v0.3.2

适用于登录角色 resident 的 `GET /api/v1/care/snapshot`，在后端 `serializers.resident_snapshot` 统一执行，客户端不承担脱敏责任。工作人员快照保持原授权视图。数据库存储结构不变。

| 字段 | 居民输出规则 |
|---|---|
| staff | 固定一个 `{id:"care-team",name:"照护团队"}`；不查询/返回真实工作人员名单 |
| residents | 只含当前会话 residentId；资料由明确白名单复制 |
| responsible | 固定 `care-team`，不公开内部 staff ID |
| events.owner / history.owner | 未分配为 null；已分配为 `care-team`，保留跟进状态 |
| history.actor | system 原样；所有其他操作人统一为“工作人员” |
| history.action | 仅允许已定义的系统动作与状态动作；其他内部动作显示“状态更新” |
| history.note | 固定空字符串，保留前端 JSON 结构 |
| doses.actor | 不返回账号或其他操作人标识 |
| doses.source | 固定“工作人员登记”，不复制内部来源文本 |
| logs | 固定空数组；居民不能访问工作人员审计 API |
| source / signalId / previousIncidentId | 不进入居民事件 DTO |
| 未知字段 | 任何层级都默认不公开，包括审计对象、敏感备注和新增内部字段 |

允许的居民资料标量：id、name、age、risk、revision、deviceState、lastSeen、updatedAt、locationAt、heartRate、bloodOxygen、temperature、systolic、diastolic、bloodSugar。contacts 只复制 name / phone；pos 只复制 x / y；metricStates 与 trend 只复制已定义指标，trend 另外允许 at。

用药 DTO 只允许 id / name / dueAt / createdAt / confirmedAt，另添加固定 source。事件 DTO 只允许 id / residentId / type / severity / detail / state / revision / createdAt / dueAt / updatedAt / closedAt / sourceRecoveredAt，另构建 owner / notification / history。notification 只允许 cycle / deliveredCycle / presentedCycle / acknowledgedCycle。history 只公开 at / state 和上表中的固定/受限字段。

居民姓名、联系方式、指标、用药名称与事件 detail 是本人的业务数据，仍公开给本人。设备上游必须保证 detail 是供居民阅读的告警描述，不应把工作人员备注、账号或秘密写入这个公开业务字段；处置备注仅写入 history.note。serializer 保证字段边界，不猜测自由文本含义。

兼容策略：staff 集合及 responsible / owner 字段仍符合原 HTTP Adapter 的 knownStaff 校验；CareUI.staff 显示“照护团队”，无需修改 CareUI / CareSession / CareAPI。DTO 创建新对象，不改写内部数据库 JSON 或工作人员视图。

验证：`backend/tests/test_v022.py` 逐字段断言，向资料、联系人、位置、指标、趋势、用药、事件通知及历史中注入内部 canary，验证居民 JSON 不包含；同时验证工作人员仍能看到其授权信息。`tests-added/e2e_privacy.py` 验证真实后端响应、原前端 adapter、居民预警展示、资料保存和确认服药。
