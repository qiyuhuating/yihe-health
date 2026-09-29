# 颐和健康前端

面向居家照护场景的静态前端项目，包含公开页面、居民端和工作人员端。GitHub Pages 从 docs/ 发布，因此仓库根目录只保留项目说明、Git 忽略规则、工作流和一个项目文件夹。

## 快速开始

需要 Node.js 18+ 和 Python 3。运行项目检查或生成生产静态包：

~~~sh
npm --prefix docs run check
npm --prefix docs run build
~~~

生产构建输出到 docs/dist/。生产部署应发布该目录；GitHub Pages 当前用于展示演示站点。

## 项目目录

- docs/：完整前端项目，包括网页、样式、脚本、数据、静态资源、测试、工具、交付文档和部署配置。
- .github/workflows/：持续集成工作流。
- README.md：项目介绍与使用说明。

## 交付文档

- [系统架构](docs/文档/architecture.md)
- [后端接入交付说明](docs/文档/handoff/backend-integration.md)
- [前端交接说明](docs/文档/handoff/frontend.md)
- [发布与回滚手册](docs/文档/handoff/release.md)

## 当前交付边界

项目包含前端 HTTP 适配层和静态包构建脚本，不包含真实后端服务、生产数据库、身份提供方或通知投递服务。后端须按接入说明实现契约并完成联调，才能作为完整线上产品交付。