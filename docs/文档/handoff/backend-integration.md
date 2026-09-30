# 后端接入交付说明

本文是前端与后端、部署团队的接口交接基准。前端实现已包含同源 HTTP 适配器、会话界面、快照校验、写操作反馈与静态发布构建；仓库不包含真实 API 服务、账户、数据库、通知投递或生产域名。完成此文档中的服务端工作并通过真实环境验收后，才能开放真实用户。

## 1. 接入目标与边界

- 浏览器只访问当前站点同源的 `/api/v1/`，Cookie 由浏览器管理；前端不保存会话 Cookie、健康记录或服务端凭据到 `localStorage`。
- 生产构建 `npm run build` 生成 `dist/`，运行模式固定为 `http`，不包含演示账号、演示居民数据或演示登录逻辑。
- API 必须按当前会话裁剪数据：居民只能读取和修改自己的允许字段；工作人员只能处理授权范围内的居民与事件。
- 前端负责展示与输入校验；身份、授权、状态机、revision、幂等、医学规则和审计由服务端负责。
- API 返回 JSON，不得把登录 HTML、代理错误页或站点首页作为 API 响应。

## 2. 部署与运行配置

生产 API 前缀默认为 `/api/v1`。静态站点与 API 必须同源，Nginx 示例把 `/api/` 转给服务端。跨域配置不是本项目的接入方式。

生产包不应启用运行时覆盖。仅在本地联调时，可在 `脚本/care-runtime-config.js` 之前加载一段部署配置：

```js
window.YIHE_RUNTIME_CONFIG_OVERRIDE = {
  mode: 'http',
  apiBaseUrl: '/api/v1',
  requestTimeoutMs: 15000,
  pollIntervalMs: 15000
};
```

不得在该对象或前端文件中放密码、密钥、数据库连接串或服务端令牌。配置只接受以 `/` 开头的同源路径；生产包只接受 `http` 模式。

## 3. 会话、Cookie 与 CSRF

所有写请求由浏览器携带同源 Cookie，并带 `X-CSRF-Token`。CSRF token 只保存在页面内存中，从会话响应获取。服务端必须验证 token 与请求来源，不能依赖前端的角色或隐藏控件。

| 请求 | 请求体 | 成功响应 |
|---|---|---|
| `GET /api/v1/session` | 无 | `200 { "user": null, "csrfToken": "..." }`；已登录时返回当前 `user` 与 token |
| `POST /api/v1/session` | `{ "role": "resident" 或 "staff", "username": "...", "password": "..." }` | `200 { "user": { ... }, "csrfToken": "..." }`，并设置会话 Cookie |
| `DELETE /api/v1/session` | 空 | `204`，撤销会话并清除 Cookie |

`user` 必须含非空 `name` 与 `role`。居民身份另含正整数 `residentId`；工作人员身份另含非空字符串 `staffId`。Cookie 应使用 `HttpOnly; Secure; SameSite=Lax` 或经安全评审认可的更严格策略，并设置适当的过期时间。登录成功后应轮换会话 ID 和 CSRF token；登出必须服务端撤销会话。

公开咨询使用匿名会话获取 CSRF token，也要验证 Origin、限速和输入；不能因为接口匿名就跳过反滥用措施。

## 4. 快照接口

`GET /api/v1/care/snapshot` 返回 `200 application/json`。可直接返回对象或使用 `{ "data": 对象 }` 包装。前端在渲染前验证主要结构；未通过校验会拒绝整份快照并锁定业务区。

根对象：

- `serverTime`：服务端生成快照时的带时区 ISO 8601 时间。前端用它加单调计时增量显示时限，不依赖用户系统时间；后台恢复后重新读取快照。真实超时、离线和用药时间窗仍由后端判定。
- `revision`：非负安全整数。任何会改变客户端可见快照的业务写入都必须递增。
- `residents`：以居民 ID 字符串为键的对象；对象中的 `id` 必须与键相同。
- `events`、`logs`、`staff`：数组，空集合返回 `[]`，不能返回 `null`。
- `staff[]`：`{ "id": string, "name": string }`；ID 唯一。必须包含当前工作人员，以及快照中被引用或可被指派的工作人员。

