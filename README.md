# 颐和 · 智慧康养（静态演示版）

社区智慧养老服务平台的前端演示站点。纯 HTML / CSS / JavaScript，**不含后端与数据库**。

在线访问：<https://qiyuhuating.github.io/yihe-health/>

## 页面一览

| 文件 | 说明 |
|---|---|
| `index.html` | 首页 |
| `个人端.html` | 个人端：健康指标、提醒、AI 陪伴对话 |
| `管理端.html` | 管理端：老人档案、告警、数据看板 |
| `login.html` | 登录页（演示用） |
| `services.html` / `guide.html` / `news.html` / `about.html` / `contact.html` | 服务、指南、资讯、关于、联系 |

## 技术说明

- **数据**：健康指标来自内置模拟数据（`data/`），模拟状态和用户输入保存在当前浏览器，部分数据刷新后仍保留。清理网站数据会删除本机记录。
- **AI 对话**：需在界面设置中填入使用者自己的 DeepSeek API Key。Key 通过 WebCrypto
  以 AES-GCM 加密后存于浏览器 IndexedDB，直接请求 `api.deepseek.com`，不经过第三方服务器。
  不填 Key 时自动降级为本地规则回复。
- **实时推送**：静态托管下 WebSocket 不可用，已自动降级为轮询模式。
- **无后端**：登录、数据写入等均为前端模拟，不具备真实鉴权能力，请勿用于生产。

## 本地预览

```bash
python -m http.server 8000
# 打开 http://localhost:8000
```

直接双击 `index.html` 也能看，但部分浏览器会限制 `file://` 下的模块加载，建议起本地服务。

## 前端检查与交付

```bash
python -m pip install -r requirements-test.txt
python -m playwright install chromium
python tests/check_static.py
python -m unittest discover -s tests -v
```

语法检查需要 Node.js。浏览器回归使用独立的本地服务与临时浏览器，不操作线上数据。
GitHub Actions 会检查 main 推送和 PR。接口字段、失败状态及验收方式见 [FRONTEND-HANDOFF.md](FRONTEND-HANDOFF.md)。本机存储与可选 AI 的数据说明见 [privacy.html](privacy.html)。

## 部署

推送到 `main` 分支后 GitHub Pages 自动重新构建。
站点根目录为仓库根（`/`），已放置 `.nojekyll` 跳过 Jekyll 处理。
