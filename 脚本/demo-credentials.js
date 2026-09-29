/**
 * demo-credentials.js — 演示站通用凭据层（个人端 & 管理端共用一套实现）
 *
 * ── 静态站的安全天花板（务必读完再改）──
 * 本站没有后端。任何写在前端的凭据对访问者都是可见的，所以这里做的不是"保密"：
 *   1. 源码中不出现可直读的明文口令（只存摘要）
 *   2. 凭据可替换：调用方传入自己的 cfg 即可
 *   3. 优先走 Web Crypto 的 SHA-256（需 https 或 localhost）
 * 真正的访问控制必须由服务端承担 —— 静态站做不到，别指望这里。
 *
 * 降级路径：非安全上下文（如 file:// 直接打开）下 crypto.subtle 不可用，
 * 退回同步弱摘要（FNV-1a 迭代 64 轮），仅保证"源码无明文 + 功能可用"，
 * 不具备密码学强度。
 */
window.DemoCredentials = function() {
    "use strict";

    function fnv1a(str) {
        var h = 0x811c9dc5,
            i, j, k, t;
        for (i = 0; i < str.length; i++) {
            h ^= str.charCodeAt(i);
            h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
        }
        for (k = 0; k < 64; k++) {
            t = h.toString(36);
            for (j = 0; j < t.length; j++) {
                h ^= t.charCodeAt(j);
                h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
            }
        }
        return ("00000000" + (h >>> 0).toString(16)).slice(-8);
    }

    function weakDigest(salt, password) {
        return fnv1a(salt + ":" + password);
    }

    function sha256Hex(str) {
        if (!window.crypto || !crypto.subtle || !crypto.subtle.digest) return Promise.resolve(null);
        try {
            var buf = new TextEncoder().encode(str);
            return crypto.subtle.digest("SHA-256", buf).then(function(e) {
                var out = "",
                    b = new Uint8Array(e),
                    i;
                for (i = 0; i < b.length; i++) out += ("0" + b[i].toString(16)).slice(-2);
                return out;
            }).catch(function() {
                return null;
            });
        } catch (e) {
            return Promise.resolve(null);
        }
    }

    /* 常量时间比较，避免通过响应时间侧信道逐字节试出摘要 */
    function safeEqual(a, b) {
        if ("string" != typeof a || "string" != typeof b || a.length !== b.length) return !1;
        var r = 0,
            i;
        for (i = 0; i < a.length; i++) r |= a.charCodeAt(i) ^ b.charCodeAt(i);
        return 0 === r;
    }

    /**
     * 校验口令
     * @param {Object} cfg  {hash?:string, hashWeak?:string, password?:string}
     * @param {string} password 用户输入的口令
     * @param {string} salt
     * @returns Promise<{ok:boolean, mode:'sha256'|'weak'|'plain-config'|'none', reason?:string}>
     */
    function verify(cfg, password, salt) {
        cfg = cfg || {};
        return sha256Hex(salt + ":" + password).then(function(hex) {
            if (hex && cfg.hash) return {
                ok: safeEqual(hex, cfg.hash),
                mode: "sha256"
            };
            if (cfg.hashWeak && safeEqual(weakDigest(salt, password), cfg.hashWeak)) return {
                ok: !0,
                mode: "weak"
            };
            if (cfg.password) return {
                ok: password === cfg.password,
                mode: "plain-config"
            };
            return {
                ok: !1,
                mode: "none",
                reason: "当前环境无法校验凭据"
            };
        });
    }

    return {
        verify: verify,
        weakDigest: weakDigest,
        safeEqual: safeEqual,
        detectMode: function() {
            return !(!window.crypto || !crypto.subtle || !crypto.subtle.digest);
        }
    };
}();
