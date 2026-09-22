! function() {
    var t = Metrics.RANGES,
        e = (Metrics.metricStatus, Metrics.isLifeThreat, Metrics.alertMessage),
        a = Metrics.evaluateStatus,
        r = {
            x1: 40,
            y1: 40,
            x2: 760,
            y2: 560
        },
        n = [{
            key: "b1",
            name: "1号楼",
            x: 150,
            y: 125
        }, {
            key: "b2",
            name: "2号楼",
            x: 330,
            y: 125
        }, {
            key: "b3",
            name: "3号楼",
            x: 150,
            y: 265
        }, {
            key: "b5",
            name: "5号楼",
            x: 610,
            y: 125
        }, {
            key: "b6",
            name: "6号楼",
            x: 610,
            y: 265
        }, {
            key: "b7",
            name: "7号楼",
            x: 150,
            y: 450
        }, {
            key: "garden",
            name: "中心花园",
            x: 400,
            y: 275
        }, {
            key: "clinic",
            name: "卫生站",
            x: 505,
            y: 450
        }, {
            key: "activity",
            name: "活动室",
            x: 290,
            y: 450
        }, {
            key: "gate",
            name: "社区大门",
            x: 400,
            y: 552
        }],
        o = {
            1: "b1",
            2: "b1",
            3: "b2",
            4: "b2",
            5: "b3",
            6: "b3",
            7: "b5",
            8: "b5",
            9: "b6",
            10: "b6",
            11: "b7",
            12: "b7"
        },
        i = SeedData.PATIENTS,
        s = JSON.parse(JSON.stringify(i)),
        c = {};
    i.forEach(function(t) {
        c[t.id] = t
    }), s.forEach(function(t) {
        t.status = a(t, c[t.id]), t.admitted = t.admitted || !1
    });
    var l = !0,
        u = {},
        d = {};

    function m(t) {
        return d[t] || null
    }

    function g(t, e) {
        return t + (2 * Math.random() - 1) * e
    }

    function y(t) {
        var e = null,
            a = 1 / 0;
        return n.forEach(function(r) {
            var n = Math.hypot(t.x - r.x, t.y - r.y);
            n < a && (a = n, e = r)
        }), e ? a < 55 ? "在" + e.name : e.name + "附近" : "社区内"
    }

    function f(t, e) {
        return t + Math.random() * (e - t)
    }

    function h(t, e, a) {
        return Math.min(Math.max(t, e), a)
    }

    function p(t) {
        return new Promise(function(e) {
            setTimeout(e, t)
        })
    }

    function v(t) {
        return Math.random() < t
    }
    n.forEach(function(t) {
        d[t.key] = t
    }), s.forEach(function(t) {
        var e = m(o[t.id]) || n[0];
        t.home = {
            x: g(e.x, 18),
            y: g(e.y, 14),
            name: e.name
        };
        var a = Math.random() < .5 ? e : n[Math.floor(Math.random() * n.length)];
        t.pos = {
            x: g(a.x, 20),
            y: g(a.y, 16)
        }, t.moveTarget = null, t.trail = [], t.fenceStatus = "in", t.speed = .3 + 2.6 * (t.mobility || .5) + .3 * Math.random()
    });
    var S = !1;
    try {
        localStorage.setItem("__cw_ls_t", "1"), localStorage.removeItem("__cw_ls_t"), S = !0
    } catch (t) {
        S = !1
    }
    var b = "cw_sim_store_v2",
        x = "cw_sim_leader_v1";
    try {
        localStorage.getItem("cw_sim_store_v1") && localStorage.removeItem("cw_sim_store_v1")
    } catch (t) {}
    var M = null,
        w = null,
        O = String(Math.random()).slice(2) + Date.now().toString(36),
        _ = [];

    function I() {
        if (S) try {
            localStorage.setItem(b, JSON.stringify(SimStore.buildStore(s, u, {
                v: Date.now(),
                from: O
            })))
        } catch (t) {
            console.warn("[sim] 写入共享单据失败（可能 localStorage 配额已满）", t.message)
        }
    }

    function T(t) {
        SimStore.applyStore(s, u, t)
    }

    function R() {
        if (!S) return null;
        try {
            return JSON.parse(localStorage.getItem(x))
        } catch (t) {
            return null
        }
    }

    function D(t) {
        return !!t && Date.now() - t.ts < 6e3
    }

    function E() {
        if (!S) return !0;
        var t = R();
        return D(t) && t.id === O
    }
    S && (function() {
        if (!S) return !1;
        try {
            var t = localStorage.getItem(b);
            return !!t && (T(JSON.parse(t)), !0)
        } catch (t) {
            return !1
        }
    }() || I(), M = setInterval(function() {
        var t = R();
        D(t) && t.id !== O || function() {
            if (S) {
                var t = R();
                if (!D(t) || t.id === O) try {
                    localStorage.setItem(x, JSON.stringify({
                        id: O,
                        ts: Date.now()
                    }))
                } catch (t) {}
            }
        }()
    }, 2e3), w = setInterval(function() {
        if (S) try {
            var t = localStorage.getItem(b);
            if (!t) return;
            var e = JSON.parse(t);
            if (!e || e.from === O) return;
            T(e), _.forEach(function(t) {
                try {
                    t()
                } catch (t) {}
            })
        } catch (t) {}
    }, 800)), window.addEventListener("beforeunload", function() {
        if (S && E()) try {
            localStorage.removeItem(x)
        } catch (t) {}
        M && clearInterval(M), w && clearInterval(w)
    });
    var acknowledged = loadJSON("yihe-alert-ack", {});
    var H = {
        getPatients: async () => (await p(f(80, 200)), s.map(t => {
            var {
                medicalHistory: e,
                height: a,
                weight: r,
                trail: n,
                moveTarget: o,
                speed: i,
                ...s
            } = t;
            return {
                ...s,
                place: y(t.pos),
                admitted: !!u[t.id]
            }
        })),
        async getPatientDetail(t) {
            await p(f(80, 150));
            var e = s.find(e => e.id === t);
            return e ? {
                ...e,
                place: y(e.pos),
                admitted: !!u[e.id],
                medicalHistory: {
                    ...e.medicalHistory
                }
            } : null
        },
        async getAlerts() {
            await p(f(50, 150));
            var t = s.filter(t => "danger" === t.status && !u[t.id]).map(t => ({
                    id: t.id,
                    name: t.name,
                    type: "health",
                    status: t.status,
                    alertMsg: t.alertMsg,
                    place: y(t.pos)
                })),
                e = s.filter(t => "out" === t.fenceStatus && !u[t.id]).map(t => ({
                    id: t.id,
                    name: t.name,
                    type: "fence",
                    status: "danger",
                    alertMsg: "走失预警：居民可能已走失",
                    place: y(t.pos)
                }));
            return t.concat(e).filter(alert => !acknowledged[alert.id + "|" + alert.type + "|" + alert.alertMsg])
        },
        async resolveAlert(id) {
            const alerts = await H.getAlerts();
            alerts.filter(alert => alert.id === id).forEach(alert => acknowledged[alert.id + "|" + alert.type + "|" + alert.alertMsg] = new Date().toISOString());
            saveJSON("yihe-alert-ack", acknowledged);
            return { success:true, acknowledged:true };
        },
        async simulateDanger() {
            await p(100);
            var a = s.filter(t => "danger" !== t.status && !u[t.id]);
            if (!a.length) return {
                success: !1,
                reason: "没有可模拟的居民"
            };
            var r = a[Math.floor(Math.random() * a.length)],
                n = [() => {
                    r.heartRate = f(t.heartRate.dangerHigh, 135)
                }, () => {
                    r.heartRate = f(35, t.heartRate.dangerLow)
                }, () => {
                    r.bloodOxygen = f(78, t.bloodOxygen.dangerLow)
                }, () => {
                    r.temperature = f(t.temperature.dangerHigh, 39.5)
                }, () => {
                    r.temperature = f(34, t.temperature.dangerLow)
                }, () => {
                    r.systolic = f(t.systolic.dangerHigh, 195)
                }, () => {
                    r.bloodSugar = f(t.bloodSugar.dangerHigh, 11)
                }, () => {
                    r.bloodSugar = f(2, t.bloodSugar.dangerLow)
                }];
            return n[Math.floor(Math.random() * n.length)](), r.status = "danger", r.alertMsg = e(r), I(), {
                success: !0,
                patient: {
                    id: r.id,
                    name: r.name,
                    type: "health",
                    status: r.status,
                    alertMsg: r.alertMsg,
                    place: y(r.pos)
                }
            }
        },
        async simulateWander() {
            await p(100);
            var t = s.filter(t => "out" !== t.fenceStatus && !u[t.id]);
            if (!t.length) return {
                success: !1,
                reason: "没有可模拟的居民"
            };
            var e = t[Math.floor(Math.random() * t.length)];
            return e.pos.x = r.x1 + f(20, 50), e.pos.y = 565 + f(5, 30), e.fenceStatus = "out", e.moveTarget = null, I(), {
                success: !0,
                patient: {
                    id: e.id,
                    name: e.name
                }
            }
        },
        async simulateDangerFor(a) {
            await p(60);
            var r = s.find(t => t.id === a);
            return r ? (r.heartRate = f(t.heartRate.dangerHigh, 135), r.status = "danger", r.alertMsg = e(r), {
                success: !0,
                patient: {
                    id: r.id,
                    name: r.name,
                    alertMsg: r.alertMsg,
                    place: y(r.pos)
                }
            }) : {
                success: !1,
                reason: "居民不存在"
            }
        },
        getLocations: async () => (await p(f(50, 120)), s.filter(t => !u[t.id]).map(t => ({
            id: t.id,
            name: t.name,
            gender: t.gender,
            x: Math.round(t.pos.x),
            y: Math.round(t.pos.y),
            place: y(t.pos),
            home: t.home.name,
            fenceStatus: t.fenceStatus,
            status: t.status,
            trail: t.trail.slice(-12)
        }))),
        sendSMS: async (t, e) => (await p(300), console.log("[模拟短信] 发送至 " + t + "：" + e), {
            success: !0,
            simulated: !0,
            to: t,
            content: e
        }),
        async updateMedical(t, e, a) {
            await p(50);
            var r = s.find(function(e) {
                return e.id === t
            });
            return r ? (r.medicalHistory || (r.medicalHistory = {}), r.medicalHistory[e] = a, I(), {
                success: !0
            }) : {
                success: !1
            }
        },
        admitPatient: async (t, e) => (await p(100), u[t] = !0, I(), {
            success: !0,
            record: {
                ...e,
                patientId: t,
                id: Date.now()
            }
        }),
        async dischargePatient(t, e) {
            await p(100), delete u[t];
            var r = c[t],
                n = s.find(function(e) {
                    return e.id === t
                });
            return n && r && (Object.assign(n, {
                heartRate: r.heartRate,
                bloodOxygen: r.bloodOxygen,
                temperature: r.temperature,
                systolic: r.systolic,
                diastolic: r.diastolic,
                bloodSugar: r.bloodSugar
            }), n.status = a(n, r), n.alertMsg = null), I(), {
                success: !0,
                record: {
                    ...e,
                    patientId: t,
                    id: Date.now()
                }
            }
        },
        setEnabled(t) {
            l = t
        },
        onExternalSync(t) {
            "function" == typeof t && _.push(t)
        },
        tick: function() {
            if (l && E()) {
                var t = (new Date).getHours();
                s.forEach(function(n) {
                    if (!u[n.id]) {
                        var o = c[n.id];
                        if (o) {
                            var i = o.slots && o.slots[Metrics.slotOf(t)] || o,
                                s = n.moveTarget && Math.hypot(n.moveTarget.x - n.pos.x, n.moveTarget.y - n.pos.y) > 6,
                                l = i.heartRate + (s ? 1.5 : 0),
                                d = i.systolic + (s ? 1 : 0),
                                y = i.diastolic,
                                p = i.bloodOxygen,
                                S = i.temperature,
                                b = i.bloodSugar;
                            n.heartRate = h(n.heartRate + .12 * (l - n.heartRate) + f(-.3, .3), 50, 130), n.systolic = h(n.systolic + .1 * (d - n.systolic) + f(-.4, .4), 85, 180), n.diastolic = h(n.diastolic + .1 * (y - n.diastolic) + f(-.3, .3), 55, 110), n.bloodOxygen = h(n.bloodOxygen + .1 * (p - n.bloodOxygen) + f(-.1, .1), 90, 100), v(.6) && (n.temperature = h(n.temperature + .1 * (S - n.temperature) + f(-.02, .02), 35.8, 37.6)), v(.5) && (n.bloodSugar = h(n.bloodSugar + .08 * (b - n.bloodSugar) + f(-.05, .05), 3.5, 9)), v(5e-5) && n.chronic && (n.chronic.indexOf("高血压") >= 0 && (n.systolic += f(2, 5), n.diastolic += f(1, 3)), n.chronic.indexOf("糖尿病") >= 0 && (n.bloodSugar = h(n.bloodSugar + f(.5, 1.5), 3.5, 9.5))), n.status = a(n, c[n.id]), n.alertMsg = "danger" === n.status ? e(n) || "健康指标显著异常，请尽快检查" : null,
                                function(t) {
                                    if (!u[t.id])
                                        if (t.trail.push({
                                                x: Math.round(t.pos.x),
                                                y: Math.round(t.pos.y)
                                            }), t.trail.length > 20 && t.trail.shift(), t.moveTarget) {
                                            var e = t.moveTarget.x - t.pos.x,
                                                a = t.moveTarget.y - t.pos.y,
                                                n = Math.hypot(e, a);
                                            if (n < 6) return t.moveTarget = null, void(t._rest = Math.floor(6 + 30 * (1 - (t.mobility || .5))));
                                            var o, i, s = Math.min(t.speed, n);
                                            t.pos.x += e / n * s, t.pos.y += a / n * s, t.pos.x = Math.round(10 * t.pos.x) / 10, t.pos.y = Math.round(10 * t.pos.y) / 10, t.fenceStatus = (o = t.pos, i = r, o.x >= i.x1 && o.x <= i.x2 && o.y >= i.y1 && o.y <= i.y2 ? "in" : "out")
                                        } else {
                                            if (t._rest > 0) return void t._rest--;
                                            var c = (new Date).getHours();
                                            if (c >= 22 || c <= 6) return;
                                            var l = t.mobility || .5;
                                            if (Math.random() < .14 * l * l * l * l) {
                                                var d = ["garden", "activity", "garden", "activity", "clinic", "b1", "b2", "b3", "b5", "b6", "b7"],
                                                    y = m(d[Math.floor(Math.random() * d.length)]);
                                                t.moveTarget = {
                                                    x: g(y.x, 26),
                                                    y: g(y.y, 22)
                                                }
                                            }
                                            if (t.chronic && t.chronic.indexOf("脑梗") >= 0 && Math.random() < 12e-7) {
                                                var f = m("gate");
                                                t.moveTarget = {
                                                    x: g(f.x, 50),
                                                    y: 565 + 30 * Math.random()
                                                }
                                            }
                                        }
                                }(n)
                        }
                    }
                }), I()
            }
        }
    };
    window.API = H, window.PATIENTS = i, window.LANDMARKS = n
}();