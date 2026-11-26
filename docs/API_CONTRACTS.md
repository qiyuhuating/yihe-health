# 前后端接口合同与差异处理

接口路径以 Go 路由为准，当前共 109 个路径。`scripts/generate-openapi.py --check` 校验路由、OpenAPI 和网页状态码字典；字段清单保存在 `backend/internal/apidoc/contracts.json`。仓库提交 `openapi-template.json`，服务在首次请求时将模板中的共享定义、接口差异和字段清单合并，再通过 `GET /openapi.json` 返回完整契约。

## Preview 命名

运营和家属页面分别使用 `/operations`、`/family-portal` 命名空间，登录与家属注册入口为
`/sign-in`、`/join-family`。健康与活动接口使用 `/api/v1/vitals/`、`/api/v1/activity/`，
公告、评论和展示图片使用 `/api/v1/notices/`、`/api/v1/notice-comments/`、`/api/v1/gallery/`。
这些入口仅注册新路径。健康列表使用 `vitalRecords`、`activityRecords`，活动记录的睡眠告警字段为
`sleepAlertState`。

运营总览通过 `POST /api/v1/locations/alerts/summary` 获取长者总数和最新有效告警数。该接口仅返回计数。

## 请求与响应

JSON 成功使用 HTTP 200，根字段 `returnCode` 固定为字符串 `1111`。业务字段保留在根对象中。失败使用 HTTP 4xx/5xx，根字段为 `returnCode="0000"`、字符串 `code`、字符串 `message`；内部错误另带 `reference`，网页和手表均展示该编号。

分页请求使用 `page`、`pageSize`，分页结果使用数值 `total`、`page`、`pageSize`。既有表单接口接受整数或整数字符串 ID；按结构解码的健康、围栏与命令接口使用 JSON 数值。网页写操作保留 CSRF 与会话认证，设备接口校验设备密钥；授权失败与网络失败均不自动重放写操作。

`image/saveImage` 使用 multipart `file`，返回 `filename`。`image/download?name=...` 成功返回图片字节；失败仍返回 JSON 错误。健康时间使用带时区的 RFC3339；历史业务列表沿用明确的日期或日期时间格式。可空字段保留 `null`，空集合返回 `[]`。

## 差异与处理结果

| 位置 | 原差异 | 处理结果 |
| --- | --- | --- |
| `backend/internal/response` 与所有 JSON 调用点 | 查询、认证、业务操作的成功包格式不同 | 统一补充 `returnCode="1111"`，移除调用点重复标记 |
| 评论查询（两端页面） | 发送后端未读取的 `familyPhone` | 删除参数，身份从会话读取 |
| 评论发表 | 发送 `phone`、`personName`、`touPic`，后端使用会话身份 | 删除三个多余字段 |
| 评论赞同 | 发送后端未读取的 `phone` | 仅发送 `saidId` |
| 公告删除与编辑 | 发送后端未读取的 `picId` | 删除参数，移除编辑器的闲置 `picId` 状态；图片归属由后端原记录确定 |
| 长者新增、编辑 | 直接发送表单或查询 DTO，夹带入院时间、家属和设备展示字段 | 只发送后端可编辑字段与 `oldId`，保留 `customerId` |
| 阈值与家属编辑 | 发送查询行的附加字段 | 阈值限制为设备 ID 和四项阈值，家属限制为六项档案字段 |
| 设备确认操作 | 内层请求未返回给确认框链，错误仅打印日志 | 返回 Promise；取消关闭不报错，HTTP 失败显示统一消息 |
| 人员权限与审计面板 | 错误消息各自拼接，人员刷新错误可能被清除 | 使用统一消息，保留人员刷新错误状态 |
| 公告与展示图片参数 | 非数组返回 500，数组元素被自动转成字符串 | `fileListName` 统一为字符串数组，类型错误返回 400 |
| 表单校验与业务失败 | HTTP 200 携带失败码，前端容易按成功处理 | 改为 HTTP 400，统一 `INVALID_REQUEST` 和业务消息 |
| `handler_device_protocol`、健康与命令认证 | `0004` 同时表示密钥错误和停用，部分接口返回状态不同 | `DEVICE_AUTH_FAILED` 对应 401，`DEVICE_DISABLED` 对应 403；锁后复核使用相同定义 |
| 设备绑定码、家属邀请码 | `0001` 至 `0006` 含义分散 | 不存在 404、已用或过期 409、缺参数 400；使用语义错误码 |
| 档案与阈值写入 | 未命中记录只返回裸失败码 | 返回 404 `NOT_FOUND`；重复阈值档案返回 409 `CONFLICT` |
| 公告详情 | 公告不存在返回 500 | 返回 404 `NOT_FOUND` |
| `cmd/server/database.go` | 服务状态未入 OpenAPI，错误方法进入其他路由 | 纳入接口清单，错误方法返回 JSON 405 和 `Allow: GET` |
| `response.Router` | 未注册路径、错误方法使用默认文本响应 | 已授权 API 请求统一 JSON 404/405，405 保留 `Allow` |
| `handler_vitals_steps` | `allCount` 是字符串，其余计数是数值 | 改为整数 |
| `handler_vitals_measurements` | 曲线同时返回数组和拼错名称的重复对象列表 | 保留 `hrs`、`bps`、`createTimes`、`time`，删除无人使用的 `ratepreDtoListDetial` |
| 网页 Axios 与页面错误处理 | 部分页面丢失后端消息、状态及追踪编号 | 共享 `httpContract.ts` 校验结果包，`apiErrorMessage` 展示后端消息与编号 |
| 图片上传 | Element Plus 将错误响应放入 `UploadAjaxError.message` | 解析 JSON 错误包，头像与公告上传使用同一错误格式 |
| 鸿蒙 `WatchApi.ets` | 非 2xx 只显示通用 HTTP 提示，所有 409 都解释为绑定冲突 | 读取后端 `message` 与 `reference`，保留请求销毁与单次发送 |
| OpenAPI 与网页状态码 | 文档遗漏根字段、上传类型和错误结构 | 109 路径登记请求与响应字段，错误模型集中定义，网页状态字典由 Go 生成 |
| 浏览器验收夹具 | 旧成功数据缺少结果包 | 按新合同补齐，保留原来的业务、CSRF、写入次数与布局断言 |

