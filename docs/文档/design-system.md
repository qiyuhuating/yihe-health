# 设计体系 v2（xz-system）· v0.3.0

目标：**老少咸宜**。同一套页面，长者可以放大、年轻人可以收紧，不需要两套界面。

## 样式文件（v0.3.2）
每个页面只加载一个样式包，互不叠加：

| 样式包 | 页面 |
|---|---|
| `site.css` | 官网 home / about / services / contact / guide / privacy |
| `login.css` | 居民登录页 |
| `resident.css` | 居民端首页 |
| `staff.css` | 工作人员端 |

可编辑源位于 `docs/样式源/`，公共 reconstruction / ui-polish / xinzhongshi / xz-system 只维护一份；`python docs/工具/build_styles.py` 生成上表四个 bundle。**不要直接编辑生成的 `docs/样式/*.css`。**

修改样式后依次运行 `python docs/工具/build_styles.py`、`python tests-added/qa_style_snapshot.py --record/--compare`；`python docs/工具/css_audit.py` 会拦截死声明、重复规则和重复 keyframes，`build_frontend.py` 还会校验生成 bundle 与样式源一致。

## 字号四档与密度
| 档位 | 倍率 | 面向 | 密度 `data-density` |
|---|---:|---|---|
| 紧凑 | 0.9 | 习惯小字号、想一屏看到更多的年轻用户（居民端正文约 16px） | compact（内边距 ×0.8） |
| 标准 | 1 | 默认 | normal |
| 大 | 1.15 | 长者 | roomy（×1.1） |
| 特大 | 1.3 | 长者 / 远距离阅读 | roomy |

- 偏好保存在 `localStorage['yihe-text-scale']`，居民端、工作人员端、官网共用，多标签页通过 `storage` 事件同步。
- 设置入口：居民端顶栏、工作人员端顶栏与"更多"、官网页头 A− / A＋（在四档之间逐级切换）。
- 居民端字号 = `calc(Npx * var(--text-scale))`；官网用 `html{font-size:max(15px, 16px*scale)}`；工作人员端用 `zoom`，并按倍率校正 `100vh`。
- **触控目标始终 ≥ 44px**，不随"紧凑"缩小（`tests-added/qa_layout.py` 校验）。

## 令牌
间距 `--xz-s1…s7`（4/8/12/16/24/32/48）· 圆角 `--xz-r-sm/md/lg`（6/10/16）· 阴影 `--xz-shadow-1/2` · 缓动 `--xz-ease` · 密度 `--xz-dens`。
颜色沿用传统色：`--xz-accent` 黛青、`--xz-seal` 朱砂、`--xz-gold` 鎏金、`--xz-paper` 宣纸、`--xz-ink` 墨；深色模式在 `:root[data-theme="dark"]` 覆盖。

## 居民端结构
- 窄屏（<1024px）：`.xz-home` 单列，`order` 保证 **快捷拨打 → 状态 → 用药与提醒 → 紧急联系**；底部导航固定。
- 宽屏（≥1024px）：`.xz-home` 两列 `主栏(.xz-main) | 侧栏(.xz-aside)`，侧栏吸顶；底栏变为左侧导航栏（印章 + 三个入口），页面左边距让出 `--rail`。
- 状态用"颜色 + 符号 + 文字"三重表达：✓ 正常 · ▲ 关注 · ! 紧急 · ? 数据不足。

## 约定
- 新组件只用令牌，不写死像素间距；需要随字号缩放的尺寸用 `calc(Npx * var(--text-scale, 1))`。
- 改动页面结构后运行 `python tools/check_release.py --e2e` 与 `python tests-added/qa_layout.py`。