居民对象：

- 必需：`id` 正整数、`revision` 非负安全整数、非空 `name`、`age`（1–120 整数）、`responsible`（存在于 `staff`）、`risk` 布尔值、`deviceState`（`online` / `offline` / `unknown`）、`updatedAt`、`lastSeen`、`locationAt`、`pos:{x,y}`（有限数值）、`contacts[]`、`doses[]`、`trend[]`。
- 时间使用带时区的 ISO 8601。无效日期或不带时区的日期会被拒绝。
- 联系人形如 `{ "name": string, "phone": string }`。前端允许 0–3 位联系人。
- 用药任务形如 `{ "id": string, "name": string, "source": string, "dueAt": ISO8601, "confirmedAt"?: ISO8601 }`。任务 ID 在同一居民内唯一。
- 趋势项含 `at` 和可缺失的指标：`heartRate`、`bloodOxygen`、`systolic`、`diastolic`、`temperature`。数值缺失用 `null` 或省略；不能把未知值填成 0。
- `metricStates` 可选，支持 `heartRate`、`bloodOxygen`、`temperature`、`systolic`、`diastolic` 等键，值为 `normal` / `warning` / `danger` / `unknown`。缺少测量值或状态时，前端显示“数据不足”，不会推断为正常。
- `pos` 是 800×600 示意地图的相对坐标，不是经纬度，不可用于导航或真实定位。
- 页面还会读取可选展示字段 `place`、`chronic`、`medicalHistory`。缺失时显示“未记录”；这些字段不改变核心授权规则。

事件对象：

- 必需字段：`id`、`residentId`、`type`、`state`、`severity`、`detail`、`revision`、`owner`、`createdAt`、`dueAt`、`history[]`、`notification`。
- `type`：`health`、`medication`、`offline`、`fence`。
- `state`：`new`、`claimed`、`processing`、`resolved`、`false_alarm`、`escalated`。
- `severity`：`warning` 或 `danger`。`new` 状态的 `owner` 必须为 `null`；其他状态必须引用 `staff[].id`。
- `history[]` 项含 `{ "at": ISO8601, "action": string, "actor": string, "note": string }`。自由文本须按敏感健康信息保护。
- `notification` 必须含 `cycle` 正整数以及 `deliveredCycle`、`presentedCycle`、`acknowledgedCycle` 非负整数，三个水位均不得大于 cycle。`sourceRecoveredAt` 可选；信号恢复不自动关闭事件。
- 日志项含 `at`、`actor`、`action`、`residentId`，可含 `note`；引用的居民必须在当前快照中。

全量快照目前面向社区规模。服务端必须按当前会话裁剪并限制响应体大小；人数增长后应另行设计分页/查询接口，不能无限增大单次响应。

## 5. 写接口

除登录外，所有写接口需验证有效会话和 CSRF token。操作者从服务端会话确定；请求体不得信任浏览器提交的 `actor`、`staffId` 或角色。

| 操作 | 请求 | 成功 |
|---|---|---|
| 事件处置 | `POST /api/v1/events/{eventId}/transitions`，JSON `{ "action": string, "note": string, "revision": number, "targetStaffId"?: string }` | `204`；随后前端读取新快照 |
| 修改居民资料 | `PATCH /api/v1/residents/{residentId}`，允许字段 `name`、`age`、`contacts`、`contactUpdates`、`responsible`，并必须含 `expectedRevision` | `204`；递增该居民 `revision` 和根 `revision` |
| 新增用药任务 | `POST /api/v1/residents/{residentId}/doses`，JSON `{ "name": string, "dueAt": ISO8601 }`，并带 `Idempotency-Key` | `204`；随后前端读取新快照 |
| 确认服药 | `POST /api/v1/residents/{residentId}/doses/{doseId}/confirm`，JSON `{}` | `204`；重复确认幂等 |
| 确认提醒已展示 | `POST /api/v1/events/presented`，JSON `{ "events": [{ "id": string, "cycle": number }] }` | `204`；只确认请求中展示的轮次，不代表短信或推送已送达 |
| 提交公开咨询 | `POST /api/v1/inquiries`，JSON `{ "name": string, "phone": string, "type": string, "message": string }`，并带 `Idempotency-Key` | `204` |