## 状态码

| HTTP | 默认 code | 含义 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | 参数或业务输入无效 |
| 401 | AUTH_REQUIRED | 需要登录或设备认证 |
| 403 | ACCESS_DENIED | 角色、设备状态或 CSRF 不允许操作 |
| 404 | NOT_FOUND | 记录或接口不存在 |
| 405 | METHOD_NOT_ALLOWED | HTTP 方法错误，允许方法见 Allow |
| 409 | CONFLICT | 版本、归属或状态冲突 |
| 413 | PAYLOAD_TOO_LARGE | 请求或结果超出大小上限 |
| 415 | UNSUPPORTED_MEDIA_TYPE | 请求内容类型错误 |
| 429 | RATE_LIMITED | 触发限流 |
| 500 | INTERNAL_ERROR | 服务内部失败，保留 reference |
| 503 | SERVICE_UNAVAILABLE | 数据库或必要服务暂不可用 |


## 逐路径字段核对

下表列出根请求字段与可能出现的成功字段；条件字段并非每次都会出现。嵌套业务对象的字段由对应 Go DTO 和前端类型定义。所有 JSON 成功结果另含 `returnCode`，图片下载除外。

| 方法与路径 | 请求字段 | 成功字段 |
| --- | --- | --- |
| `GET /api/v1/auth/session` | 无 | `account`, `authenticated`, `csrfHeader`, `csrfToken`, `setupRequired` |
| `GET /api/v1/image/download` | `name` | 图片字节 |
| `GET /api/v1/service/status` | 无 | `databaseReady`, `primaryEngine`, `configError`, `analytics` |
| `POST /api/v1/alert-reviews` | `expectedReviewId`, `kind`, `reason`, `recordId`, `verdict` | `alarmStateChanged`, `reviewId`, `verdict` |
| `POST /api/v1/alert-reviews/search` | `after`, `entity`, `kind`, `recordId` | `items` |
| `POST /api/v1/auth/login` | `otp`, `password`, `phone`, `recoveryCode` | `authenticated` |
| `POST /api/v1/auth/logout` | 无 | 结果包 |
| `POST /api/v1/care/accessAudit` | 无 | `accesses` |
| `POST /api/v1/care/assignDuty` | `adminId`, `duty` | `adminId`, `duty` |
| `POST /api/v1/care/diagnostics` | 无 | `analytics`, `failedNotifications`, `note`, `notificationConfigured`, `oldestOutstandingAt`, `overdueIncidents`, `pendingNotifications`, `primaryEngine`, `receiptConfigured`, `staleSending`, `workerHealthy`, `workerLastSeenAt`, `workerStartedAt` |
| `POST /api/v1/care/events` | `id`, `page`, `pageSize` | `events`, `page`, `pageSize`, `total` |
| `POST /api/v1/care/incidents` | `state`, `page`, `pageSize` | `actorId`, `duty`, `items`, `page`, `pageSize`, `total` |
| `POST /api/v1/care/receipt` | `notificationId`, `status` | `notificationId`, `status` |
| `POST /api/v1/care/staff` | 无 | `staff` |
| `POST /api/v1/care/transition` | `action`, `id`, `note`, `verdict` | `id`, `state` |
| `POST /api/v1/guardians/create` | `customerAddress`, `customerAge`, `customerName`, `customerPhone`, `customerSex` | 结果包 |
| `POST /api/v1/guardians/delete` | `customerId` | 结果包 |
| `POST /api/v1/guardians/list` | `customerName`, `page`, `pageSize` | `customers`, `page`, `pageSize`, `total` |
| `POST /api/v1/guardians/update` | `customerAddress`, `customerAge`, `customerId`, `customerName`, `customerPhone`, `customerSex` | 结果包 |
| `POST /api/v1/desktop/exit` | 无 | `closing` |
| `POST /api/v1/device-command-profiles` | `deviceId`, `enabled`, `protocol`, `wireType` | `deviceId`, `enabled` |
| `POST /api/v1/device-commands` | `action`, `deviceId`, `requestKey`, `ttlSeconds` | `commandId`, `duplicate`, `status` |
| `POST /api/v1/device-commands/search` | `after`, `deviceId` | `items` |
| `POST /api/v1/device/alertCounts` | 无 | `items`, `personTotal`, `preTotal`, `rateTotal`, `total` |
| `POST /api/v1/device/alertDetail` | `deviceId`, `kind` | `alias`, `deviceId`, `deviceNo`, `oldPersonName`, `personHandled`, `preHandled`, `rateHandled`, `hasAlert`, `message`, `kind`, `kindText`, `reason`, `detail`, `value`, `threshold`, `occurTime`, `recordId`, `advice` |
| `POST /api/v1/device/bind` | `code`, `deviceNo`, `secret` | `oldId`, `oldPersonName` |
| `POST /api/v1/device/bindCodeList` | `oldId` | `bindCodes` |
| `POST /api/v1/device/commandAck` | `deviceNo`, `secret`, `bindingVersion`, `commandId`, `deliveryToken`, `result` | `commandId`, `status` |
| `POST /api/v1/device/commandPull` | `bindingVersion`, `deviceNo`, `secret` | `command` |
| `POST /api/v1/device/create` | `alias`, `model`, `oldId` | `deviceId`, `deviceNo`, `secret` |
| `POST /api/v1/device/createBindCode` | `createUser`, `expireDays`, `oldId` | `code`, `expireTime`, `oldId`, `oldPersonName` |
| `POST /api/v1/device/delete` | `deviceId` | 结果包 |
| `POST /api/v1/device/deleteBindCode` | `id` | 结果包 |
| `POST /api/v1/device/fencePoint` | `deviceNo`, `secret`, `bindingVersion`, `coordinateSystem`, `point`, `accuracy`, `occurredAt` | `events` |
| `POST /api/v1/locations/alerts/resolve-latest` | `oldId` | 结果包 |
| `POST /api/v1/device/healthBatch` | `bindingVersion`, `deviceNo`, `samples`, `secret` | `accepted`, `duplicates`, `writeRevision` |
| `POST /api/v1/device/heartbeat` | `appVersion`, `battery`, `deviceNo`, `secret` | `bindingVersion`, `bound`, `oldId`, `pendingAlarm`, `reportInterval`, `serverTime` |
| `POST /api/v1/device/list` | `query`, `state`, `page`, `pageSize` | `devices`, `onlineCount`, `page`, `pageSize`, `total` |
| `POST /api/v1/device/login` | `deviceNo`, `platform`, `protocolVersion`, `secret` | `alias`, `bindingVersion`, `bound`, `deviceId`, `deviceNo`, `oldId`, `oldPersonName`, `protocolVersion`, `maxBatchSize` |
| `POST /api/v1/device/report` | `deviceNo`, `secret`, `hr`, `bp`, `step`, `sleep`, `sleepState`, `lng`, `lat`, `alarm`, `battery`, `beTime`, `appVersion`, `requestId`, `source`, `positionSource`, `positionSampledAt`, `positionAccuracy` | `message`, `preState`, `queued`, `rateState`, `serverTime` |
| `POST /api/v1/device/resetSecret` | `deviceId` | `secret` |
| `POST /api/v1/device/resolveAlert` | `deviceId`, `kind`, `recordId` | `handledReason`, `message` |
| `POST /api/v1/device/sos` | `alarm`, `bindingVersion`, `deviceNo`, `lat`, `lng`, `positionAccuracy`, `positionSampledAt`, `positionSource`, `requestId`, `secret` | `duplicate`, `incidentId`, `incidentState`, `message`, `notificationState` |
| `POST /api/v1/device/unbind` | `deviceId` | 结果包 |
| `POST /api/v1/family/add` | `familyAddress`, `familyName`, `familyPhone`, `familyRelation`, `familySex`, `oldPhone` | 结果包 |
| `POST /api/v1/family/deleteFamily` | `familyId` | 结果包 |
| `POST /api/v1/family/select` | `familyName`, `page`, `pageSize` | `familyDtos`, `page`, `pageSize`, `total` |
| `POST /api/v1/family/selectTwo` | `familyPhone` | `familyDto2` |
| `POST /api/v1/family/updateFamily` | `familyAddress`, `familyId`, `familyName`, `familyPhone`, `familyRelation`, `familySex` | 结果包 |
| `POST /api/v1/family/updatePassword` | `familyPassword`, `familyPhone`, `oldPassword` | 结果包 |
| `POST /api/v1/family/updateTouPic` | `familyPhone`, `familyTouPic` | 结果包 |
| `POST /api/v1/family/updateTwo` | `familyAddress`, `familyId`, `familyName`, `familyPhone`, `familySex` | 结果包 |
| `POST /api/v1/fence-events/search` | `after`, `oldId` | `items` |
| `POST /api/v1/fences` | `coordinateSystem`, `coordinates`, `deviceId`, `dwellSeconds`, `enabled`, `id`, `maxAccuracy`, `minPoints`, `name`, `revision`, `timeBegin`, `timeEnd`, `timezone` | `deviceApplied`, `id`, `revision`, `state` |
| `POST /api/v1/fences/search` | `deviceId` | `items` |
| `POST /api/v1/health/changes` | `after`, `limit`, `oldId` | `changes`, `hasMore`, `nextCursor`, `oldId` |
| `POST /api/v1/health/daily-query` | `oldId`, `source`, `sourceDevice`, `metric`, `semantics`, `day`, `offsetMinutes`, `days` | `algorithmVersion`, `items`, `metric`, `offsetMinutes`, `semantics`, `source`, `sourceDevice` |
| `POST /api/v1/health/latest` | `oldId` | `items`, `oldId` |
| `POST /api/v1/health/statistics` | `from`, `metric`, `oldId`, `semantics`, `source`, `sourceDevice`, `to` | `metric`, `semantics`, `source`, `sourceDevice`, `statistics` |
| `POST /api/v1/image/saveImage` | `file` | `filename` |
| `POST /api/v1/inviteCode/checkCode` | `code` | `expireTime`, `oldPersonName`, `remainDays` |
| `POST /api/v1/inviteCode/create` | `count`, `createUser`, `expireDays`, `oldId` | `codes`, `expireTime`, `oldId`, `oldPersonName` |
| `POST /api/v1/inviteCode/delete` | `id` | 结果包 |
| `POST /api/v1/inviteCode/join` | `code`, `familyAddress`, `familyName`, `familyPassword`, `familyPhone`, `familyRelation`, `familySex` | `familyId`, `familyName`, `oldPersonName` |
| `POST /api/v1/inviteCode/list` | `page`, `pageSize`, `state` | `inviteCodes`, `page`, `pageSize`, `total` |
| `POST /api/v1/inviteCode/oldPersonList` | 无 | `oldPeople` |
| `POST /api/v1/notifications/search` | `after`, `familyId` | `items` |
| `POST /api/v1/residents/create` | `name`, `bothdate`, `sex`, `phone`, `room`, `illness`, `warn`, `foodLike`, `foodHate`, `drag`, `customerId`, `deviceId` | 结果包 |
| `POST /api/v1/residents/delete` | `oldId` | 结果包 |
| `POST /api/v1/residents/list` | `name`, `page`, `pageSize` | `oldPeople`, `page`, `pageSize`, `total` |
| `POST /api/v1/residents/details` | `name`, `page`, `pageSize` | `oldPersonDetails`, `page`, `pageSize`, `total` |
| `POST /api/v1/residents/update` | `oldId`, `name`, `bothdate`, `sex`, `phone`, `room`, `illness`, `warn`, `foodLike`, `foodHate`, `drag`, `customerId`, `customerName`, `deviceId` | 结果包 |
| `POST /api/v1/gallery/replace` | `fileListName` | 结果包 |
| `POST /api/v1/gallery/list` | `id`, `picName` | `galleryImages` |
| `POST /api/v1/position/select` | `name`, `page`, `pageSize` | `page`, `pageSize`, `positionDtos`, `total` |
| `POST /api/v1/position/selectFamily` | `oldId` | `positionDtos` |
| `POST /api/v1/vitals/alerts/count` | 无 | `preTotal`, `rateTotal`, `total` |
| `POST /api/v1/vitals/alerts/detail` | `kind`, `oldId`, `recordId` | `hasAlert`, `message`, `recordId`, `deviceId`, `oldId`, `oldPersonName`, `hr`, `bp`, `rateBad`, `preBad`, `occurTime`, `reason`, `detail`, `advice` |
| `POST /api/v1/vitals/alerts/resolve` | `kind`, `recordId` | `message` |
| `POST /api/v1/vitals/latest` | `name`, `timeFirst`, `timeLast`, `page`, `pageSize` | `page`, `pageSize`, `vitalRecords`, `total` |
| `POST /api/v1/vitals/alerts/device-counts` | 无 | `heartRate`, `bloodPressure` |
| `POST /api/v1/vitals/chart` | `oldId` | `bps`, `createTimes`, `hrs`, `time` |
| `POST /api/v1/vitals/history` | `oldId` | `vitalRecords` |
| `POST /api/v1/rule-policies` | `deviceId`, `policy`, `revision` | `policy`, `revision` |
| `POST /api/v1/notice-comments/delete` | `saidId` | 结果包 |
| `POST /api/v1/notice-comments/toggle-like` | `saidId` | 结果包 |
| `POST /api/v1/notice-comments/create` | `said`, `textId` | 结果包 |
| `POST /api/v1/notice-comments/list` | `textId` | `comments` |
| `POST /api/v1/notice-comments/search` | `page`, `pageSize` | `page`, `pageSize`, `moderatedComments`, `total` |
| `POST /api/v1/notice-comments/reply` | `back`, `saidId` | 结果包 |
| `POST /api/v1/security/factor` | `action`, `otp`, `password` | `secret`, `uri`, `recoveryCodes`, `reloginRequired` |
| `POST /api/v1/security/login-audit` | `after` | `items` |
| `POST /api/v1/setup` | `name`, `password`, `phone` | 结果包 |
| `POST /api/v1/health/alerts/device-counts` | 无 | `heartRate`, `bloodPressure`, `sleep` |
| `POST /api/v1/activity/latest` | `name`, `timeFirst`, `timeLast`, `page`, `pageSize` | `page`, `pageSize`, `activityRecords`, `total` |
| `POST /api/v1/activity/alerts/device-counts` | 无 | `sleep` |
| `POST /api/v1/activity/chart` | `oldId` | `createTime`, `sleep`, `activityRecords` |
| `POST /api/v1/activity/history` | `oldId` | `activityRecords` |
| `POST /api/v1/notices/create` | `fileListName`, `textMain`, `title` | 结果包 |
| `POST /api/v1/notices/delete` | `textId` | 结果包 |
| `POST /api/v1/notices/list` | `title`, `page`, `pageSize` | `page`, `pageSize`, `texts`, `total` |
| `POST /api/v1/notices/detail` | `textId` | `pics`, `text` |
| `POST /api/v1/notices/update` | `textId`, `title`, `textMain`, `fileListName` | 结果包 |
| `POST /api/v1/user/select` | `userPhone` | `usersByPhone2` |
| `POST /api/v1/monitoring-profiles/create` | `deviceId`, `hrf`, `hrl`, `bpf`, `bpl` | 结果包 |
| `POST /api/v1/monitoring-profiles/delete` | `deviceId` | 结果包 |
| `POST /api/v1/monitoring-profiles/list` | `oldName`, `page`, `pageSize` | `page`, `pageSize`, `total`, `watchDtos` |
| `POST /api/v1/monitoring-profiles/update` | `deviceId`, `hrf`, `hrl`, `bpf`, `bpl` | 结果包 |
