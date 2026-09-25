! function() {
    "use strict";
    var e = window.AdminApp,
        t = KEYS.SETTINGS,
        n = e.settings,
        a = e.sl;

    function i(e) {
        document.body.className = document.body.className.replace(/\b(light|dark|font-\w+)\b/g, ""), document.body.classList.add(e.darkMode ? "dark" : "light", "font-" + e.fontSize), $("#settingDarkMode").checked = e.darkMode, $("#settingFontSize").value = e.fontSize, $("#settingRefreshRate").value = String(e.refreshRate || 1e4), $("#settingPush").checked = e.notifications.push, $("#settingSMS").checked = e.notifications.sms, $("#settingEmail").checked = e.notifications.email
    }
    i(n);
    var s = $("#hamburgerBtn"),
        r = $("#menuOverlay"),
        o = $("#menuPanel"),
        d = $("#menuClose");

    function c() {
        s.setAttribute("aria-expanded", "false"), s.classList.remove("open"), r.classList.remove("open"), o.classList.remove("open")
    }
    s && s.addEventListener("click", function() {
        s.setAttribute("aria-expanded", "true"), s.classList.add("open"), r.classList.add("open"), o.classList.add("open")
    }), r && r.addEventListener("click", c), d && d.addEventListener("click", c);
    var l = {
        summary: "概览",
        map: "地图",
        sharing: "导出",
        browse: "档案",
        profile: "账户",
        admission: "入院",
        discharge: "出院",
        audit: "审计"
    };

    function u(t) {
        e.currentView = t, c(), $("#topbarTitle").textContent = l[t] || t;
        for (var n = document.querySelectorAll(".view"), a = 0; a < n.length; a++) n[a].classList.remove("active");
        var i = $("#view-" + t);
        i && i.classList.add("active");
        for (var s = document.querySelectorAll(".menu-item"), r = 0; r < s.length; r++) {
            var o = s[r].dataset.view === t;
            s[r].classList.toggle("active", o);
            var d = s[r].querySelector(".menu-icon img[data-menu-icon]");
            d && (d.src = iconSVG(d.dataset.menuIcon, 22, o ? "#007AFF" : "#8E8E93"))
        }
        "browse" === t && e.renderBrowseView && e.renderBrowseView(), "sharing" === t && e.initExportPanel && e.initExportPanel(), "profile" === t && e.initProfileView && e.initProfileView(), "admission" === t && e.initAdmissionView && e.initAdmissionView(), "discharge" === t && e.initDischargeView && e.initDischargeView(), "audit" === t && e.initAuditView && e.initAuditView(), "map" === t && e.startMapView ? e.startMapView() : e.stopMapView && e.stopMapView()
    }
    for (var m = document.querySelectorAll(".menu-item"), g = 0; g < m.length; g++)(function(e) {
        e.setAttribute("role", "button"), e.setAttribute("tabindex", "0"), e.addEventListener("click", function() {
            u(e.dataset.view)
        }), e.addEventListener("keydown", function(t) {
            "Enter" !== t.key && " " !== t.key || (t.preventDefault(), e.click())
        })
    })(m[g]);

    var v = "",
        h = null;

    function S() {
        return "summary" === e.currentView && Promise.all([API.getPatients(), API.getAlerts()]).then(function(t) {
            ! function(e) {
                var t = {
                    normal: 0,
                    warning: 0,
                    danger: 0,
                    admitted: 0
                };
                e.forEach(function(e) {
                    e.admitted ? t.admitted++ : t[e.status]++
                });
                var n = getEl("statTotal"),
                    a = getEl("statNormal"),
                    i = getEl("statWarning"),
                    s = getEl("statDanger");
                n && a && i && s && (n.textContent = e.length, a.textContent = t.normal, i.textContent = t.warning, s.textContent = t.danger)
            }(t[0]),
            function(e) {
                var t = getEl("alertList");
                if (t) {
                    var n = getEl("alertStats");
                    if (n) {
                        const pending=e.filter(item=>item.state==='triggered').length;
                        n.textContent='待知晓 '+pending+' · 已知晓/处理中 '+(e.length-pending)+' · 仅为模拟预警';
                    }
                    t.innerHTML=e.length ? e.map(item => '<div class="alert-item danger"><span class="alert-icon" aria-hidden="true">!</span><div class="alert-info"><div class="alert-name">'+escapeHtml(item.name)+'</div><div class="alert-msg">'+escapeHtml(item.alertMsg)+'</div><small>'+(item.state==='acknowledged'?'已知晓 · 异常仍在持续':'待知晓')+'</small></div>'+(item.state==='triggered'?'<button class="btn btn-sm" data-ack-event="'+escapeHtml(item.eventId)+'">已知晓</button>':'')+'</div>').join('') : '<p class="empty">暂无危险预警</p>';
                    t.querySelectorAll('[data-ack-event]').forEach(button=>button.addEventListener('click',async()=>{
                        button.disabled=true;
                        try {const result=await API.acknowledgeAlert(button.dataset.ackEvent);if(!result.success)throw new Error(result.reason);Audit.log('知晓模拟预警',button.dataset.ackEvent);await S();}
                        catch(error){showToast(error.message);button.disabled=false;}
                    }));
                }
            }(t[1]),
            function(t) {
                var n = t.map(function(e) {
                    return e.id + "|" + e.heartRate.toFixed(1) + "|" + e.bloodOxygen.toFixed(1) + "|" + e.systolic.toFixed(1) + "|" + e.diastolic.toFixed(1) + "|" + e.bloodSugar.toFixed(1) + "|" + e.temperature.toFixed(1) + "|" + e.status + "|" + (e.admitted ? 1 : 0)
                }).join(",");
                if (n !== v) {
                    var i = h;
                    h = {}, t.forEach(function(e) {
                        h[e.id] = {
                            hr: e.heartRate.toFixed(1),
                            bo: e.bloodOxygen.toFixed(1),
                            sys: e.systolic.toFixed(1),
                            dia: e.diastolic.toFixed(1),
                            bs: e.bloodSugar.toFixed(1),
                            temp: e.temperature.toFixed(1),
                            st: e.status
                        }
                    }), v = n;
                    var s = getEl("patientTableBody");
                    if (s) {
                        var r = "";
                        t.forEach(function(e) {
                            var t = "";
                            e.admitted ? t = "admitted-row" : "danger" === e.status ? t = "danger-row" : "warning" === e.status && (t = "warning-row");
                            var n = Metrics.statusBadge(e.status),
                                s = e.admitted ? '<span class="badge badge-admitted">住院中</span>' : '<span class="badge ' + n + '">' + a(e.status) + "</span>",
                                o = e.admitted ? " t3" : "",
                                d = i && i[e.id],
                                c = d && d.hr !== e.heartRate.toFixed(1) ? ' class="val-updated"' : "",
                                l = d && d.bo !== e.bloodOxygen.toFixed(1) ? ' class="val-updated"' : "",
                                u = d && d.temp !== e.temperature.toFixed(1) ? ' class="val-updated"' : "",
                                m = d && d.sys !== e.systolic.toFixed(1) ? ' class="val-updated"' : "",
                                g = d && d.bs !== e.bloodSugar.toFixed(1) ? ' class="val-updated"' : "";
                            r += '<tr class="' + t + o + '" data-pid="' + e.id + '"><td><strong>' + escapeHtml(e.name) + "</strong></td><td>" + escapeHtml(e.gender) + "</td><td>" + e.age + "</td><td" + c + ">" + e.heartRate.toFixed(1) + ' <span class="fs11-t3">bpm</span></td><td' + l + ">" + e.bloodOxygen.toFixed(1) + '<span class="fs11-t3">%</span></td><td' + u + ">" + e.temperature.toFixed(1) + '<span class="fs11-t3">°C</span></td><td' + m + ">" + e.systolic.toFixed(1) + "/" + e.diastolic.toFixed(1) + "</td><td" + g + ">" + e.bloodSugar.toFixed(1) + ' <span class="fs11-t3">mmol/L</span></td><td>' + s + "</td></tr>"
                        }), s.innerHTML = r, s.querySelectorAll("tr").forEach(function(t) {
                            t.addEventListener("click", function() {
                                e.openDrawer && e.openDrawer(parseInt(t.dataset.pid, 10), t)
                            })
                        })
                    }
                }
            }(t[0]), $("#lastUpdate").textContent = "更新于 " + (new Date).toLocaleTimeString("zh-CN"), AlertSystem.sync(t[1])
        }).catch(function(e) {
            console.error("[Admin] 概览数据加载失败:", e.message);
            var t = getEl("lastUpdate");
            t && (t.textContent = "数据加载失败"), showToast("概览数据加载失败，请稍后重试")
        })
    }
    $("#simToggle").addEventListener("change", function() {
        API.setEnabled(this.checked)
    }), $("#btnSimDanger").addEventListener("click", function() {
        AudioUtils.warmup(), AudioUtils.ensureResumed(), API.simulateDanger().then(function(e) {
            e && e.success ? (S(), Audit.log("模拟危险", "随机居民")) : showToast(e && e.reason || "模拟失败")
        }).catch(function(e) {
            showToast("错误：" + e.message)
        })
    }), $("#btnSimWander").addEventListener("click", function() {
        AudioUtils.warmup(), AudioUtils.ensureResumed(), API.simulateWander().then(function(t) {
            t && t.success ? (S(), Audit.log("模拟走失", "随机居民"), "map" === e.currentView && e.refreshMap && e.refreshMap()) : showToast(t && t.reason || "模拟失败")
        }).catch(function(e) {
            showToast("错误：" + e.message)
        })
    }), $("#btnRefresh").addEventListener("click", S), document.addEventListener("keydown", function(t) {
        "Escape" === t.key && ($("#sidePanel").classList.contains("open") && e.closeDrawer ? e.closeDrawer() : o.classList.contains("open") && (c(), s.focus()))
    });
    var w = null;

    function y() {
        clearInterval(w);
        var t = n.refreshRate;
        t > 0 && (w = setInterval(function() {
            "summary" === e.currentView && S()
        }, t))
    }
    window._adminTickTimer = setInterval(API.tick, 3e3), y(), WSClient.on("health_update", function() {
            "summary" === e.currentView && S();
            var t = getEl("lastUpdate");
            t && (t.textContent = "实时更新 " + (new Date).toLocaleTimeString("zh-CN"))
        }), WSClient.on("alert", function(e) {
            AlertSystem.show({
                id: e.id,
                eventId: e.eventId,
                state: e.state,
                name: e.name,
                type: e.type,
                alertMsg: e.alertMsg,
                place: e.place
            })
        }), WSClient.on("alerts", function() {
            "summary" === e.currentView && S()
        }), WSClient.on("connected", function() {
            console.log("[admin] WS 已连接（仅实时预警）")
        }), WSClient.on("fallback", function() {}), WSClient.on("disconnected", function() {
            console.log("[admin] WS 已断开，管理端继续使用 mock 数据源")
        }), API.onExternalSync(function() {
            "summary" === e.currentView ? S() : "map" === e.currentView && e.refreshMap && e.refreshMap()
        }),
        function() {
            for (var e = document.querySelectorAll(".menu-icon img[data-menu-icon]"), t = 0; t < e.length; t++) {
                var n = e[t].dataset.menuIcon,
                    a = e[t].closest(".menu-item").classList.contains("active");
                e[t].src = iconSVG(n, 22, a ? "#007AFF" : "#8E8E93")
            }
            for (var i = document.querySelectorAll(".stat-icon img[data-stat-icon]"), s = {
                    check: "#34C759",
                    warn: "#FF9500",
                    bell: "#FF3B30",
                    users: "#007AFF"
                }, r = 0; r < i.length; r++) {
                var o = i[r].dataset.statIcon;
                i[r].src = iconSVG(o, 24, s[o] || "#8E8E93")
            }
            for (var d = document.querySelectorAll("img[data-admin-icon]"), c = 0; c < d.length; c++) {
                var l = d[c].classList.contains("ico-white");
                d[c].src = iconSVG(d[c].dataset.adminIcon, 16, l ? "#FFFFFF" : "#8E8E93")
            }
            var u = document.querySelector("img[data-modal-bell]");
            u && (u.src = iconSVG("bell", 48, "#FF3B30"))
        }();
    try {
        AdminPanel(AdminApp)
    } catch (e) {
        console.error("[admin-panel] 初始化失败:", e.message)
    }
    try {
        AdminExport(AdminApp)
    } catch (e) {
        console.error("[admin-export] 初始化失败:", e.message)
    }
    try {
        AdminViews(AdminApp)
    } catch (e) {
        console.error("[admin-views] 初始化失败:", e.message)
    }
    AlertSystem.init(),
        function() {
            function e() {
                n.darkMode = $("#settingDarkMode").checked, n.fontSize = $("#settingFontSize").value, n.refreshRate = parseInt($("#settingRefreshRate").value, 10), n.notifications.push = $("#settingPush").checked, n.notifications.sms = $("#settingSMS").checked, n.notifications.email = $("#settingEmail").checked, saveJSON(t, n), i(n), y()
            }
            $("#settingDarkMode").addEventListener("change", function() {
                var e = this.checked;
                document.body.classList.toggle("light", !e), document.body.classList.toggle("dark", e), n.darkMode = e, saveJSON(t, n)
            }), $("#settingFontSize").addEventListener("change", e), $("#settingRefreshRate").addEventListener("change", e), $("#settingPush").addEventListener("change", e), $("#settingSMS").addEventListener("change", e), $("#settingEmail").addEventListener("change", e)
        }(), S(),
        function e() {
            var t = document.getElementById("adminClock");
            if (t) {
                var n = new Date,
                    a = String(n.getHours()).padStart(2, "0"),
                    i = String(n.getMinutes()).padStart(2, "0");
                t.textContent = a + ":" + i, setTimeout(e, 3e4)
            }
        }(), e.refreshSummary = S
}();