### 事件状态规则

- `new` → `claim`：服务端将事件指派给当前工作人员。
- `claimed` 或 `escalated` → `processing`：仅当前负责人可提交。
- `processing` → `resolved`、`false_alarm` 或 `escalated`：仅当前负责人可提交；必须提供非空处置记录。
- 升级必须提供不同于当前操作者、且在授权范围内的 `targetStaffId`。服务端校验目标及操作者权限。
- 每次状态写入必须原子更新状态、owner（如适用）、事件 revision、history 和根 revision。`revision` 不匹配返回 `409`，不得留下半条日志或重复历史。
- 前端不会提交可信操作者身份。`targetStaffId` 是指派目标，不是操作者身份。

### 并发与幂等

- 居民 PATCH 使用居民级 `expectedRevision` 做比较并交换。版本不匹配返回 `409` 且不写入；成功后递增居民和根 revision。`contactUpdates: [{index: 0..2, value: {name, phone}}]` 只修改指定联系人槽位；整组 `contacts` 替换仍必须依赖版本检查。两个字段不得同时提交。
- 新增用药任务和咨询使用 `Idempotency-Key`。同一认证主体（匿名咨询使用匿名会话）及同一接口下，同键同请求体重放应返回原结果、不得重复建单；同键不同请求体应拒绝。请定义并记录服务端幂等记录的保留期限，覆盖实际重试窗口。
- 用药确认按任务 ID 幂等。事件 transition 用事件 revision 防止重复状态推进。提醒展示确认按事件 ID 与 cycle 幂等。
- 前端对写请求不自动重试。超时、断网、5xx 或无效响应代表结果未知；用户会先重新读取。服务端仍必须正确实现幂等，因为响应可能在写入后丢失。

## 6. 错误响应

前端按 HTTP 状态处理，不依赖服务端错误正文：

| 状态 | 前端含义 | 服务端使用场景 |
|---|---|---|
| `400` / `422` | 输入无效 | 字段缺失、格式或业务校验不通过 |
| `401` | 会话失效 | 未登录、会话过期；不得重定向到 HTML |
| `403` | 无权访问 | 身份有效但无目标资源/操作权限 |
| `409` | 版本或幂等冲突 | expected revision 不匹配、事件 revision 冲突或幂等键与请求体不一致 |
| `429` | 请求过频 | 登录、咨询及其他需要限速的路径 |
| `5xx` | 服务暂不可用 | 服务端故障；不得暴露堆栈、SQL、内部路径或健康记录 |

读取成功的 JSON 响应必须包含 `Content-Type: application/json`。不符合预期的 JSON、HTML 错误页、缺字段快照将作为 `INVALID_RESPONSE` 处理。前端请求超时默认为 15 秒，轮询间隔默认为 15 秒；页面隐藏或离线时暂停轮询。

## 7. 安全与隐私要求

- 仅 HTTPS；HTTP 站点强制跳转到 HTTPS，并在 HTTPS 响应发送 HSTS。示例见 `部署/headers` 和 `部署/nginx.conf.example`。
- 会话 Cookie 使用 HttpOnly、Secure 与合适的 SameSite；CSRF 同时校验 token 和 Origin。登录、咨询和写接口实施服务端限速。
- 服务端对每个请求重新执行对象级与字段级授权。居民 ID、staff ID、隐藏按钮和前端路由都不能作为授权证明。
- 校验并限制字符串长度、数组大小、时间范围、分页/快照体积；输出日志时避免写入口令、Cookie、CSRF token 和不必要的健康信息。
- 保留 CSP、`X-Content-Type-Options: nosniff`、`Referrer-Policy`、禁止嵌入等发布响应头。HTML 404 与 `/api/v1/` 路由必须分开，API 404 返回 JSON。
- 事件导出和自由文本可能包含个人信息；由机构确定访问、保存、下载、删除和审计流程。姓名打码只作用于姓名字段，不是去标识化保证。

