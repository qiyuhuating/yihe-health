/* ===========================================================================
 * metrics.js — 健康指标区间与状态判定
 * ---------------------------------------------------------------------------
 * ⚠️ RANGES 里的数值是**演示基线，不是诊断标准**。
 *
 * 它们来自通用体检参考区间，用于让演示数据产生"正常/异常/危险"三态，
 * 从而驱动界面上的状态徽标与模拟告警。**这些数值未经医学审核**，
 * 不同标准（教科书 / 诊疗指南 / 不同年龄段）取值并不相同。
 *
 * 真实落地前必须做的事（属临床决策，需专业人员参与，不要由前端自行改数）：
 *   1. 按年龄 / 性别 / 慢病分层，而不是所有老人共用一套区间
 *   2. 与机构采用的诊断标准逐项对齐，并记录来源与版本
 *   3. 明确"提示复测"与"提示就医"的阈值差异
 *   4. 单次越界与持续越界要分开处理，避免噪声告警
 *
 * 现状：RANGES 是模块内常量，未提供运行时覆盖入口。
 *      如需按机构配置外置，建议新增 data/thresholds.js（与 residents.js
 *      同为演示数据层），而不是在页面上硬编码。
 *
 * evaluateStatus() 另有一层"与个人基线比较"的逻辑（warnPct / dangerPct），
 * 同样属于演示取值。
 * =========================================================================== */
! function(e) {
    var r = function() {
        var e = {
            heartRate: {
                min: 60,
                max: 100,
                dangerLow: 42,
                dangerHigh: 125
            },
            bloodOxygen: {
                min: 95,
                max: 100,
                dangerLow: 85,
                dangerHigh: 999
            },
            temperature: {
                min: 36,
                max: 37.2,
                dangerLow: 35,
                dangerHigh: 39
            },
            systolic: {
                min: 90,
                max: 140,
                dangerLow: 80,
                dangerHigh: 185
            },
            diastolic: {
                min: 60,
                max: 90,
                dangerLow: 50,
                dangerHigh: 115
            },
            bloodSugar: {
                min: 3.9,
                max: 6.1,
                dangerLow: 2.5,
                dangerHigh: 10
            }
        };

        function r(r, a) {
            var n = e[r];
            if (!n || !Number.isFinite(a)) return "unknown";
            return n ? a <= n.dangerLow || a >= n.dangerHigh ? "danger" : a < n.min || a > n.max ? "warning" : "normal" : "normal"
        }

        function a(r, a) {
            var n = e[r];
            return !!n && Number.isFinite(a) && (a <= n.dangerLow || a >= n.dangerHigh)
        }
        var n = [{
            name: "清晨",
            from: 6,
            to: 9
        }, {
            name: "上午",
            from: 9,
            to: 12
        }, {
            name: "中午",
            from: 12,
            to: 14
        }, {
            name: "下午",
            from: 14,
            to: 18
        }, {
            name: "晚间",
            from: 18,
            to: 22
        }];
        return {
            RANGES: e,
            metricStatus: r,
            isLifeThreat: a,
            alertMessage: function(e) {
                if (!e) return "";
                var r = [];
                return a("heartRate", e.heartRate) && r.push("心率" + Number(e.heartRate).toFixed(0) + "bpm"), a("bloodOxygen", e.bloodOxygen) && r.push("血氧" + Number(e.bloodOxygen).toFixed(0) + "%"), a("temperature", e.temperature) && r.push("体温" + Number(e.temperature).toFixed(1) + "°C"), a("systolic", e.systolic) && r.push("收缩压" + Number(e.systolic).toFixed(0)), a("diastolic", e.diastolic) && r.push("舒张压" + Number(e.diastolic).toFixed(0)), a("bloodSugar", e.bloodSugar) && r.push("血糖" + Number(e.bloodSugar).toFixed(1)), r.join("，")
            },
            evaluateStatus: function(e, a) {
                if (!e || !a) return "unknown";
                var n = !1,
                    t = !1,
                    missing = !1;
                return [{
                    key: "heartRate",
                    warnPct: .2,
                    dangerPct: .35
                }, {
                    key: "bloodOxygen",
                    warnPct: .05,
                    dangerPct: .1
                }, {
                    key: "temperature",
                    warnPct: .05,
                    dangerPct: .08
                }, {
                    key: "systolic",
                    warnPct: .15,
                    dangerPct: .25
                }, {
                    key: "diastolic",
                    warnPct: .15,
                    dangerPct: .25
                }, {
                    key: "bloodSugar",
                    warnPct: .2,
                    dangerPct: .4
                }].forEach(function(o) {
                    var i = r(o.key, e[o.key]);
                    if (i === "unknown") { missing = !0; return; }
                    if ("danger" !== i)
                        if ("warning" !== i) {
                            var d = a[o.key];
                            if ("number" == typeof e[o.key] && "number" == typeof d) {
                                var g = Math.abs(e[o.key] - d) / Math.max(d, 1);
                                g > 1.5 * o.dangerPct ? n = !0 : g > o.warnPct && (t = !0)
                            }
                        } else t = !0;
                    else n = !0
                }), n ? "danger" : t ? "warning" : missing ? "unknown" : "normal"
            },
            SLOTS: n,
            slotOf: function(e) {
                for (var r = 0; r < n.length; r++)
                    if (e >= n[r].from && e < n[r].to) return r;
                return 0
            },
            statusText: function(e) {
                return "normal" === e ? "正常" : "warning" === e ? "异常" : "danger" === e ? "危险" : "—"
            },
            statusBadge: function(e) {
                return "warning" === e ? "badge-warning" : "danger" === e ? "badge-danger" : "badge-normal"
            }
        }
    }();
    "undefined" != typeof module && module.exports ? module.exports = r : e.Metrics = r
}(this);
