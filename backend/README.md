# 最小后端

Python 3.12 标准库 + SQLite，单社区、单实例部署。无预置账号，无默认密码，无自动导入演示居民。接口与现有 HTTP Adapter 同源，数据库和设备密钥不得进入发布目录或 git。

## 本地运行

先在仓库根目录构建网页：`python docs/工具/build_frontend.py`。完整安装流程见根目录 `README.md`。

```sh
python backend/service.py --db /tmp/yihe-care.sqlite3 init
python backend/service.py --db /tmp/yihe-care.sqlite3 add-user staff-account staff staff-1 工作人员
```

命令交互输入至少 12 字符的密码，不打印或保存明文。准备居民 JSON，例如：

```json
{"id":1,"name":"合成居民","age":70,"responsible":"staff-1"}
```

```sh
python backend/service.py --db /tmp/yihe-care.sqlite3 add-resident resident.json
python backend/service.py --db /tmp/yihe-care.sqlite3 add-user resident-account resident 1 居民账户
python backend/service.py --db /tmp/yihe-care.sqlite3 serve --port 8000 --origin http://127.0.0.1:8000 --insecure-local
```

打开 `http://127.0.0.1:8000/管理端.html` 或 `/login.html`，使用刚创建的账号。姓名改动不影响 username / residentId。

## 设备模拟

启动服务前通过环境变量设置独立的 `YIHE_DEVICE_KEY`；未设置时设备接口禁用。模拟器请求携带 `Authorization: Bearer <密钥>` 和唯一 `Idempotency-Key`，不把设备密钥放进网页。

`POST /api/v1/device/events`：

```json
{"residentId":1,"type":"health","source":"simulator","active":true,"severity":"danger","detail":"合成危险信号","metrics":{"bloodOxygen":80},"metricStates":{"bloodOxygen":"danger"}}
```

恢复时发送同一 residentId/type/source，`active:false`，并使用新的幂等键。上游负责提供经过审核的 severity 和 metricStates；本服务不会根据未经审核的医学阈值诊断。`type:heartbeat` 只更新采样与在线状态。5 秒后台扫描负责心跳失联与用药逾期检测，不依赖网页轮询；当前 5 分钟离线、30 分钟逾期仍须按机构规则确认。

## 状态与持久化

- SQLite 事务持有版本比较、状态更新、历史、审计与通知 outbox；并发接单或旧资料提交只允许一个成功。
- Signal 与 Incident 分开。持续信号结案立即创建关联旧事件的新事件；信号恢复不自动结案，恢复后再异常增加 notification cycle。
- delivered / presented / acknowledged 分离；展示回执核对实际 cycle，旧回执整批回滚。页面展示不等于接单或短信送达。
- NotificationService 已提供接口，当前 Page 状态为 available，SMS 为 disabled；没有发送短信。正式供应商接入需增加可靠 outbox 消费、重试与供应商回执。
- staff 会话默认 12 小时（`--staff-session-hours`，允许 0.25–24）、resident 会话 24 小时，Cookie HttpOnly + SameSite=Lax，生产额外 Secure。值班台需要持续接收预警，因此不再用 15 分钟固定 TTL；无人操作的保护由前端“锁屏”承担：15 分钟无操作隐藏内容、需重新输入密码，会话与轮询保持，预警仍会在锁屏上提示（不含居民信息）。固定 TTL 不因轮询续期，到期后需原页重新登录，草稿仍由前端内存保留。
- 反向代理后请加 `--trusted-proxy`：登录限流按 `X-Forwarded-For` 最后一跳（由本机代理追加）区分客户端；不加该参数时使用 `REMOTE_ADDR`，此时所有经代理的访客共享同一个限流桶，任何人连续输错 5 次都会锁住该账号 60 秒。
- 升级（escalated）会开启新的提醒轮次，接收人的页面会弹出提醒；开始处理即视为确认该轮次。
- v0.3.2 居民快照继续由 `serializers.py` 的嵌套白名单生成：固定公开照护团队取代真实 staff、responsible 与非空 owner，历史 actor 使用通用标签，处置 note 为空，用药 actor / 内部来源及审计 logs 不公开。字段契约见 `docs/文档/resident-privacy-contract.md`。
- 操作者来自会话。单社区工作人员可访问本社区所有居民；居民仅访问自身数据和允许字段。多机构隔离、细粒度工作人员范围不在当前版本范围内。
- 密码使用 scrypt 与随机盐。登录失败 5 次后限制 60 秒；生产入口还应对匿名会话、设备与咨询设置网关速率和连接限制。
- 用药和设备请求持久化幂等键；写成功后响应丢失可用同一键读取原结果，改变内容返回 409。
- 快照只带最近 200 个归档事件、每事件最近 100 条历史、500 条日志、200 条已完成用药记录；开放事件与未完成任务保留。数据库保存完整事件、历史和审计，`GET /audit?after=0&limit=100` 提供工作人员分页读取。未完成任务上限为每居民 500。

## 验证与部署

```sh
python tools/run_unit_tests.py
```

本包提供 Chromium 的真实 HTTP/SQLite 回归与居民隐私联调；安装测试依赖后可执行 `python tools/check_release.py --e2e`。GitHub Actions 已配置基础发布门禁和 Chromium E2E；分支保护仍需在仓库设置中启用。

v0.3.2 已分离读写事务：快照与审计 GET 使用 `PRAGMA query_only=ON` 的只读事务，不再取得写锁；`snapshot()` 不再调用 `tick()`。后台 worker 启动即扫描并每 5 秒继续，是生产运行时时间驱动状态变化的唯一入口。

生产通过 TLS 反向代理访问该服务；进程只监听 loopback，`--origin` 必须与对外 HTTPS 源一致，禁用 `--insecure-local`。WSGI 开发服务器提供可运行基线，正式运营前应替换生产 WSGI 容器、执行容量测试、配置备份恢复、密钥管理及机构数据保留政策。当前不提供多租户、医疗阈值审批、短信供应商、生产运维部署或无限规模承诺。