## 8. 联调顺序与验收

1. 部署同源 HTTPS 静态包和 `/api/v1/` 代理；确认 API 404 不回退到 HTML。
2. 接入 `GET /session`、登录、角色隔离、会话过期、登出和 CSRF/Origin 校验。
3. 用 `测试/fixtures/http-snapshot.json` 对齐快照字段、居民级 revision、提醒轮次和空集合行为；该文件仅为合成测试数据。
4. 接入快照授权裁剪、居民 PATCH 冲突、事件状态机、用药确认/新增、提醒展示确认与公开咨询。
5. 逐项验证：断网与超时、写入后响应丢失、重复幂等键、并发修改、401/403/409/422/429/5xx、坏 JSON、未授权居民 ID、登录后刷新及登出后回退。
6. 真实数据上线前由业务和专业人员确认医学阈值、设备心跳、提醒升级、通知投递回执、用药规则及数据保留政策；当前前端不会生成医学诊断，也没有短信/推送送达能力。

## 9. 交付清单

- 前端源代码和 HTTP-only 发布包：`dist/`（通过 `npm run build` 生成）。
- HTTP API 合约与联调验收：本文。
- 部署响应头与 Nginx 示例：`部署/headers`、`部署/nginx.conf.example`；部署方须填入实际域名、证书、静态根目录和 API upstream。
- 合成快照：`测试/fixtures/http-snapshot.json`，不得作为生产数据。
- 当前未交付：真实后端、真实账号、数据库迁移、生产域名/证书、设备连接、医学规则审批、通知服务及生产数据验收。


## 10. 持续信号与提醒语义

- 信号仍 active 时结案不能停止检测。生成新的 incident，记录 previousIncidentId，保留旧事件历史；设备离线不能证明健康异常已恢复。
- deliveredCycle 仅表示页面通道可读取；presentedCycle 表示实际展示；acknowledgedCycle 表示工作人员主动接单。showModal 不得写 acknowledgedCycle。每次新的告警 cycle 都需要新的确认。
- 页面展示回执必须携带实际展示的 cycle。旧 cycle 遇到新 cycle 返回 409，整批不写入；同 cycle 重放幂等。
- 提醒作用域：所有有权查看事件的页面独立展示；presentedCycle 是审计水位，不是全局排他锁。页面内 seen 只对本次页面访问的 eventId + cycle 去重；其他页面的展示不能抑制本页面。acknowledgedCycle 表示当前轮次已由工作人员主动接单，允许所有页面停止重复提示。每次恶化 cycle 增长后再次提示。
- 刷新后尚未 acknowledged 的事件需要重新提示。
- Demo 居民 revision 从 0 开始；旧格式缺失 revision 时补 0。保存居民资料递增居民版本；采样与提醒展示不改变资料编辑版本。


## 11. 会话恢复与空闲

- 401 时锁定业务区，草稿只留在当前页面内存，不刷新、不写入浏览器持久存储。原账号登录后恢复表单；不同账号丢弃原草稿，重新读取授权数据。原表单 revision 不因重新登录而自动推进，避免把旧草稿当成新版本保存。
- HTTP 工作人员页面 15 分钟无键盘/指针交互即锁定并请求 DELETE /session；退出结果未知必须明确提示。前端空闲锁定不能代替后端会话 TTL 与吊销。
- 登录返回角色与入口不符时，撤销该服务端会话并清空内存身份和 token。吊销请求失败时报告“退出结果未确认”，不得显示成功退出。
- 离线前危险采样保留 severity，同时标明当前状态未知；后端需返回最后有效采样的 metricStates，不能因离线把历史危险标为 normal。
