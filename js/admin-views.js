window.AdminViews = function(e) {
    "use strict";
    var t = e.avatarHTML,
        n = e.sl,
        a = !1,
        i = null;

    function r() {
        "map" === e.currentView && API.getLocations().then(function(e) {
            CommunityMap.update(e);
            var t = {
                normal: 0,
                warning: 0,
                danger: 0,
                out: 0
            };
            e.forEach(function(e) {
                "out" === e.fenceStatus ? t.out++ : "danger" === e.status ? t.danger++ : "warning" === e.status ? t.warning++ : t.normal++
            });
            var n = function(e, t) {
                var n = getEl(e);
                n && (n.textContent = t)
            };
            n("msNormal", t.normal), n("msWarn", t.warning), n("msDanger", t.danger), n("msOut", t.out);
            var a = getEl("mapAlert"),
                i = getEl("mapAlertList");
            if (a && i)
                if (t.out > 0) {
                    var r = e.filter(function(e) {
                        return "out" === e.fenceStatus
                    }).map(function(e) {
                        return e.name
                    });
                    i.textContent = r.slice(0, 3).join("、") + (r.length > 3 ? " 等 " + r.length + " 人" : ""), a.hidden = !1
                } else a.hidden = !0;
            var s = t.out;
            $("#mapFoot").textContent = "在线 " + e.length + " 人" + (s ? " · ⚠ " + s + " 人越界" : "") + " · 更新于 " + (new Date).toLocaleTimeString("zh-CN");
            return API.getAlerts().then(alerts=>AlertSystem.sync(alerts))
        }).catch(function(e) {
            console.error("[Admin] 地图数据加载失败:", e.message);
            var t = getEl("mapFoot");
            t && (t.textContent = "地图数据加载失败，请稍后重试")
        })
    }

    function s(a, i) {
        var r = $("#browseGrid"),
            s = $("#browseEmpty");
        r && s && function(e, t) {
            var n = void 0 !== e ? e : $("#browseSearch").value.toLowerCase(),
                a = void 0 !== t ? t : $("#browseFilter").value;
            return API.getPatients().then(function(e) {
                var t = e;
                return n && (t = t.filter(function(e) {
                    return e.name.indexOf(n) >= 0
                })), "admitted" === a ? t = t.filter(function(e) {
                    return e.admitted
                }) : "all" !== a && (t = t.filter(function(e) {
                    return !e.admitted && e.status === a
                })), t
            })
        }(a, i).then(function(a) {
            if (!a.length) return r.innerHTML = "", void(s.hidden = !1);
            s.hidden = !0, r.innerHTML = a.map(function(e) {
                var a = Metrics.statusBadge(e.status),
                    i = e.admitted ? '<span class="badge badge-admitted">住院中</span>' : '<span class="badge ' + a + '">' + n(e.status) + "</span>";
                return '<div class="browse-card" data-pid="' + e.id + '"><div class="card-avatar">' + t(e.id, e.gender, "avatar-emoji") + '</div><div class="card-name">' + escapeHtml(e.name) + '</div><div class="card-meta">' + escapeHtml(e.gender) + " · " + e.age + '岁</div><div class="card-status">' + i + "</div></div>"
            }).join(""), r.querySelectorAll(".browse-card").forEach(function(t) {
                t.addEventListener("click", function() {
                    e.openDrawer && e.openDrawer(parseInt(t.dataset.pid, 10))
                })
            })
        }).catch(function(e) {
            console.error("[Admin] 档案加载失败:", e.message), r.innerHTML = "", s.hidden = !1, s.textContent = "档案加载失败，请稍后重试"
        })
    }
    var o = null;

    function d() {
        e.avatars._profile_ || (e.avatars._profile_ = avatarSVGForRole("nurse")), applyAvatarImg($("#profileAvatarImg"), $("#profileAvatarFallback"), e.avatars._profile_)
    }

    function c() {
        var t = $("#admissionRecords"),
            n = e.admissionRecords.filter(function(e) {
                return "admitted" === e.status
            });
        n.length ? t.innerHTML = n.map(function(e) {
            return '<div class="record-item"><strong>' + escapeHtml(e.patientName || "未知") + '</strong><div class="record-meta">🏥 ' + escapeHtml(e.hospital || "未填写") + " · " + escapeHtml(e.admissionDate || "未填写") + ' 入院</div><div class="mt-4">' + escapeHtml(e.reason || "未填写") + "</div>" + (e.notify ? '<div class="fs11-blue mt-2">演示通知已记录，未实际发送</div>' : "") + "</div>"
        }).join("") : t.innerHTML = '<p class="empty">暂无</p>'
    }

    function l() {
        var n = $("#admittedList"),
            a = e.admissionRecords.filter(function(e) {
                return "admitted" === e.status
            });
        if (!a.length) return n.innerHTML = '<p class="empty">暂无</p>', void($("#dischargeForm").hidden = !0);
        n.innerHTML = a.map(function(e) {
            var n = (PATIENTS.find(function(t) {
                return t.id === e.patientId
            }) || {}).gender || "男";
            return '<div class="admitted-card" data-pid="' + e.patientId + '"><div class="card-avatar">' + t(e.patientId, n, "avatar-emoji") + '</div><div class="admitted-info"><div class="admitted-name">' + escapeHtml(e.patientName || "未知") + '</div><div class="admitted-detail">🏥 ' + escapeHtml(e.hospital || "未填写") + " · " + escapeHtml(e.admissionDate || "未填写") + " · " + escapeHtml(e.reason || "未填写") + "</div></div></div>"
        }).join(""), n.querySelectorAll(".admitted-card").forEach(function(t) {
            t.addEventListener("click", function() {
                n.querySelectorAll(".admitted-card").forEach(function(e) {
                    e.classList.remove("selected")
                }), t.classList.add("selected"), e.selDisPid = parseInt(t.dataset.pid, 10), $("#dischargeForm").hidden = !1, $("#dischargeDate").value = (new Date).toISOString().slice(0, 10), $("#dischargeSummary").value = "", $("#dischargeHint").textContent = ""
            })
        })
    }

    function m() {
        var t = $("#dischargeRecords"),
            n = e.admissionRecords.filter(function(e) {
                return "discharged" === e.status
            });
        n.length ? t.innerHTML = n.slice().reverse().map(function(e) {
            return '<div class="record-item"><strong>' + escapeHtml(e.patientName || "未知") + '</strong><div class="record-meta">入院 ' + escapeHtml(e.admissionDate || "未填写") + " → 出院 " + escapeHtml(e.dischargeDate || "未填写") + " · " + escapeHtml(e.hospital || "未填写") + '</div><div class="mt-2">入院：' + escapeHtml(e.reason || "未填写") + "</div>" + (e.dischargeSummary ? '<div class="mt-2">小结：' + escapeHtml(e.dischargeSummary) + "</div>" : "") + "</div>"
        }).join("") : t.innerHTML = '<p class="empty">暂无</p>'
    }

    function u() {
        $("#auditList") && function(e) {
            var t = $("#auditList");
            t && (e && e.length ? t.innerHTML = "<table><thead><tr><th>时间</th><th>操作</th><th>详情</th></tr></thead><tbody>" + e.map(function(e) {
                return "<tr><td>" + escapeHtml(e.ts || e.created_at || "") + "</td><td>" + escapeHtml(e.action || "") + "</td><td>" + escapeHtml(e.detail || "") + "</td></tr>"
            }).join("") + "</tbody></table>" : t.innerHTML = '<p class="empty">暂无审计记录</p>')
        }(Audit.all())
    }
    $("#browseSearch").addEventListener("input", function() {
        clearTimeout(o), o = setTimeout(s, 250)
    }), $("#browseFilter").addEventListener("change", function() {
        s()
    }), $("#profileAvatarWrap").addEventListener("click", function() {
        $("#avatarFileInput").click()
    }), $("#avatarFileInput").addEventListener("change", function() {
        readImageFile(this.files[0], function(t) {
            e.avatars._profile_ = t, AvatarStore.save("_profile_", t).catch(function(e) {
                console.warn("[Avatar] 管理员头像保存失败:", e.message), showToast("头像保存失败，但当前页面仍可使用")
            }), d()
        })
    }), $("#profileName").addEventListener("change", function() {
        e.profile.name = this.value, saveJSON(KEYS.PROFILE_ADMIN, e.profile)
    }), $("#profileRole").addEventListener("change", function() {
        e.profile.role = this.value, saveJSON(KEYS.PROFILE_ADMIN, e.profile)
    }), $("#btnAdmit").addEventListener("click", function() {
        var t = $("#admissionHint"),
            n = parseInt($("#admissionPatient").value, 10),
            a = $("#admissionHospital").value.trim(),
            i = $("#admissionReason").value.trim(),
            r = $("#admissionDate").value,
            s = $("#admissionNotify").checked;
        n ? a ? r ? API.getPatients().then(function(e) {
            var t = "";
            return e.some(function(e) {
                if (e.id === n) return t = e.name, !0
            }), t
        }).then(function(e) {
            var t = {
                patientId: n,
                patientName: e,
                hospital: a,
                reason: i,
                admissionDate: r,
                status: "admitted",
                notify: s,
                id: Date.now()
            };
            return API.admitPatient(n, t).then(function() {
                return s && API.sendSMS("主治医生", "【入院通知】" + e + " 已办理入院（" + a + "），请关注。"), t
            })
        }).then(function(n) {
            e.admissionRecords.push(n), Audit.log("入院", n.patientName || ""), saveJSON(KEYS.ADMISSIONS, e.admissionRecords), t.textContent = "✅ " + (n.patientName || "") + " 已入院", $("#admissionPatient").value = "", $("#admissionHospital").value = "", $("#admissionReason").value = "", c(), "summary" === e.currentView && e.refreshSummary && e.refreshSummary()
        }).catch(function(e) {
            console.error("[Admin] 入院操作失败:", e.message), t.textContent = "入院操作失败，请重试"
        }) : t.textContent = "请选择日期" : t.textContent = "请填写医院" : t.textContent = "请选择居民"
    }), $("#btnDischarge").addEventListener("click", function() {
        var t = $("#dischargeHint"),
            n = $("#dischargeDate").value,
            a = $("#dischargeSummary").value.trim();
        e.selDisPid ? n ? API.dischargePatient(e.selDisPid, {}).then(function() {
            Audit.log("出院", "居民id=" + e.selDisPid);
            var i = e.admissionRecords.findIndex(function(t) {
                return t.patientId === e.selDisPid && "admitted" === t.status
            });
            i >= 0 && (e.admissionRecords[i].status = "discharged", e.admissionRecords[i].dischargeDate = n, e.admissionRecords[i].dischargeSummary = a), saveJSON(KEYS.ADMISSIONS, e.admissionRecords), t.textContent = "✅ 已出院", e.selDisPid = null, $("#dischargeForm").hidden = !0, l(), m(), "summary" === e.currentView && e.refreshSummary && e.refreshSummary()
        }).catch(function(e) {
            console.error("[Admin] 出院操作失败:", e.message), t.textContent = "出院操作失败，请重试"
        }) : t.textContent = "请选择日期" : t.textContent = "请先选择一位住院居民"
    }), e.startMapView = function() {
        a || (CommunityMap.init("mapContainer", function(t) {
            e.openDrawer && e.openDrawer(t)
        }), a = !0), r(), clearInterval(i), i = setInterval(r, 2e3)
    }, e.stopMapView = function() {
        clearInterval(i), i = null
    }, e.refreshMap = r;
    var f = document.getElementById("btnAuditRefresh");
    f && f.addEventListener("click", u), e.initAuditView = u, e.renderBrowseView = s, e.initProfileView = function() {
        $("#profileName").value = e.profile.name, $("#profileRole").value = e.profile.role, d()
    }, e.applyProfileAvatar = d, e.initAdmissionView = function() {
        $("#admissionDate").value = (new Date).toISOString().slice(0, 10), API.getPatients().then(function(e) {
            $("#admissionPatient").innerHTML = '<option value="">-- 请选择 --</option>' + e.filter(function(e) {
                return !e.admitted
            }).map(function(e) {
                return '<option value="' + e.id + '">' + escapeHtml(e.name) + " · " + escapeHtml(e.gender) + " " + e.age + "岁</option>"
            }).join("")
        }).catch(function(e) {
            console.error("[Admin] 入院居民加载失败:", e.message), $("#admissionHint").textContent = "居民列表加载失败"
        }), c()
    }, e.renderAdmissionRecords = c, e.renderAdmittedList = l, e.renderDischargeRecords = m, e.initDischargeView = function() {
        l(), m()
    }
};
