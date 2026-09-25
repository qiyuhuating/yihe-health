# 颐和·智慧康养 · 缺陷修复报告

**日期**：2026-09-25
**仓库**：https://github.com/qiyuhuating/yihe-health
**修复基线**：`c6e6d47` → 当前工作区
**验证状态**：自带测试 **16/16 通过** ｜ 静态检查 **PASS（45 JS / 13 HTML）** ｜ 运行时 **13 页 console 错误全为 0**

---

## 0. 修复总览

| 编号 | 缺陷 | 级别 | 状态 | 关键指标变化 |
|---|---|---|---|---|
| P1-1 | 管理端 13 个头像 404 | P1 | ✅ **已修** | 404：13 → **0**；资源：39 → 27 个 |
| P1-2 | 首页 27 个脚本同步阻塞 | P1 | ✅ **已修** | **84 个脚本全部 defer** |
| P1-3 | 管理端表格窄屏溢出 | P1 | ⚪ **误判撤销** | 见 §3.1 |
| P1-4 | 兼容性回退缺失 | P1 | ✅ **已修** | color-mix **23 组静态回退**；backdrop 前缀 **11 处** |
| P1-5 | CSP 只覆盖部分页面 | P1 | ✅ **已修** | CSP 覆盖：2 → **13/13** |
| P2-1 | 表单缺可访问名称 | P2 | ✅ **已修** | 无标签控件：7 → **1** |
| P2-2 | backdrop 前缀缺失 | P2 | ✅ **已修** | 11 处补齐 |
| P2-3 | 无构建流程 | P2 | ⏸ 未做（结构性） | 见 §4 |
| P2-4 | 页面缺 h1 | P2 | ✅ **已修** | 见 §3.2（部分误判） |
| P2-5 | landmark 覆盖不足 | P2 | ⚪ **误判撤销** | 见 §3.3 |
| P2-6 | 深色模式不生效 | P2 | ✅ **已修** | 4 页 dark/light 全部正确 |
| P2-7 | 图片懒加载不足 | P2 | ✅ **已修** | 新增 **68 个** `loading="lazy"` |
| P2-8 | 遗留调试日志 | P2 | ✅ **已修** | 删除 3 条（含误导性日志） |
| P3-1 | CSP 放行未用域名 | P3 | ✅ **已修** | 移除 3 个字体外域 |
| P3-2 | viewport 写法不一 | P3 | ✅ **已修** | 13 页统一 |
| P3-3 | 未跟踪文件未忽略 | P3 | ✅ **已修** | `.gitignore` 补 9 条 |
| P3-4 | 演示密码硬编码 | P3 | ⏸ 保留（见 §4） | — |

**统计**：修复 **13 项** ｜ 撤销误判 **3 项** ｜ 保留待议 **2 项**

---

## 1. P1 级修复（逐条：成因 / 影响 / 方案 / 改动）

### P1-1 管理端 13 个头像 404

**成因（根因在代码，不在网络）**
`js/storage.js` 的 `AvatarStore.load()` 实现为：先用 `new Image()` 无条件探测 `avatars/<id>.jpg`，失败后才回落查 IndexedDB。
但 **`avatars/` 目录在仓库中并不存在** —— 该分支每次必然失败。更糟的是 `storage.js` 里还留有日志「头像已保存到 avatars/ 文件夹」，而**静态网页根本无法写入本地文件夹**，这是对用户的虚假承诺。

**影响范围**
- 管理端（概览/档案/账户）每次加载产生 **13 个 404 请求**，控制台持续报错
- 用户可能被日志误导，去找一个永远不存在的文件夹

**修复方案与改动**
1. `js/storage.js` — 给 `load(e, forceFileProbe)` 增加开关：**默认跳过文件探测、直接查 IndexedDB**；仅当调用方显式传入 `forceFileProbe` 时才探测静态目录。并在源码中写明原因（防止后人改回去）。
2. 删除 `storage.js` 中那条误导性 `console.log`。

**验证**
```
修复前：管理端 总请求 40 个，失败 13 个（/avatars/1.jpg … /avatars/_profile_.jpg）
修复后：管理端 总请求 27 个，失败  0 个；console 错 13 → 0
```

---

### P1-2 首页 27 个脚本同步阻塞

**成因**
`index.html` 以**裸 `<script src>`** 方式引入 27 个脚本，全部无 `defer`/`async`/`type="module"`。浏览器必须逐个下载并执行完毕才能继续解析 HTML。

