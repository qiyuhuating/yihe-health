# 颐和健康 v0.3.2

社区照护与风险事件原型：居民资料、设备信号、风险事件、用药任务、工作人员处置、审计与页面提醒。系统不提供医疗诊断；短信通道仍明确为 `disabled`。v0.3.2 的重点是**维护与可靠性**，不扩张业务功能。

## v0.3.2 关键变化

- `GET /api/v1/care/snapshot` 与 `GET /api/v1/audit` 使用 SQLite 强制只读事务（`PRAGMA query_only=ON` + `BEGIN`），不再取得写锁。
- `snapshot()` 不再调用 `tick()`；时间驱动状态只由后台 worker 处理，worker 启动后立即扫描并每 5 秒继续扫描。
- 写事务仍使用 `BEGIN IMMEDIATE`；登录失败限流不再由业务函数手工 `commit()`。
- 后端按职责拆成 `common.py`、`care.py`、`incidents.py`、`notifications.py`、`auth.py`、`serializers.py` 与薄 WSGI `service.py`。
- CSS 的可编辑源移动到 `docs/样式源/`，四个页面单 CSS 包由脚本生成；发布运行时仍保持每页一个 CSS 请求。
- 新增 GitHub Actions：基础发布核验 + Chromium 真实后端 E2E。
- 新增 v0.3.2 事务/纯读契约测试；严格测试当前为 54 项。

## Development

需要 Python 3.12+；发布核验还需要 Node.js 18+。在项目根目录：

```sh
python docs/工具/build_styles.py
python docs/工具/build_frontend.py
python backend/service.py --db care.sqlite3 init
python backend/service.py --db care.sqlite3 add-user staff-account staff staff-1 工作人员
python backend/service.py --db care.sqlite3 add-resident examples/resident.json
python backend/service.py --db care.sqlite3 add-user resident-account resident 1 居民账户
python backend/service.py --db care.sqlite3 serve --port 8000 --origin http://127.0.0.1:8000 --insecure-local --static docs/dist
```

两次 `add-user` 会交互要求至少 12 字符密码。数据库和真实密钥不要加入源码或发布 ZIP。

## Validation

快速且完整的本地发布门禁：

```sh
python tools/check_release.py
```

它会验证源码 SHA-256 manifest、54 项严格后端测试、SQLite 连接归还、CSS 源/生成包一致性、CSP、HTTP-only 构建、JavaScript 语法、敏感文件扫描和前端数据契约。

浏览器联调：

```sh
python -m pip install -r requirements-test.txt
python -m playwright install chromium
python tools/check_release.py --e2e

# 若受限环境已有系统 Chromium，也可：
# YIHE_CHROMIUM_EXECUTABLE=/usr/bin/chromium python tools/check_release.py --e2e
```

后端覆盖率示例：

```sh
python -m coverage run --branch --include="backend/*.py" tools/run_unit_tests.py
python -m coverage report -m
```

## Architecture

```text
HTTP / WSGI  backend/service.py
        │
        ├── Auth/session/rate-limit  backend/auth.py
        ├── Care persistence         backend/care.py
        ├── Incident state machine   backend/incidents.py
        ├── Notification capability  backend/notifications.py
        ├── Resident public DTO      backend/serializers.py
        └── Shared validation/time   backend/common.py
```

读路径与写路径分离：

```text
snapshot / audit GET → read_transaction → query_only → no business mutation
other API writes      → write_transaction → BEGIN IMMEDIATE → atomic commit
background worker     → write_transaction → tick → time-driven state changes
```

`docs/` 保留可离线演示源码；`docs/dist/` 是 HTTP-only 发布产物。生产构建排除演示凭据和演示居民数据。

## Security and privacy

- Resident / Staff 在后端真正做授权检查，不依赖前端隐藏按钮。
- 居民快照通过 `serializers.py` 嵌套 allow-list 生成，真实工作人员身份、内部备注、审计信息不会进入居民响应。
- HttpOnly Session Cookie；写请求验证 Origin + CSRF；生产要求 Secure Cookie。
- 设备事件使用独立 Bearer key，并支持幂等键。
- SQLite 连接由严格测试追踪，当前 0 泄漏。

## Current limits

这是单社区可运行原型，不宣称生产医疗系统。当前仍使用 SQLite + stdlib WSGI，短信 provider 未接入；正式运营还需要生产 WSGI 容器、备份恢复、容量测试、监控告警和实际短信供应商。

更多细节见 `backend/README.md`、`docs/文档/architecture.md`、`docs/文档/resident-privacy-contract.md` 和 `REFACTOR-NOTES.md`。
