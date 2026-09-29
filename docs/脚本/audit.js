/**
 * audit.js — 本机操作记录一致性校验
 *
 * 记录结构：{ ts, action, detail, actor, result, target, prev, hash }
 *   actor  操作者（管理端取当前登录管理员，个人端取登录居民；取不到记"未登录"）
 *   result success / failure / info
 *   target 操作对象（居民 id、导出范围等）
 *   prev   上一条记录的 hash
 *   hash   本条记录 + prev 的摘要
 *
 * 哈希链仅校验本机当前保留记录是否一致；本机数据可被整体替换。
 * 裁剪后的锚点随记录在同一次写入中更新，不证明已裁剪的历史。
 *
 * 兼容性：旧调用 Audit.log(action, detail) 仍可用，result 默认 "info"；
 * 升级前写入的旧记录没有 hash 字段，校验时视为链的新起点。
 */
var Audit = function() {
    "use strict";
    var KEY = "hm-audit";
    var MAX = 500;

    /* 同步摘要（FNV-1a）：审计写入必须同步完成，不能用异步的 crypto.subtle */
    function chainHash(rec, prev) {
        var s = [rec.ts, rec.action, rec.detail, rec.actor, rec.result, rec.target, prev].join("|"),
            h = 0x811c9dc5,
            i;
        for (i = 0; i < s.length; i++) {
            h ^= s.charCodeAt(i);
            h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
        }
        return ("00000000" + (h >>> 0).toString(16)).slice(-8);
    }

    function readStore() {
        var raw = localStorage.getItem(KEY);
        if (raw === null) return {version: 2, anchor: "", records: [], retainedOnly: false};
        var value = JSON.parse(raw);
        if (Array.isArray(value)) value = {
            version: 2, anchor: value.length ? value[0].prev || "" : "",
            records: value, retainedOnly: value.length > 0
        };
        if (!value || value.version !== 2 || typeof value.anchor !== 'string' || !Array.isArray(value.records) ||
            !value.records.every(function(r) { return r && typeof r === 'object' && typeof r.action === 'string' && (typeof r.ts === 'string' || typeof r.ts === 'number'); })) throw new Error("审计记录格式错误");
        return value;
    }

    function all() {
        try { return readStore().records; }
        catch (e) { return []; }
    }

    function currentActor() {
        try {
            if (window.AdminLogin && AdminLogin.getLogin) {
                var s = AdminLogin.getLogin();
                if (s && s.name) return s.name;
            }
        } catch (e) {}
        try {
            if (window.App && App.loginInfo && App.loginInfo.name) return App.loginInfo.name;
        } catch (e) {}
        return "未登录";
    }

    return {
        MAX: MAX,
        /**
         * @param {string} action
         * @param {string} [detail]
         * @param {{actor?:string,result?:string,target?:string}} [opts]
         * @returns {{ok:boolean, dropped?:number}}
         */
        log: function(action, detail, opts) {
            opts = opts || {};
            try {
                var store = readStore(), list = store.records.slice();
                var last = list.length ? list[list.length - 1] : null;
                var prev = last && last.hash ? last.hash : store.anchor;
                var rec = {
                    ts: new Date().toISOString(),
                    action: action,
                    detail: detail || "",
                    actor: opts.actor || currentActor(),
                    result: opts.result || "info",
                    target: opts.target || "",
                    prev: prev,
                    hash: ""
                };
                rec.hash = chainHash(rec, prev);
                list.push(rec);
                var dropped = 0;
                if (list.length > MAX) {
                    dropped = list.length - MAX;
                    list = list.slice(-MAX);
                    store.anchor = list[0].prev || "";
                    store.retainedOnly = true;
                }
                store.records = list;
                var ok = saveJSON(KEY, store);
                return {
                    ok: !!ok,
                    dropped: dropped
                };
            } catch (e) {
                return {
                    ok: !1
                };
            }
        },
        all: all,
        actor: currentActor,
        /* 条件筛选：{action, result, actor} 均可选，空则不过滤 */
        filter: function(cond) {
            cond = cond || {};
            return all().filter(function(r) {
                if (cond.action && r.action !== cond.action) return !1;
                if (cond.result && (r.result || "info") !== cond.result) return !1;
                if (cond.actor && r.actor !== cond.actor) return !1;
                return !0;
            });
        },
        /**
         * 校验哈希链是否完整
         * @returns {{ok:boolean, broken:number, total:number, legacy:number}}
         */
        verify: function() {
            var store;
            try { store = readStore(); }
            catch (e) { return {ok: false, broken: 1, total: 0, legacy: 0, retainedOnly: false}; }
            var list = store.records,
                prev = store.anchor || "",
                broken = 0,
                legacy = 0,
                i, r;
            for (i = 0; i < list.length; i++) {
                r = list[i];
                if (!r.hash) {
                    /* 升级前写入的旧记录：没有哈希，作为新链的起点 */
                    legacy++;
                    prev = "";
                    continue;
                }
                if (r.prev !== prev) broken++;
                else if (chainHash(r, r.prev) !== r.hash) broken++;
                prev = r.hash;
            }
            return {
                ok: 0 === broken,
                broken: broken,
                total: list.length,
                legacy: legacy,
                retainedOnly: !!store.retainedOnly
            };
        },
        /* 清空（页面「清空审计」按钮用；清空本身会留一条记录） */
        clear: function() {
            var rec = {ts: new Date().toISOString(), action: '清空本机审计记录', detail: '此前记录已清空，仅校验当前保留记录', actor: currentActor(), result: 'success', target: KEY, prev: '', hash: ''};
            rec.hash = chainHash(rec, '');
            return !!saveJSON(KEY, {version: 2, anchor: "", records: [rec], retainedOnly: true});
        }
    };
}();