**影响范围**
- 首页首屏被 27 次串行网络往返阻塞。本地实测 `DOM=10ms` 是**零延迟假象**；真实 4G 下每个往返按 50–150ms 计，累积延迟可达秒级
- 对性能弱、网络差的老年用户设备影响最大

**修复方案与改动**
**全站 84 个外链脚本统一加 `defer`**（12 个页面）。`defer` 保证按文档顺序执行且不阻塞解析，脚本间依赖关系不受影响。

**验证**
```
home.html   DOM 124ms → 20ms
about.html  DOM  87ms → 16ms
services    DOM  79ms → 17ms
news.html   DOM  78ms → 16ms
```
> 说明：这是本地无网络延迟下的对比，真实环境收益更显著。

---

### P1-4 兼容性回退缺失（color-mix / backdrop 前缀）

**成因**
CSS 大量使用 `color-mix()`（31 处）与 `backdrop-filter`（11 处标准声明），但：
- `color-mix()` 需 Chrome 111+ / Safari 16.2+，且**无任何回退** → 老浏览器上整条声明失效，元素底色变透明
- 11 处 `backdrop-filter` **缺 `-webkit-` 前缀** → 旧 Safari 毛玻璃失效

**影响范围**
目标用户（老年人）常用旧设备，正是被这条基线排除的群体。

**修复方案与改动**
1. **backdrop 前缀**：为 11 处标准声明补 `-webkit-backdrop-filter`。
2. **color-mix 回退**：按「**同一表达式 → 一个主题感知的静态变量**」方案处理：
   - 用脚本解析 `tokens.css` 的 light/dark 两套变量，**逐表达式精确计算**出等价色值
   - 生成 23 个回退变量 `--cm-1 … --cm-23`，分别注入 `:root,body.light{…}` 与 `body.dark,…{…}`
   - 在原属性前插入回退声明，形成标准的两段式写法：
     ```css
     background:var(--cm-1);
     background:color-mix(in srgb,var(--bg) 86%, transparent);
     ```
   - 老浏览器用 `var(--cm-1)`，新浏览器用 `color-mix()` —— 两者视觉等价

   **关键实现细节**：初版脚本**漏解析** `var(--x)` 后的百分比，把 `color-mix(in srgb,var(--bg) 86%,transparent)` 误算成 alpha `1.0`。经复核发现后**全部回滚重做**，修正算法并对关键值手工验算：
   ```
   var(--bg) 86% + transparent  →  rgb(246 242 232 / 0.86)   ✅
   var(--gold) 16% + #fff       →  rgb(239 233 224)          （0.16×155 + 0.84×255 = 239）✅
   var(--blue) 14% + transparent → rgb(54 91 73 / 0.14)      ✅
   ```

3. **`:has()`（约 1 处实际使用）**：**不做修改**。CSS 对不支持的选择器会**整条忽略**（容错），天然优雅降级；失效后果仅为旧浏览器上「新对话空状态提示不自动隐藏」，影响轻微。已在 §4 记录。

---

### P1-5 CSP 覆盖不全

**成因**
CSP 只写在 `index.html` 与 `yangheng.html`（后经复查 `管理端.html` 也有），其余页面无。初次统计时**中文文件名未被正确扫描**导致漏计。

**影响范围**
- 缺少 XSS 纵深防御，安全策略不一致、易在维护中退化
- `管理端.html` 是数据展示最密集的页面，最需要

**修复方案与改动**
1. 统一 CSP 模板下发到全部 13 页（新增 10 页 + 更新 3 页）
2. **移除未使用的字体外域**（`fonts.googleapis.cn` / `fonts.loli.net` / `fonts.gstatic.com`）——经确认 CSS 中无 `@font-face`/`@import`，全站未加载任何网络字体，同时移除 `yangheng.html` 中指向它的无用 `preconnect`
3. **消除 `login.html` 的阻断风险**：该页原有一段 **287 行内联 `<style>`**，会被 `style-src 'self'` 直接废掉。已抽出为 `css/login.css` 并以 `<link>` 引入，从而全站可用严格 CSP（无需 `style-src 'unsafe-inline'`）

**验证**
```
修复前：CSP 覆盖 2（统计口径错误）/ 实际 3
修复后：CSP 覆盖 13 / 13
login.html：资源 3 → 4 个（新增 login.css），console 错 0，无 CSP 违规
```

---

## 2. P2 级修复

