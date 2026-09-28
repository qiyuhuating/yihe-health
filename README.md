# 颐和 · 社区/居家健康监护前端

目标：老人少操作，系统发现异常，工作人员及时处理。当前为纯静态前端模拟交付，没有真实数据库、设备接入、短信或服务端权限。

## 使用

在仓库目录执行 `python -m http.server 8000`，打开 http://localhost:8000/home.html 。请使用 localhost 或 HTTPS，事件写入需要浏览器 Web Locks，不能直接双击文件使用。

- 居民：`login.html`，姓名“王国栋”，演示口令 `123456`。
- 工作人员：`管理端.html`，演示账户 `admin` / `admin123`。
- 居民端：首页 / 健康 / 我的。
- 管理端：工作台 / 预警 / 居民 / 地图 / 更多。
- 更多中的异常模拟器可触发健康异常、漏服药、设备离线、越界等场景。地图为安全处置示意图，不是真实定位地图。

## 验证

安装 Node 18+、Python 和 `requirements-test.txt`，执行 `python -m playwright install chromium`。

```text
npm run check
npm run test:domain
python -m unittest discover -s tests -v
```

Windows 可通过 `powershell -File tools/setup_test_env.ps1 -AllBrowsers` 配置独立环境，再运行 `powershell -File tools/run_tests.ps1 -Browser all`。测试使用 requirements-test.txt 指定的 Playwright 及其匹配浏览器，不复用其他版本的可执行文件。安装器将浏览器保存在已忽略的 `.pw-browsers/` 中，依赖从官方 PyPI 安装。

只测单个浏览器可使用 `-Browser chromium`（默认）、`-Browser firefox` 或 `-Browser webkit`。跨平台的 Python 入口读取环境变量 `YIHE_BROWSER`；自有浏览器可由 `YIHE_BROWSER_PATH` 显式指定。截图目录由 `YIHE_ARTIFACT_DIR` 指定。GitHub Actions 在 main/release 推送与 PR 时分别执行三个浏览器引擎，并保存截图。

当前测试与旧产品测试的范围区别见 `tests/legacy/README.md`。实际结果见 `COMMUNITY-PLAN.md`。代码未在本轮推送，线上页面不代表此工作树。

## 数据边界

所有居民、指标和负责人均为演示数据。页面提醒不代表短信送达；关闭全部页面后不会继续自动检测。姓名字段打码不会清除自由文本中的个人信息。演示阈值不是诊断标准。浏览器登录、员工切换、本机日志均不能替代真实身份鉴权或服务端审计。

本机记录保存在 `yihe-community-v1`；主题使用 `yihe-theme`。清除网站数据会丢失本机记录。旧键保留，不自动删除。详情见 `ARCHITECTURE.md` 和 `FRONTEND-HANDOFF.md`。

数据损坏时会暂停相关操作、保留原记录并显示“重新读取”；请由维护人员恢复有效记录后重试，不要用清空浏览器数据来代替修复。缺失或非数值采样显示“数据不足”，不能作为健康正常或信号恢复的依据。居民端的急救拨号入口在读取失败时仍可使用。
