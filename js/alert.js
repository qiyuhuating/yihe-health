var AlertSystem = {
    _modal: null,
    _queue: [],
    _current: null,
    _dismissedFor: {},
    _DISMISS_TTL: 18e5,
    _alertLog: [],
    init: function() {
        if (AudioUtils.warmup(), this._modal = document.getElementById("dangerModal"), this._modal) {
            var t = document.getElementById("btnAcknowledge"),
                e = document.getElementById("btnSendSMS");
            t ? t.addEventListener("click", this._dismiss.bind(this)) : console.error("[Alert] 未找到 #btnAcknowledge"), e ? e.addEventListener("click", this._sendSMS.bind(this)) : console.error("[Alert] 未找到 #btnSendSMS");
            var n = this;
            DataStore.get("alert-log").then(function(t) {
                t && (n._alertLog = t)
            }).catch(function(t) {
                console.warn("[Alert] 预警历史读取失败:", t.message)
            })
        } else console.error("[Alert] 未找到 #dangerModal，预警弹窗不可用")
    },
    _fp: function(t) {
        return t.id + "|" + (t.type || "health") + "|" + t.alertMsg
    },
    _isDismissed: function(t) {
        var e = this._dismissedFor[t];
        return !(!e || Date.now() - e >= this._DISMISS_TTL && (delete this._dismissedFor[t], 1))
    },
    show: function(t) {
        if (this._modal) {
            t.type || (t.type = "health");
            var e = this._fp(t);
            if (!(this._isDismissed(e) || this._current && this._fp(this._current) === e)) {
                for (var n = 0; n < this._queue.length; n++)
                    if (this._fp(this._queue[n]) === e) return;
                this._queue.push(t), this._current || this._next()
            }
        }
    },
    _next: function() {
        var t = this._queue.shift();
        if (t) {
            this._current = t;
            var e = "fence" === t.type,
                n = document.getElementById("modalIcon"),
                s = document.getElementById("modalTitle"),
                i = document.getElementById("modalPatientName"),
                r = document.getElementById("modalDetail"),
                o = document.getElementById("modalPlace"),
                l = document.getElementById("modalUrgent"),
                a = document.getElementById("modalQueue");
            if (!(n && s && i && r && o && l)) return console.error("[Alert] 弹窗 DOM 元素缺失，请检查 HTML"), void(this._current = null);
            n.textContent = e ? "🟣" : "🚨", s.textContent = e ? "走失预警" : "危险预警", i.textContent = t.name, r.textContent = (e ? "" : "异常指标：") + t.alertMsg, o.textContent = "📍 最后已知位置：" + (t.place || "未知"), l.textContent = e ? "居民可能已走失，请立即确认位置并安排人员寻找" : "居民健康指标触发危险预警，请立即联系医生处理", a && (this._queue.length > 0 ? (a.hidden = !1, a.textContent = "⚠ 还有 " + this._queue.length + " 条预警待处理") : a.hidden = !0), this._alertLog.push({
                id: t.id,
                name: t.name,
                type: t.type,
                alertMsg: t.alertMsg,
                place: t.place || "",
                time: (new Date).toISOString(),
                status: "triggered"
            }), this._alertLog.length > 100 && (this._alertLog = this._alertLog.slice(-100)), DataStore.set("alert-log", this._alertLog).catch(function(t) {
                console.warn("[Alert] 预警历史保存失败:", t.message)
            }), this._modal.hidden = !1;
            var d = document.getElementById("btnSendSMS");
            d && setTimeout(function() {
                d.focus()
            }, 100), this._playAlarm()
        } else this._current = null
    },
    _dismiss: function() {
        if (this._current) {
            var t = this._current,
                e = this._fp(t);
            this._dismissedFor[e] = Date.now();
            for (var n = this._alertLog.length - 1; n >= 0; n--)
                if (this._alertLog[n].id === t.id && this._alertLog[n].alertMsg === t.alertMsg && "triggered" === this._alertLog[n].status) {
                    this._alertLog[n].status = "resolved";
                    break
                } DataStore.set("alert-log", this._alertLog).catch(function(t) {
                console.warn("[Alert] 预警状态保存失败:", t.message)
            })
        }
        this._current = null, this._modal.hidden = !0, this._stopAlarm(), this._queue.length > 0 && this._next()
    },
    _sendSMS: function() {
        var t = this._current;
        if (t) {
            var e = "【紧急预警】" + t.name + " " + t.alertMsg + "，最后位置：" + (t.place || "未知") + "，请立即处理。",
                n = this;
            API.sendSMS("主治医生", e).then(function() {
                showToast("【模拟】短信已发送给医生！\n\n" + e + "\n\n（实际短信功能待后端接口对接）")
            }).catch(function(t) {
                console.error("[Alert] 模拟短信发送失败:", t.message), showToast("短信发送失败，请改用电话联系医生")
            }).finally(function() {
                n._dismiss()
            })
        } else this._dismiss()
    },
    _playAlarm: function() {
        AudioUtils.startAlarm();
        var t = this._current,
            e = t && "fence" === t.type ? "有居民可能走失，请立即处理" : "有居民触发危险预警，请立即处理";
        AudioUtils.speak(e)
    },
    _stopAlarm: function() {
        AudioUtils.stopAlarm(), AudioUtils.stopSpeak()
    }
};