! function(e) {
    "use strict";
    var t;
    if ("undefined" != typeof module && module.exports) {
        t = [];
        for (var r = 1; r <= 12; r++) t.push(require("../data/residents/" + r + ".js"))
    } else {
        var o = e.RESIDENT_DATA || {};
        t = Object.keys(o).map(function(e) {
            return o[e]
        }).sort(function(e, t) {
            return e.id - t.id
        })
    }
    // 统一按数据契约补全/强类型（schema.js 存在时生效，缺失则原样透传，保证向后兼容）
    if ("undefined" != typeof ResidentSchema && ResidentSchema.normalize) {
        t = t.map(function(r) {
            return ResidentSchema.normalize(r)
        })
    } else if ("undefined" != typeof e.ResidentSchema && e.ResidentSchema.normalize) {
        t = t.map(function(r) {
            return e.ResidentSchema.normalize(r)
        })
    }
    "undefined" != typeof module && module.exports ? module.exports = {
        PATIENTS: t
    } : e.SeedData = {
        PATIENTS: t
    }
}(this);