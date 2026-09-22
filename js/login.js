var LoginModule = function() {
    "use strict";
    var e = "hm-login";

    function t() {
        var e = document.getElementById("loginOverlay");
        e && (e.hidden = !0)
    }

    function n() {
        var n = document.getElementById("loginHint"),
            o = document.getElementById("loginName").value.trim(),
            i = document.getElementById("loginPassword").value;
        if (n && (n.hidden = !0), o) {
            var d = PATIENTS ? PATIENTS.find(function(e) {
                return e.name === o
            }) : null;
            if (d)
                if ("123456" === i) {
                    try {
                        localStorage.setItem(e, JSON.stringify({
                            uid: d.id,
                            name: d.name,
                            ts: Date.now()
                        }))
                    } catch (e) {}
                    Audit.log("登录", d.name), App.USER_ID = d.id, "login.html" === location.pathname.split("/").pop() ? location.href = "index.html" : (t(), App.onLogin && App.onLogin(d))
                } else n && (n.textContent = "密码错误，请核对后重试", n.hidden = !1);
            else n && (n.textContent = "姓名不存在，请核对后重试", n.hidden = !1)
        } else n && (n.textContent = "请输入姓名", n.hidden = !1)
    }
    var o = document.getElementById("loginSubmit");
    o && o.addEventListener("click", n);
    var i = document.getElementById("loginName");
    i && i.addEventListener("keydown", function(e) {
        if ("Enter" === e.key) {
            e.preventDefault();
            var t = document.getElementById("loginPassword");
            t && t.focus()
        }
    });
    var d = document.getElementById("loginPassword");
    d && d.addEventListener("keydown", function(e) {
        "Enter" === e.key && (e.preventDefault(), n())
    });
    return {
        getLogin: function() {
            var t = loadJSON(e, null);
            if (t && t.uid) {
                var n = PATIENTS && PATIENTS.some(function(e) {
                        return e.id === t.uid
                    }),
                    o = !t.ts || Date.now() - t.ts < 864e5;
                if (!n || !o) {
                    try {
                        localStorage.removeItem(e)
                    } catch (e) {}
                    return null
                }
            }
            return t
        },
        show: function() {
            var e = document.getElementById("loginOverlay");
            e && (e.hidden = !1);
            var t = document.getElementById("loginName");
            t && setTimeout(function() {
                t.focus()
            }, 50)
        },
        hide: t,
        submit: n,
        logout: function() {
            try {
                localStorage.removeItem(e)
            } catch (e) {}
            App.USER_ID = null, Audit.log("退出登录"), location.reload()
        }
    }
}();