### P2-1 表单可访问名称缺失
**成因**：输入框仅有 `placeholder`。`placeholder` 在输入后消失、对比度通常偏低，**不能替代 `<label>`**，屏幕阅读器读不出字段用途。
**改动**：为登录表单（姓名/密码）与管理端登录（账号/密码）补 `aria-label`。
**验证**：无标签控件 **7 → 1**（剩余 1 个为管理端动态容器内输入框，见 §4）。

### P2-6 深色模式不生效（两个独立根因）
**成因**
1. **5 个页面漏引 `theme.js`**（index/login/yangheng/个人端/管理端）→ 根本没有设置 `data-theme`
2. **CSS 特异性陷阱**：`tokens.css` 的默认变量选择器写作 `:root,body.light`，其中 `body.light` **直接命中 body 元素**；而深色规则 `:root[data-theme="dark"]` 只命中 `<html>`，body 依赖**继承**。**直接命中优先于继承** → 凡是 `<body class="light">` 的页面（index / yangheng / 管理端）**永久锁定浅色**。

**影响范围**：系统处于深色模式的用户打开站点仍看到亮色；且 head 里声明的 `theme-color media=(prefers-color-scheme:dark)` 与之矛盾，浏览器 UI 与内容不一致。

**改动**
1. 为 5 个缺失页面补 `theme.js`
2. 提升深色规则特异性，使其**直接命中 body**：
   ```css
   body.dark,:root[data-theme="dark"],:root[data-theme="dark"] body { … }
   ```
3. 为 `css/login.css`（独立主题，仅含 light 变量）**补一套深色变量**（纸色转深、墨色反白，保留朱砂与金色相）

**验证**
```
                 dark                light
/yangheng.html   rgb(25,31,27)  ✅    rgb(246,242,232) ✅
/login.html      rgb(25,31,27)  ✅    rgb(245,239,226) ✅
/home.html       rgb(26,22,17)  ✅    rgb(245,239,226) ✅
/管理端.html      rgb(25,31,27)  ✅    rgb(246,242,232) ✅
```

### P2-7 图片懒加载
**改动**：为 68 个非首屏 `<img>` 补 `loading="lazy" decoding="async"`（每页首个图片保持 eager，避免影响 LCP）。

### P2-8 遗留调试日志
**改动**：删除 3 条独占一行的 `console.log`（`app.js` 2 条、`storage.js` 1 条——后者同时是误导性信息）。
**保留**：`mock-api.js` 中作为演示反馈的 1 条（位于表达式内，且属 mock 特性）；`solarlunar.min.js` 第三方库注释中的 2 处（非可执行代码）。

---

## 3. 审核误判更正（重要）

本轮修复过程中，**用运行时实测推翻了原报告中的 3 条结论**，特此更正并说明原因：

### 3.1 P1-3「管理端动态表格缺滚动容器」—— **不成立**

**原判据**：375px 下检测到 123 个元素越界（table/thead/tr/th），且 `admin-views.js` / `admin-export.js` 生成的 `<table>` 未包裹滚动容器。

**推翻证据**：逐元素追溯祖先链后显示——**所有越界元素都在 `div.table-wrap` 内**，而该容器已有 `overflow-x:auto`（`css/xz.css:48`）；且 **`document.scrollWidth - clientWidth = 0`**，页面本身零横向溢出。表格在容器内横向滚动是**正确的响应式表格实现**。

**我的检测缺陷**：只判断「元素右边超出视口」，**未排除「处于横向滚动容器内」这一合法情况**，产生假阳性。
另：`admin-export.js` 中生成的 `<table>` 是**导出用 HTML 字符串**（用于下载报表），根本不渲染在页面上。

**处置**：不改代码。

### 3.2 P2-4「news-detail.html 缺 h1」—— **不成立**

**原判据**：静态 HTML 中 `h1=0`。

**推翻证据**：`news-detail.js:67` 实际渲染了 `<h1>' + esc(art.title) + '</h1>'`。**页面标题由 JS 注入，静态扫描看不到。**

**处置**：仍为 `yangheng.html` 补了页面级 `<h1 class="sr-only">`（该页确实缺），并给管理端登录层补了 `role="dialog" aria-modal="true" aria-labelledby` —— 它有两个 h1（登录层 + 主界面顶栏），视觉上互斥但 DOM 并存，加对话框语义后屏幕阅读器可正确区分。

### 3.3 P2-5「landmark 仅 1 个 nav」—— **不成立**

