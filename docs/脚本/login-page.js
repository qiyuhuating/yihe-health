/*
 * 登录页专用脚本（login.html）
 * -------------------------------------------------------------
 * 设计说明：
 *   登录由 login.html 与本脚本处理，并直接对接全站唯一数据源 数据/residents.js。
 *
 * 会话契约：
 *   成功登录后写入 localStorage["hm-login"] = JSON.stringify({uid, name, ts})
 *   index.html 加载时读取该会话；有效则进入居民端，无效则跳回登录页。
 */
(function() {
    "use strict";
    if (window.YIHE_RUNTIME_CONFIG?.mode !== 'demo') { CareLoginHttp.attach(); return; }

    // 居民数据：取自 数据/residents.js（全站单一数据源 RESIDENT_DATA）
    var RES = window.RESIDENT_DATA || {};
    var PATIENTS = Object.keys(RES)
        .map(function(k) {
            return RES[k];
        })
        .sort(function(a, b) {
            return a.id - b.id;
        });

    var SESSION_KEY = "hm-login";
    /* 演示口令摘要集中在 脚本/config.js。校验模块缺失时拒绝登录；
       window.__DEMO_CRED__ 可覆盖演示配置，真实鉴权由后端承担。 */
    var PWD_CFG = window.__DEMO_CRED__ || (function() {
        var C = window.YIHE_CONFIG || {};
        return {hash: C.userHash, hashWeak: C.userHashWeak};
    })();

    function showError(msg, invalidField, focusField) {
        var hint = document.getElementById("loginHint");
        if (hint) {
            hint.textContent = msg;
            hint.hidden = false;
            [document.getElementById("loginName"), document.getElementById("loginPassword")].forEach(function(field) {
                field.setAttribute("aria-describedby", hint.id);
                if (field === invalidField) field.setAttribute("aria-invalid", "true");
                else field.removeAttribute("aria-invalid");
            });
        }
        (focusField || document.getElementById("loginPassword"))?.focus();
    }

    function clearErrors() {
        var hint=document.getElementById("loginHint");if(hint)hint.hidden=true;
        [document.getElementById("loginName"),document.getElementById("loginPassword")].forEach(function(field){field.removeAttribute("aria-invalid");field.removeAttribute("aria-describedby");});
    }

    var pending = false;
    function doLogin() {
        if (pending) return;
        clearErrors();
        var nameEl = document.getElementById("loginName");
        var pwdEl = document.getElementById("loginPassword");
        var name = nameEl ? nameEl.value.trim() : "";
        var pwd = pwdEl ? pwdEl.value : "";

        if (!name) {
            showError("请输入姓名", nameEl, nameEl);
            return;
        }
        if (!pwd) {
            showError("请输入密码", pwdEl, pwdEl);
            return;
        }

        var savedResidents = {};
        try { savedResidents = JSON.parse(localStorage.getItem('yihe-community-v1') || '{}').residents || {}; }
        catch (_) { /* Keep built-in demo identities available when profile data is unreadable. */ }
        var matches = PATIENTS.filter(function(p) {
            return 'resident-'+p.id === name || p.name === name || savedResidents[p.id] && savedResidents[p.id].name === name;
        });
        var user=matches.length===1?matches[0]:null;
        if (!user) {
            showError("账号不存在或姓名重复，请使用 resident-居民编号", nameEl, nameEl);
            return;
        }

        if (!window.DemoCredentials || typeof DemoCredentials.verify !== 'function') {
            showError("登录校验未能加载，请刷新页面后重试");
            return;
        }
        pending = true;
        var submit = document.getElementById('loginSubmit');
        var submitLabel=submit?.querySelector('[data-login-label]'),originalLabel=submitLabel?.textContent;
        if (submit) { submit.disabled = true; submit.setAttribute('aria-busy', 'true'); }
        if(submitLabel)submitLabel.textContent='正在验证…';
        Promise.resolve().then(function() {
            return DemoCredentials.verify(PWD_CFG, pwd, (window.YIHE_CONFIG || {}).userSalt);
        }).then(function(r) {
            if (!r.ok) {
                showError("密码错误，请核对后重试", pwdEl, pwdEl);
                return;
            }
            try {
                localStorage.setItem(
                    SESSION_KEY,
                    JSON.stringify({
                        uid: user.id,
                        name: user.name,
                        ts: Date.now()
                    })
                );
            } catch (e) {
                showError("浏览器无法保存演示会话，请允许本机存储后重试");
                return;
            }
            // 写入标准会话后跳转个人端
            location.href = "index.html";
        }).catch(function() {
            showError("登录校验暂时不可用，请稍后重试");
        }).finally(function() {
            pending = false;
            if (submit) { submit.disabled = false; submit.removeAttribute('aria-busy'); }
            if(submitLabel)submitLabel.textContent=originalLabel;
        });
    }

    // 表单提交（拦截默认刷新，避免冲掉重定向）
    var form = document.getElementById("loginForm");
    if (form) {
        form.addEventListener("submit", function(e) {
            e.preventDefault();
            doLogin();
        });
    }

    // 按钮点击
    var btn = document.getElementById("loginSubmit");
    if (btn) {
        btn.addEventListener("click", function(e) {
            e.preventDefault();
            doLogin();
        });
    }

    // 输入框回车：姓名 -> 密码；密码 -> 提交
    var nameEl = document.getElementById("loginName");
    if (nameEl) {
        nameEl.addEventListener("input", clearErrors);
        nameEl.addEventListener("keydown", function(e) {
            if (e.key === "Enter") {
                e.preventDefault();
                var p = document.getElementById("loginPassword");
                if (p) p.focus();
            }
        });
    }
    var pwdEl = document.getElementById("loginPassword");
    if (pwdEl) {
        pwdEl.addEventListener("input", clearErrors);
        pwdEl.addEventListener("keydown", function(e) {
            if (e.key === "Enter") {
                e.preventDefault();
                doLogin();
            }
        });
    }
})();
