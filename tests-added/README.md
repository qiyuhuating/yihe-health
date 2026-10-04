# v0.3.2 测试

源码包保留可直接运行的测试；同批测试另有独立编号 ZIP，便于归档。所有命令在源码包根目录执行。

| 文件 | 检查 | 运行 |
|---|---|---|
| backend/tests/test_v020.py | 原有 9 个回归：会话、升级提醒、隐私、限流、快照规模、用药恢复 | `python tools/run_unit_tests.py` |
| backend/tests/test_v022.py | 40 个核心业务与安全测试、逐字段居民隐私契约、并发修改和连接生命周期 | 同上 |
| backend/tests/test_v032.py | 5 个读写事务、WAL 并发、纯 snapshot 与登录限流事务契约 | 同上，共 54 个测试 |
| tools/run_unit_tests.py | ResourceWarning 严格检查、Python 3.12 SQLite 连接跟踪、析构错误检查 | 同上 |
| e2e_privacy.py | 真实后端、SQLite、原 HTTP Adapter 和居民页隐私/资料/用药联调 | `python tests-added/e2e_privacy.py` |
| e2e_real_backend.py | 原浏览器回归：锁屏、多标签页、升级提醒、一键接单和居民重登录 | `python tests-added/e2e_real_backend.py` |
| qa_layout.py | v0.2.1 居民布局巡检脚本，作为参考保留 | `python tests-added/qa_layout.py` |

浏览器测试需要 `python -m pip install -r requirements-test.txt`、`python -m playwright install chromium`，并先构建 `python docs/工具/build_frontend.py`。统一核验可运行 `python tools/check_release.py --e2e`。GitHub Actions 会执行基础发布门禁和 Chromium E2E；布局/样式专项脚本仍可独立运行。

测试全部使用临时合成居民与账号，不连接生产服务。覆盖率建议使用 `--source=backend` 覆盖后端全部模块（含 CLI 与后台启动），无删除难测行或覆盖率排除项。

## v0.3.0 更新
- `qa_layout.py` 重写：居民端 / 登录 / 工作人员端 / 官网 × 6 种屏宽 × 紧凑·标准·特大三档字号，共 343 项检查（横向溢出、触控目标 ≥44px、桌面侧栏不遮挡内容、首页两列结构、字号控件与持久化、密度联动、工作人员端侧栏铺满视口）。
- 运行：`python docs/工具/build_frontend.py && python tests-added/qa_layout.py`（失败时退出码非 0）。
