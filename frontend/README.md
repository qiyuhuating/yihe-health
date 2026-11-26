# 颐和前端 · 2.1.0

颐和前端基于 Vue 3、Vue Router 4、Element Plus 2 与 Vite 8 构建，使用 Axios 调用 Go 后端接口，通过 ECharts 6 渲染健康趋势图表，并集成 Leaflet 与 OpenStreetMap 展示定位与轨迹。在 Windows 桌面端中，Tauri 2（WebView2）直接加载编译内嵌在 Go 服务端中的生产静态资源。

[整体技术栈与架构](../docs/WORKSPACE.md) · [源码工程总览](../README.md) · [后端接口与数据](../backend/README.md) · [历史改进记录](../docs/CHANGELOG.md)

## 模块职责

- 管理端与家属端共享基础 UI 组件、服务端会话及 CSRF 校验机制，家属账号登录后仅展示已绑定长者的数据。
- 侧边导航按当前路由自动展开对应分组；长者档案页在窄屏下自动切换为资料卡片视图，运营总览在宽屏下采用栅格布局，列表与表单页面均内置加载状态与失败重试操作。
- 照护协作模块提供工单与履历分页、接单、处置登记、双人独立复核及队列诊断界面，并对护理员与审计员账号实行按角色受限访问。
- `src/auth.ts` 负责跨页面轮询服务就绪状态。当数据库断开或尚未就绪时，界面会保留当前路由与用户输入内容，提示手动重试而不自动重放写请求。
- 首次部署的管理员初始化界面通过 `src/desktop.ts` 调用受限的 Tauri 原生命令，前端代码不保存桌面初始化令牌或数据库凭据。

前端所有数据均通过 Go `/api/v1/` 接口读写，不直接连接 MySQL。

### 数据流及代码所有权

| 层 | 入口 | 职责 |
| --- | --- | --- |
| 后端契约 | `src/api/generated-api.d.ts`、`contractClient.ts` | Go OpenAPI 生成的请求与响应类型；生产请求仍经 HTTP 安全中间件 |
| 邀请数据 | `src/api/invitations.ts` | 邀请记录、可绑定长者 DTO；分页总数在读取边界验证 |
| 公告数据 | `src/domain/notices.ts` | 文章、附件和评论 DTO；家属端与管理端共用绝对路径图片 URL |
| 监护人状态 | `src/composables/useGuardians.ts` | 监护人查询、创建、编辑和删除；后端继续沿用 `customerId` 等 API 字段 |
| 网络与会话 | `src/api/client.ts`、`src/auth.ts` | API 路径、Cookie、CSRF、统一响应校验及连接状态 |
| 读取调度 | `src/composables/latestRead.ts`、`usePagedRecords.ts` | 取消过期读取，避免迟到响应覆盖当前列表 |
| 设备领域数据 | `src/composables/deviceModels.ts` | 设备、告警、绑定码 DTO；独立组件实例的初始状态 |
| 设备绑定流程 | `src/composables/useDeviceBindings.ts` | 绑定码查询、创建、作废与切换长者时的响应隔离 |
| 设备页面编排 | `src/composables/useDevices.ts` | 设备与告警读取、过滤分页、定时刷新和页面事件 |
| 展示层 | `src/views/DeviceManagement.vue` | 渲染设备列表及对话框，不承担后端数据转换 |
| 照护领域规则 | `src/domain/care.ts` | 工单与履历 DTO、优先级、状态及通知显示规则；无组件依赖 |
| 照护页面 | `src/components/CareInbox.vue` | 接单与复核交互、异步读取、页面可见性和任务事件响应 |
| 图表配置 | `src/charts/recordChartOptions.ts` | ECharts 心率、血压、睡眠渲染参数与无障碍/动画设置 |
| 图表生命周期 | `src/composables/useRecordCharts.ts` | 健康数据读取、取消过期请求、图表实例回收与尺寸监听 |

读路径是 **Go API → Axios/会话拦截器 → 领域组合式函数 → Vue 模板**。 设备异常工作台最多同时读取 6 个告警明细，防止设备数增长造成无界网络并发；请求总量仍取决于存在告警的设备与类别。写请求由用户操作触发，成功后显式重新读取；不得在读取失败或页面恢复可见时自动重放写操作。当前 `useDevices()` 的页面接口保持不变；新设备功能优先沿现有领域边界扩展，不把响应 DTO、持久状态和 DOM 事件混写在同一个文件。

## 本地开发与构建

请使用 Node.js `^20.19.0 || >=22.12.0`，并通过 `package-lock.json` 安装固定版本依赖：

```powershell
npm ci
# 仅生成或检查 API 类型时需要；应用开发和构建仍使用根目录的 TS 6
npm run api:tools:ci
npm run dev
# 构建生产静态产物至 dist/
npm run build
```

本地开发时 Vite 仅绑定本机回环地址，并将 `/api/v1` 代理至 `http://127.0.0.1:8795`。启动 Go 后端时设置 `ADDR=127.0.0.1:8795` 、`MYSQL_DSN` 及 `DATA_ENCRYPTION_KEY_FILE` 后即可联调业务页面（首次初始化需使用桌面引导或仓库初始化脚本）。

生产构建以 Chrome 120 为目标环境，默认关闭 source map 并拆分 CSS。执行桌面打包脚本 `desktop/build.ps1` 时，会自动将 `dist/` 下的产物复制到 Go 内嵌资源目录。

## 测试与静态资源

单元测试统一放在 `tests/`，与源码目录 `src/` 分离；子目录镜像 `src/` 结构，被测模块一律通过 `@/` 别名引用，不写相对路径：

```powershell
npm test
npm run typecheck
npm run typecheck:stable
npm run api:check
npm run test:api-drift
```

应用与测试固定使用 TypeScript 6.0.3。app、Node 和测试分别通过项目配置检查，SFC 范围检查使用 Vue 解析器及真实 `vue-tsc --showConfig` 输出，不依赖 TypeScript Compiler API。当前 44 个 SFC 中有 21 个使用 TS 脚本、1 个仅含模板，另有 22 个既有 JS 脚本记录在 `scripts/sfc-typecheck-baseline.json`，尚未完成严格类型迁移；新增组件及已迁移组件不得退回 JS。

API 类型仍由 Go OpenAPI 和官方 `openapi-typescript` 生成。其 7.13.0 版本要求 TS 5，因此固定安装在 `tools/openapi/` 独立工具目录及锁文件中，避免与应用 TS 6 的 peer 约束冲突。`api:check` 检查已提交生成结果，`test:api-drift` 使用真实 `tsc` CLI 和临时源码副本验证四种不兼容变更会使消费端编译失败；执行这两个命令还需要 Go 1.27.4（与后端 `go.mod` 一致）。工具目录不会进入浏览器产物。

端到端测试位于 `qa/`，基于 Playwright：

```powershell
npm --prefix qa ci
npm --prefix qa test
```

`qa/` 目录下的端到端测试基于 Playwright 编写，默认拦截模拟 API 以验证页面交互、空状态与窄屏布局；如需针对真实后端运行验证，请先准备专用的空 MySQL 测试库。

品牌图标、字体与配图位于 `public/static/`。在线地图瓦片依赖外部网络连接；对于设备尚未上报的健康指标，界面会直接显示缺失状态，不会用 `0` 填充。

## API v1 路径与会话切换说明

所有浏览器、桌面端与手表端请求统一使用 `/api/v1/` 前缀，后端已停止接收无版本号的 `/api/` 请求。服务端会话 Cookie 名称已更新为 `YIHE_SESSION`，升级后旧会话需重新登录。
