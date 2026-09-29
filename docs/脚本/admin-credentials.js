/**
 * admin-credentials.js — 管理端凭据配置层（薄封装，实际校验复用 demo-credentials.js）
 *
 * ── 静态站的安全天花板（务必读完再改）──
 * 本站是纯静态站、没有后端。任何写在前端的凭据对访问者都是可见的，
 * 所以本文件做的不是"保密"，而是这三件事：
 *   1. 源码中不出现可直读的明文口令（只存摘要）
 *   2. 凭据可替换：在加载前设置 window.__ADMIN_CRED__ 即可换成自己的
 *   3. 真正的访问控制必须由服务端承担 —— 静态站做不到
 *
 * 摘要算法与降级策略见 脚本/demo-credentials.js。
 */
window.AdminCredentials = function() {
    "use strict";
    /* 凭据常量集中在 脚本/config.js（须在本文件之前加载）。此处只做"取值 + 允许覆盖"。 */
    var CFG = window.YIHE_CONFIG || {};
    var SALT = CFG.adminSalt;
    var DEFAULT_USER = CFG.adminUser;

    function cfg() {
        var o = window.__ADMIN_CRED__;
        if (o && o.user && (o.hash || o.hashWeak || o.password)) return o;
        return {
            user: DEFAULT_USER,
            hash: CFG.adminHash,
            hashWeak: CFG.adminHashWeak
        };
    }

    return {
        /**
         * @returns Promise<{ok:boolean, mode:string, reason?:string}>
         */
        verify: function(user, password) {
            var c = cfg();
            if (user !== c.user) return Promise.resolve({
                ok: !1,
                mode: "none",
                reason: "账号或密码错误"
            });
            if (!window.DemoCredentials) return Promise.resolve({
                ok: !1,
                mode: "none",
                reason: "凭据模块未加载"
            });
            return DemoCredentials.verify({
                hash: c.hash,
                hashWeak: c.hashWeak,
                password: c.password
            }, password, SALT);
        },
        weakDigest: function(password) {
            return DemoCredentials.weakDigest(SALT, password);
        },
        username: function() {
            return cfg().user;
        },
        detectMode: function() {
            return !!(window.DemoCredentials && DemoCredentials.detectMode());
        }
    };
}();
