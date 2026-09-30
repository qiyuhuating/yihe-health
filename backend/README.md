# 最小后端

Python 3.12 标准库 + SQLite，单社区、单实例部署。无预置账号，无默认密码，无自动导入演示居民。接口与现有 HTTP Adapter 同源，数据库和设备密钥不得进入发布目录或 git。

## 本地运行

先在仓库根目录构建网页：`npm --prefix docs run build`。

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
- staff 会话固定 15 分钟、resident 会话固定 24 小时，Cookie HttpOnly + SameSite=Lax，生产额外 Secure。固定 TTL 不因轮询续期；工作人员需原页重新登录，草稿仍由前端内存保留。
- 操作者来自会话。单社区工作人员可访问本社区所有居民；居民仅访问自身数据和允许字段。多机构隔离、细粒度工作人员范围不在当前版本范围内。
- 密码使用 scrypt 与随机盐。登录失败 5 次后限制 60 秒；生产入口还应对匿名会话、设备与咨询设置网关速率和连接限制。
- 用药和设备请求持久化幂等键；写成功后响应丢失可用同一键读取原结果，改变内容返回 409。
- 快照只带最近 200 个归档事件、每事件最近 100 条历史、500 条日志、200 条已完成用药记录；开放事件与未完成任务保留。数据库保存完整事件、历史和审计，`GET /audit?after=0&limit=100` 提供工作人员分页读取。未完成任务上限为每居民 500。

## 验证与部署

```sh
python -m unittest discover -s backend/tests -v
```

CI 另有三浏览器的真实 HTTP/SQLite 端到端测试和 Demo/HTTP 共享场景，接口测试不以模拟返回替代服务端行为。

生产通过 TLS 反向代理访问该服务；进程只监听 loopback，`--origin` 必须与对外 HTTPS 源一致，禁用 `--insecure-local`。WSGI 开发服务器提供可运行基线，正式运营前应替换生产 WSGI 容器、执行容量测试、配置备份恢复、密钥管理及机构数据保留政策。当前不提供多租户、医疗阈值审批、短信供应商、生产运维部署或无限规模承诺。
