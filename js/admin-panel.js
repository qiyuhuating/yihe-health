window.AdminPanel = function(e) {
    "use strict";
    var a = e.sl,
        t = Metrics.metricStatus;

    function s() {
        e.selRow && (e.selRow.classList.remove("selected"), e.selRow = null)
    }

    function i() {
        var a = e.selRow;
        a && a.focus(), $("#sidePanelOverlay").classList.remove("open"), $("#sidePanel").classList.remove("open"), s()
    }

    function l(e, a) {
        return '<div class="med-row"><span class="med-lbl">' + escapeHtml(e) + '</span><span class="med-val">' + escapeHtml(a) + "</span></div>"
    }

    function n(s) {
        var i = function(a, s) {
                var i = a.medicalHistory || {};

                function l(e) {
                    return void 0 !== s[e] ? s[e] : i[e] || "—"
                }

                function n(e) {
                    return "number" == typeof e && isFinite(e) ? e.toFixed(1) : "—"
                }
                var d = a.height && a.weight ? (a.weight / Math.pow(a.height / 100, 2)).toFixed(1) : null,
                    c = a.height && a.weight ? a.height + "cm · " + a.weight + "kg · BMI " + d : "—";
                return {
                    id: a.id,
                    name: a.name,
                    gender: a.gender,
                    age: a.age,
                    chronic: a.chronic,
                    status: a.status,
                    admitted: a.admitted,
                    alertMsg: a.alertMsg,
                    fenceStatus: a.fenceStatus,
                    place: a.place,
                    hasImg: e.avatars[a.id] || null,
                    vitals: [{
                        label: "心率",
                        val: n(a.heartRate),
                        unit: "bpm",
                        status: t("heartRate", a.heartRate)
                    }, {
                        label: "血氧",
                        val: n(a.bloodOxygen),
                        unit: "%",
                        status: t("bloodOxygen", a.bloodOxygen)
                    }, {
                        label: "体温",
                        val: n(a.temperature),
                        unit: "°C",
                        status: t("temperature", a.temperature)
                    }, {
                        label: "收缩压",
                        val: n(a.systolic),
                        unit: "mmHg",
                        status: t("systolic", a.systolic)
                    }, {
                        label: "舒张压",
                        val: n(a.diastolic),
                        unit: "mmHg",
                        status: t("diastolic", a.diastolic)
                    }, {
                        label: "血糖",
                        val: n(a.bloodSugar),
                        unit: "mmol/L",
                        status: t("bloodSugar", a.bloodSugar)
                    }],
                    info: [{
                        label: "血型",
                        val: l("bloodType")
                    }, {
                        label: "身高体重",
                        val: c
                    }, {
                        label: "吸烟史",
                        val: l("smoking")
                    }, {
                        label: "饮酒史",
                        val: l("alcohol")
                    }],
                    history: [{
                        label: "既往病史",
                        val: l("pastHistory"),
                        field: "pastHistory"
                    }, {
                        label: "过敏史",
                        val: l("allergies"),
                        field: "allergies"
                    }, {
                        label: "当前用药",
                        val: l("medications"),
                        field: "medications"
                    }, {
                        label: "手术史",
                        val: l("surgicalHistory"),
                        field: "surgicalHistory"
                    }, {
                        label: "家族病史",
                        val: l("familyHistory"),
                        field: "familyHistory"
                    }, {
                        label: "最近体检",
                        val: l("lastCheckup"),
                        field: "lastCheckup"
                    }]
                }
            }(s, e.medicalEdits[s.id] || {}),
            n = i.hasImg,
            c = n && (0 === n.indexOf("data:image/") || 0 === n.indexOf("avatars/")),
            r = i.admitted ? '<span class="p-status admitted">住院中</span><div class="fs12-t3 mt-4">监测已暂停</div>' : '<span class="p-status ' + escapeHtml(i.status) + '">' + escapeHtml(a(i.status)) + "</span>" + (i.alertMsg ? '<div class="fs12-red mt-4">' + escapeHtml(i.alertMsg) + "</div>" : ""),
            o = '<div class="hero"><div class="drawer-avatar" data-avpid="' + i.id + '">';
        o += c ? '<img class="show" src="' + escapeHtml(n) + '" alt="">' : "", n || (o += '<img src="' + avatarSVG(i.name, i.gender, 64) + '" width="64" height="64" alt="" class="br50">'), o += "</div>", o += '<div class="hero-info"><div class="hero-name">' + escapeHtml(i.name) + '</div><div class="hero-meta">' + escapeHtml(i.gender) + " · " + i.age + "岁" + (i.chronic ? '<br><span class="fs12-t2">' + escapeHtml(i.chronic) + "</span>" : "") + "</div>" + r + "</div></div>", o += '<div class="sec"><div class="sec-tit">当前指标</div><div class="vitals-grid">', i.vitals.forEach(function(e) {
            o += function(e, a, t, s) {
                return '<div class="' + (s && "normal" !== s ? "vital " + s : "vital") + '"><div class="vital-val">' + a + ' <span class="vital-unit">' + t + '</span></div><div class="vital-lbl">' + e + "</div></div>"
            }(e.label, e.val, e.unit, e.status)
        }), o += "</div></div>", o += '<div class="sec"><div class="sec-tit">基本信息</div>', o += l("当前位置", escapeHtml("out" === i.fenceStatus ? (i.place || "未知位置") + "（越界）" : i.place || "—")), i.info.forEach(function(e) {
            o += l(e.label, e.val)
        }), o += "</div>", o += '<div class="sec"><div class="sec-tit">病历信息<span class="edit-hint">点击右侧文字可编辑</span></div>', i.history.forEach(function(e) {
            var a, t;
            o += (a = e.label, t = e.val, '<div class="med-row"><span class="med-lbl">' + a + '</span><span class="med-val" contenteditable="true" data-pid="' + i.id + '" data-field="' + e.field + '">' + escapeHtml(t) + "</span></div>")
        }), o += "</div>", $("#panelBody").innerHTML = o;
        var v = $("#panelBody .drawer-avatar");
        v && v.addEventListener("click", function() {
            e.pendingPid = parseInt(v.dataset.avpid, 10), d.click()
        });
        for (var u = document.querySelectorAll("#panelBody [contenteditable]"), m = 0; m < u.length; m++)(function(a) {
            a.addEventListener("blur", function() {
                var a = this.dataset.field,
                    t = parseInt(this.dataset.pid, 10),
                    s = this.textContent.trim();
                e.medicalEdits[t] || (e.medicalEdits[t] = {}), e.medicalEdits[t][a] = s, saveJSON(KEYS.MEDICAL, e.medicalEdits), Audit.log("病历编辑", "居民" + t + " " + a), API.updateMedical(t, a, s).catch(function(e) {
                    console.warn("[Admin] 病历回写失败:", e.message)
                })
            })
        })(u[m])
    }
    var d = document.createElement("input");
    d.type = "file", d.accept = "image/*", d.hidden = !0, document.body.appendChild(d), d.addEventListener("change", function() {
        var a = e.pendingPid;
        a && readImageFile(this.files[0], function(t) {
            e.avatars[a] = t, AvatarStore.save(a, t).catch(function(e) {
                console.warn("[Avatar] 居民头像保存失败:", e.message), showToast("头像保存失败，但当前页面仍可使用")
            }), API.getPatientDetail(a).then(function(e) {
                e && n(e)
            }), "browse" === e.currentView && e.renderBrowseView && e.renderBrowseView()
        })
    }), $("#panelClose").addEventListener("click", i), $("#sidePanelOverlay").addEventListener("click", i), e.openDrawer = function(a, t) {
        s(), t && (t.classList.add("selected"), e.selRow = t);
        var i = getEl("panelBody");
        i && (i.innerHTML = '<p class="panel-empty">加载中…</p>', $("#sidePanelOverlay").classList.add("open"), $("#sidePanel").classList.add("open"), setTimeout(function() {
            $("#panelClose").focus()
        }, 100), API.getPatientDetail(a).then(function(e) {
            e ? n(e) : i.textContent = "未找到居民数据"
        }).catch(function(e) {
            console.error("[Admin] 居民详情加载失败:", e.message), i.textContent = "居民详情加载失败，请稍后重试"
        }))
    }, e.closeDrawer = i, e.renderPanel = n
};