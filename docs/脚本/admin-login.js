/**
 * admin-login.js — 管理端登录门禁
 *
 * 强化点（2026-09-25 A 级加固）：
 *   1. 口令校验外置到 admin-credentials.js，本文件不再出现明文口令
 *   2. 会话存入 sessionStorage（关标签即失效），并带 nonce + sig 校验，
 *      手工拼一条 localStorage 记录无法再骗过门禁
 *   3. 连续失败 5 次锁定 60 秒，锁定状态跨刷新保留
 *   4. 登录成功 / 失败 / 登出 / 锁定 全部进审计（此前管理端登录零审计）
 *   5. 未登录时不触发任何数据初始化 —— 真正的门禁在 bootstrap 上（见 app.js）
 *
 * 注意：静态站没有服务端，这里做的是"提高门槛 + 可替换 + 可审计"，
 * 不能等同于真正的身份认证。生产环境必须由后端签发会话。
 */
var AdminLogin = function() {
    "use strict";
    if (window.YIHE_RUNTIME_CONFIG?.mode === 'http') return CareSession.staffGate();
    if (window.YIHE_RUNTIME_CONFIG?.mode !== 'demo') throw Error('登录配置未能加载');
    var KEY = "hm-admin-login";
    var FAIL_KEY = "hm-admin-login-fail";
    var LOCK_KEY = "hm-admin-login-lock";
    var MAX_FAIL = 5,
        LOCK_MS = 6e4,
        TTL = 864e5;
    var readyCbs = [];

    function store(key, val) {
        try {
            sessionStorage.setItem(key, val);
            return true;
        } catch (e) { return false; }
    }

    function persist(key, val) {
        try {
            localStorage.setItem(key, val);
        } catch (e) {}
    }

    function drop() {
        try {
            sessionStorage.removeItem(KEY);
        } catch (e) {}
        try {
            localStorage.removeItem(KEY);
        } catch (e) {}
    }

    /* 会话签名：同步校验和，用于识别"手工拼造"的记录（非密码学强度）。
       盐来自 脚本/config.js；算法与盐都在公开源码里，因此这只能挡"随手拼一条
       localStorage"这种最省事的绕过，不构成鉴权。 */
    function sigOf(o) {
        var s = [o.name, o.role, o.ts, o.nonce, (window.YIHE_CONFIG || {}).adminSigSalt].join("|"),
            h = 5381,
            i;
        for (i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0;
        return ("00000000" + h.toString(16)).slice(-8);
    }

    function valid(o) {
        return !!(o && "admin" === o.role && o.ts && Date.now() - o.ts < TTL && o.nonce && o.sig === sigOf(o));
    }

    function read() {
        var raw = null;
        try {
            raw = sessionStorage.getItem(KEY) || localStorage.getItem(KEY);
        } catch (e) {
            raw = null;
        }
        if (!raw) return null;
        var o = null;
        try {
            o = JSON.parse(raw);
        } catch (e) {
            o = null;
        }
        if (!valid(o)) {
            drop();
            return null;
        }
        return o;
    }

    function lockRemaining() {
        var until = 0;
        try {
            until = parseInt(localStorage.getItem(LOCK_KEY) || "0", 10) || 0;
        } catch (e) {}
        var left = until - Date.now();
        return left > 0 ? left : 0;
    }

    function failCount() {
        var n = 0;
        try {
            n = parseInt(localStorage.getItem(FAIL_KEY) || "0", 10) || 0;
        } catch (e) {}
        return n;
    }

    function resetFail() {
        try {
            localStorage.removeItem(FAIL_KEY);
            localStorage.removeItem(LOCK_KEY);
        } catch (e) {}
    }

    function hint(msg, invalidFields, focusField) {
        var n = document.getElementById("adminLoginHint");
        var fields = [document.getElementById("adminLoginName"), document.getElementById("adminLoginPassword")];
        if (n) {
            n.textContent = msg;
            n.hidden = !1;
            fields.forEach(function(field) {
                field.setAttribute("aria-describedby", n.id);
                field.removeAttribute("aria-invalid");
            });
            (invalidFields || []).forEach(function(field) { field.setAttribute("aria-invalid", "true"); });
        }
        (focusField || fields[1]).focus();
    }

    function fireReady(session) {
        for (var i = 0; i < readyCbs.length; i++) {
            try {
                readyCbs[i](session);
            } catch (e) {
                console.error("[AdminLogin] ready 回调执行失败:", e.message);
            }
        }
        readyCbs.length = 0;
    }

    function gate() {
        var session = read(),
            t = document.getElementById("adminLoginOverlay");
        t && (t.hidden = !!session);
        if (!session) document.getElementById("adminLoginName")?.focus();
        return session;
    }

    function afterVerify(user, r) {
        if (r && r.ok) {
            var now = Date.now(),
                session = {
                    name: user,
                    role: "admin",
                    ts: now,
                    nonce: Math.random().toString(36).slice(2) + now.toString(36)
                };
            session.sig = sigOf(session);
            if (!store(KEY, JSON.stringify(session))) {
                hint('无法保存本页登录状态，请允许浏览器存储后重试');
                return;
            }
            resetFail();
            Audit.log("登录成功", user + " · 校验方式 " + (r.mode || "unknown"), {
                actor: user,
                result: "success"
            });
            gate();
            resetIdle();
            fireReady(session);
            return;
        }
        var n = failCount() + 1;
        persist(FAIL_KEY, String(n));
        if (n >= MAX_FAIL) {
            persist(LOCK_KEY, String(Date.now() + LOCK_MS));
            resetFailCountKeepLock();
            Audit.log("登录锁定", "连续失败 " + n + " 次，锁定 60 秒", {
                actor: user,
                result: "failure"
            });
            hint("尝试次数过多，已锁定 60 秒");
            return;
        }
        Audit.log("登录失败", user + " · 第 " + n + " 次", {
            actor: user,
            result: "failure"
        });
        hint((r && r.reason ? r.reason : "账号或密码错误") + "（还可尝试 " + (MAX_FAIL - n) + " 次）");
    }

    /* 锁定与失败计数分开存：锁定生效期间不清零失败数会立刻再次触发 */
    function resetFailCountKeepLock() {
        try {
            localStorage.removeItem(FAIL_KEY);
        } catch (e) {}
    }

    var loginPending = false;
    function submit() {
        if (loginPending) return;
        var nameEl = document.getElementById("adminLoginName"),
            pwdEl = document.getElementById("adminLoginPassword"),
            hintEl = document.getElementById("adminLoginHint"),
            submitBtn = document.getElementById("adminLoginSubmit"),
            submitLabel = submitBtn && submitBtn.querySelector("[data-login-label]"),
            originalLabel = submitLabel && submitLabel.textContent;
        if (hintEl) hintEl.hidden = !0;
        if (!nameEl || !pwdEl) return;
        [nameEl, pwdEl].forEach(function(field) { field.removeAttribute("aria-invalid"); field.removeAttribute("aria-describedby"); });
        var u = nameEl.value.trim(),
            p = pwdEl.value;
        if (!u || !p) {
            var missing = !u ? nameEl : pwdEl;
            return void hint(!u ? "请输入账号" : "请输入密码", [missing], missing);
        }
        var left = lockRemaining();
        if (left > 0) return void hint("尝试次数过多，请 " + Math.ceil(left / 1000) + " 秒后再试");
        loginPending = true;
        if (submitBtn) { submitBtn.disabled = true; submitBtn.setAttribute("aria-busy", "true"); }
        if (submitLabel) submitLabel.textContent = "正在验证…";
        var finish = function() {
            loginPending = false;
            if (submitBtn) { submitBtn.disabled = false; submitBtn.removeAttribute("aria-busy"); }
            if (submitLabel) submitLabel.textContent = originalLabel;
        };
        if (window.AdminCredentials) {
            AdminCredentials.verify(u, p).then(function(r) {
                afterVerify(u, r);
            }).catch(function(e) {
                console.error("[AdminLogin] 凭据校验异常:", e.message);
                hint("凭据校验失败，请重试");
            }).finally(finish);
        } else {
            afterVerify(u, {
                ok: !1,
                mode: "none",
                reason: "凭据模块未加载"
            });
            finish();
        }
    }

    function logout() {
        try {
            Audit.log("退出登录", (read() || {}).name || "", {
                actor: (read() || {}).name || "未登录"
            });
        } catch (e) {}
        drop();
        location.reload();
    }

    var submitBtn = document.getElementById("adminLoginSubmit");
    submitBtn && submitBtn.addEventListener("click", submit);
    var nameInput = document.getElementById("adminLoginName");
    nameInput && nameInput.addEventListener("keydown", function(e) {
        if ("Enter" === e.key) {
            e.preventDefault();
            var n = document.getElementById("adminLoginPassword");
            n && n.focus();
        }
    });
    var pwdInput = document.getElementById("adminLoginPassword");
    pwdInput && pwdInput.addEventListener("keydown", function(e) {
        "Enter" === e.key && (e.preventDefault(), submit());
    });
    var logo = document.querySelector("[data-admin-login-logo]");
    logo && (logo.src = iconSVG("heart", 56, "#FFFFFF"));

    /* 降级提示：file:// 下 crypto.subtle 不可用，摘要校验强度下降 */
    try {
        if (window.AdminCredentials && !AdminCredentials.detectMode()) {
            var sub = document.querySelector(".login-sub");
            sub && (sub.textContent += "（当前非 https/localhost，凭据校验已降级）");
        }
    } catch (e) {}

    var logoutBtn = document.getElementById("btnAdminLogout");
    logoutBtn && logoutBtn.addEventListener("click", logout);
    gate();

    /* 空闲超时：15 分钟无任何操作自动登出，避免管理端在无人值守的电脑上一直开着 */
    var IDLE_MS = 15 * 60 * 1000,
        idleTimer = null;

    function resetIdle() {
        if (!read()) return;
        clearTimeout(idleTimer);
        idleTimer = setTimeout(function() {
            try {
                Audit.log("空闲超时登出", (read() || {}).name || "", {
                    result: "info"
                });
            } catch (e) {}
            drop();
            location.reload();
        }, IDLE_MS);
    }
    ["mousemove", "keydown", "click", "touchstart"].forEach(function(ev) {
        document.addEventListener(ev, resetIdle, {
            passive: !0
        });
    });
    resetIdle();

    return {
        getLogin: read,
        isLoggedIn: function() {
            return !!read();
        },
        gate: gate,
        submit: submit,
        logout: logout,
        lockRemaining: lockRemaining,
        /* 登录成功后触发；已登录时立即触发一次 */
        onReady: function(cb) {
            var s = read();
            if (s) return void cb(s);
            readyCbs.push(cb);
        }
    };
}();
