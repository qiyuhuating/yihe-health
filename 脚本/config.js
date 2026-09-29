/* ===========================================================================
 * config.js — 全站配置单一来源
 * ---------------------------------------------------------------------------
 * 加载要求：必须在任何读取方之前加载（admin-credentials.js /
 * admin-login.js / login-page.js）。测试/check_static.py 会校验这个顺序。
 *
 * ── 关于本文件里的"凭据"，请先读完再改 ──
 * 本站是纯静态站，没有后端。下面这些值对任何访问者都是可见的（打开源码即可），
 * 存摘要而不是明文，只是为了避免"一眼扫到密码"，**不构成任何访问控制**。
 * 演示口令是公开的 4 位数字：管理端 admin / admin123，居民姓名 / 123456。
 * 真正的鉴权必须由服务端签发会话，前端校验一律视为可绕过。
 * 见根目录 README.md 与 文档/handoff/frontend.md「入口和状态」。
 * =========================================================================== */
(function (global) {
    "use strict";

    var CONFIG = {
        /* 演示标记：用于在界面上显示"这是演示"，并供脚本判断是否可跳过安全提示 */
        demo: true,

        adminUser: "admin",
        adminSalt: "yihe-admin-v1",
        adminHash: "8c3d52a4e79b956214fb45e0b3e5199d3055f55b71c5f83e4f2be2166d2a80c8",
        adminHashWeak: "95ed9b1a",

        /* 管理端会话签名盐。算法为 djb2，同样属"提高门槛"而非真实鉴权。 */
        adminSigSalt: "yihe-admin-sig-v1",

        /* ---------------- 居民端演示凭据 ---------------- */
        userSalt: "yihe-user-v1",
        userHash: "ae9a109cfbe2913fe68e61a9844a7e135aa339270b27afe0e13ab87d2a33ac52",
        userHashWeak: "8ed45aa9"
    };

    /* 允许部署方在 config.js 之后覆盖单个字段（不改动本文件即可换配置） */
    if (global.YIHE_CONFIG_OVERRIDE && typeof global.YIHE_CONFIG_OVERRIDE === "object") {
        Object.keys(global.YIHE_CONFIG_OVERRIDE).forEach(function (key) {
            CONFIG[key] = global.YIHE_CONFIG_OVERRIDE[key];
        });
    }

    global.YIHE_CONFIG = CONFIG;
})(window);
