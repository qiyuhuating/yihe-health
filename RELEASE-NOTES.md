# v0.3.2 · Maintenance & Reliability

v0.3.2 以 v0.3.1 为基线，只做内部维护、事务语义修正、源码可维护性和验证体系增强，不新增业务页面或医疗功能。

## 已完成

1. **GET 纯读取**：快照与审计 GET 进入 SQLite `query_only` 只读事务，不再 `BEGIN IMMEDIATE`。
2. **唯一时间驱动入口**：`snapshot()` 不再执行 `tick()`；后台 worker 启动立即扫描，之后每 5 秒扫描。
3. **事务所有权明确**：写事务统一由 transaction context 提交/回滚；登录失败限流不再在业务函数中途 `commit()`。
4. **后端模块化**：认证/session/rate-limit → `auth.py`；照护领域与持久化 → `care.py`；公共验证 → `common.py`；WSGI/routing → `service.py`。
5. **CSS 单一源**：公共样式片段位于 `docs/样式源/`；`site/login/resident/staff.css` 由 `build_styles.py` 生成，发布仍为每页一个 CSS 包。
6. **CI**：GitHub Actions 自动执行发布门禁，并在独立 job 安装 Chromium 运行真实后端 E2E。
7. **新增契约测试**：验证读事务不可写、snapshot 无副作用、snapshot HTTP 路径不打开写事务、登录失败限流可持久化。

## 本地验证基线

- 后端：54 / 54，0 skip。
- SQLite：严格 runner 检查连接生命周期，0 泄漏。
- Python：3.13 可运行严格 ResourceWarning 门禁。
- 发布：CSP、HTTP-only 构建、JS 语法、敏感文件扫描、前端数据契约与 manifest 均纳入 `tools/check_release.py`。

## 未改变

- URL 与前端 HTTP 契约。
- 居民隐私 serializer 的公开字段边界。
- 风险事件状态机与工作人员权限规则。
- SQLite schema。
- 短信仍为 disabled；没有虚构发送成功。

## 后续重点

v0.3.2 之后优先做真实通知 Outbox/Worker、备份恢复、迁移机制、结构化日志与部署能力；不优先增加页面或 AI 诊断功能。