**原判据**：静态统计 `<nav>` 仅 1 处。

**推翻证据**：运行时 13 页合计 **`<nav>` 9 个、`<header>` 10 个、`<footer>` 11 个** —— 页头页脚由 JS 注入，静态扫描同样看不到。

**处置**：不改代码。

### 3.4 附带更正：CSP 覆盖数
原报告写「仅 2/13 页有 CSP」，实为 **3/13**（`管理端.html` 也有）——中文文件名在 `git ls-files` 中为转义形式，未被扫描到。

**方法论教训**：**静态 grep 统计必须配合运行时 DOM 实测才能定性**。本轮 3 条误判全部源于「只看静态文件、不看渲染结果」。

---

## 4. 未修复 / 保留项（附理由）

| 项 | 状态 | 理由 |
|---|---|---|
| **P2-3 无构建流程** | 结构性，未做 | 引入 esbuild/vite 会改变部署链路（需同步改 GitHub Actions 工作流），属独立工程任务，非缺陷修复 |
| **P3-4 演示密码 `123456`** | 保留 | 该站为纯前端演示站，密码仅作登录态演示；改为服务端校验需后端，已超出当前架构 |
| **`:has()` 降级** | 不改 | CSS 对不支持的选择器整条忽略（天然容错），仅影响旧浏览器上一个空状态的隐藏，代价极小 |
| **管理端 1 个无标签输入框** | 未定位 | 该控件为运行时动态创建，位置未确定；建议后续用屏幕阅读器实测定位 |
| **历史 6 个 commit 的作者邮箱** | 未处理 | 需重写历史（改 hash、远端分叉），风险高；已通过修改 git 配置阻止**今后**的新提交暴露 |

---

## 5. 改动文件清单

**HTML（13 个，全部）**
`index.html` `login.html` `yangheng.html` `管理端.html` `个人端.html` `home.html` `about.html` `contact.html` `guide.html` `news.html` `news-detail.html` `privacy.html` `services.html`
> 统一项：viewport、CSP、`theme.js`、`<script defer>`、图片懒加载、（部分）h1 / `aria-label` / 角色语义

**CSS**
- `css/login.css` — **新增**（从 login.html 抽出 287 行内联样式）+ 深色主题变量
- `css/tokens.css` — 23 个 color-mix 回退变量（light/dark 两套）+ 深色规则特异性修正
- `css/content-hub.css` `css/style.css` `css/personal.css` — color-mix 回退、backdrop 前缀
- `site.css` — color-mix 回退、backdrop 前缀
- `css/xz.css` `css/common.css` — backdrop 前缀

**JS**
- `js/storage.js` — 头像加载逻辑（默认不探测静态目录）+ 删除误导性日志
- `js/app.js` — 删除 2 条调试日志

**其他**
- `.gitignore` — 新增 9 条忽略规则（备份/截图/审查脚本/报告）

---

## 6. 验证证据汇总

| 验证项 | 结果 |
|---|---|
| 自带 Playwright 测试 | **16 / 16 通过** |
| 静态检查 `check_static.py` | **PASS：45 个 JS、13 个 HTML** |
| 13 页运行时 console 错误 | **全部 0** |
| 管理端 404 请求 | **13 → 0** |
| CSP 覆盖 | **13 / 13** |
| 深色模式（4 页 × 2 主题） | **8 / 8 正确** |
| 表单无标签控件 | **7 → 1** |
| 图片 alt 覆盖 | **76 / 76** |
| CSS 括号配平 | 7 个文件全部平衡 |
| 中文编码完整性 | 无 U+FFFD 替换字符 |

---

## 7. 建议的后续动作

1. **上线前跑一次 Lighthouse**（重点 LCP/CLS/TBT），确认 defer 与懒加载的真实收益
2. **真机浏览器矩阵验证**（Safari / Firefox / 低端安卓机）
3. **决定首页策略**：目前 `/` 与 `/index.html` 会重定向到登录页 —— 建议让机构介绍页（`home.html`）作为公开入口
4. **引入构建流程**（如确有需求）——可解决压缩、hash 缓存、前缀自动化（autoprefixer 可替代本轮的手工补前缀）
5. **用屏幕阅读器（NVDA / VoiceOver）实测**关键流程，补上最后一处无标签控件

---

*报告完 · 2026-09-25*
*修复基线：`c6e6d47` ｜ 验证方式：自带测试 + 静态检查 + 无头浏览器运行时实测*
