var Audit = function() {
    "use strict";
    var t = "hm-audit";

    function a() {
        try {
            return loadJSON(t, [])
        } catch (t) {
            return []
        }
    }
    return {
        log: function(n, r) {
            try {
                var c = a();
                c.push({
                    ts: (new Date).toLocaleString("zh-CN"),
                    action: n,
                    detail: r || ""
                }), c.length > 200 && (c = c.slice(-200)), saveJSON(t, c)
            } catch (t) {}
        },
        all: a
    }
}();