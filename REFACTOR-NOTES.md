# v0.3.2 维护整理说明

## 后端

v0.3.1 的 `backend/service.py` 同时承担通用校验、领域存储、认证与 HTTP。v0.3.2 在不改变 API 的前提下拆为：

| 文件 | 职责 |
|---|---|
| `common.py` | 时间、JSON、校验、Problem、公共常量 |
| `care.py` | SQLite 照护领域存储、信号、居民、用药、快照与 outbox 持久化 |
| `incidents.py` | 纯事件状态机与合法转换规则 |
| `notifications.py` | 通知通道能力声明；当前页面可用、短信显式 disabled |
| `auth.py` | Session、用户解析、登录限流、代理地址解析 |
| `serializers.py` | 居民公开 DTO / 隐私白名单 |
| `service.py` | WSGI、HTTP routing、设备入口、静态文件、CLI/worker |

`service.py` 从约 977 行下降到约 356 行；最大业务类不超过约 500 行。重点不是行数，而是 HTTP、认证与照护领域之间不再互相混放。

## 事务语义

- `read_transaction()`：`PRAGMA query_only=ON` + `BEGIN`，退出时 rollback，只用于纯读 API。
- `write_transaction()`：`BEGIN IMMEDIATE`，统一提交/回滚。
- `transaction` 保留为 write alias，兼容旧测试/内部脚本。
- `snapshot()` 不再驱动业务状态改变。
- worker 是生产环境时间扫描的唯一入口。

## CSS

四个页面 CSS 包包含大量相同来源段。现在把可编辑片段集中到 `docs/样式源/`，由 `docs/工具/build_styles.py` 生成四个单文件 bundle。这样同时获得：

- 源码公共规则只维护一次；
- 本地页面与生产页面仍只引用一个 CSS 文件；
- `build_frontend.py` 会先检查 bundle 与源片段一致，防止直接修改生成文件。

## 验证

新增 `backend/tests/test_v032.py`，覆盖 5 个纯读事务、WAL 读写并发、snapshot 副作用与登录失败事务边界契约。所有旧业务/隐私/并发/幂等测试继续执行。
