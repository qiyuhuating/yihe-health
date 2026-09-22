var AudioUtils = function() {
    var e = null,
        n = null;

    function t() {
        if (!e) try {
            e = new(window.AudioContext || window.webkitAudioContext)
        } catch (e) {}
        return e
    }

    function i() {
        var e = t();
        if (e && "suspended" === e.state) try {
            e.resume()
        } catch (e) {}
    }
    var a = !0,
        r = [];

    function c() {
        try {
            r = window.speechSynthesis.getVoices() || []
        } catch (e) {}
    }
    return window.speechSynthesis && (c(), window.speechSynthesis.onvoiceschanged = c), {
        warmup: function() {
            var e = t();
            if (e && "suspended" === e.state) try {
                e.resume()
            } catch (e) {}
        },
        startAlarm: function() {
            if (a) {
                a = !1;
                var e = t();
                e ? (i(), function t() {
                    if (!a) {
                        var i = e.currentTime;
                        r(400, i, .4, .18), r(600, i + .4, .4, .18), r(400, i + .8, .4, .18), r(600, i + 1.2, .4, .18), n = setTimeout(t, 1600)
                    }
                }()) : a = !0
            }

            function r(n, t, i, a) {
                var r = e.createOscillator(),
                    c = e.createGain();
                r.type = "sine", r.frequency.value = n, c.gain.setValueAtTime(a, t), c.gain.linearRampToValueAtTime(.001, t + i - .02), r.connect(c), c.connect(e.destination), r.start(t), r.stop(t + i)
            }
        },
        stopAlarm: function() {
            if (a = !0, n && (clearTimeout(n), n = null), e) try {
                e.suspend()
            } catch (n) {
                try {
                    e.close()
                } catch (e) {}
            }
        },
        speak: function(e, n, t) {
            try {
                var i = new SpeechSynthesisUtterance(e),
                    a = function() {
                        c();
                        var e, n = [];
                        for (e = 0; e < r.length; e++) r[e].lang && 0 === r[e].lang.toLowerCase().indexOf("zh") && n.push(r[e]);
                        if (!n.length) return null;
                        for (var t = ["natural", "online", "xiaoxiao", "yaoyao", "xiaoyi", "yunxi", "yunjian", "kangkang", "tingting", "huihui"], i = 0; i < t.length; i++)
                            for (var a = 0; a < n.length; a++)
                                if (n[a].name && n[a].name.toLowerCase().indexOf(t[i]) >= 0) return n[a];
                        return n[0]
                    }();
                a && (i.voice = a), i.lang = n || a && a.lang || "zh-CN", i.rate = t || 1, i.pitch = 1.08, i.volume = 1, window.speechSynthesis.speak(i)
            } catch (e) {}
        },
        stopSpeak: function() {
            try {
                window.speechSynthesis.cancel()
            } catch (e) {}
        },
        ensureResumed: i
    }
}();