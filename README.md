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

- **数据**：全部为内置模拟数据（`data/` 目录），刷新即重置。
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

## 部署

推送到 `main` 分支后 GitHub Pages 自动重新构建。
站点根目录为仓库根（`/`），已放置 `.nojekyll` 跳过 Jekyll 处理。
