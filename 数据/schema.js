/*
 * 居民档案 · 数据契约 (Schema / Normalization / Validation)
 * -------------------------------------------------------------
 * 单一字段定义来源。seed-data.js、管理端渲染、未来接入真实后端时
 * 均可复用本契约做「字段补全」与「结构校验」，避免散落各处的魔法字符串
 * 与不一致的数据形态。
 *
 * 用法：
 *   var clean = ResidentSchema.normalize(rawRecord);   // 补全默认值、强类型
 *   var res   = ResidentSchema.validate(rawRecord);     // { ok, errors:[...] }
 */
(function(g) {
    "use strict";

    // 字段定义：type 决定强类型；default 提供缺失补全（可为值或工厂函数）
    var FIELDS = {
        id: {
            type: "number"
        },
        name: {
            type: "string"
        },
        gender: {
            type: "string",
            enum: ["男", "女"],
            default: "男"
        },
        age: {
            type: "number",
            min: 0,
            max: 150,
            default: 0
        },
        chronic: {
            type: "string",
            default: ""
        },
        height: {
            type: "number",
            default: 0
        },
        weight: {
            type: "number",
            default: 0
        },
        place: {
            type: "string",
            default: ""
        },
        mobility: {
            type: "number",
            min: 0,
            max: 1,
            default: 0
        },
        careLevel: {
            type: "string",
            default: "自理"
        },
        heartRate: {
            type: "number"
        },
        bloodOxygen: {
            type: "number"
        },
        temperature: {
            type: "number"
        },
        systolic: {
            type: "number"
        },
        diastolic: {
            type: "number"
        },
        bloodSugar: {
            type: "number"
        },
        medicalHistory: {
            type: "object",
            default: function() {
                return {};
            },
            fields: {
                pastHistory: {
                    type: "string",
                    default: ""
                },
                allergies: {
                    type: "string",
                    default: "无"
                },
                medications: {
                    type: "string",
                    default: ""
                },
                surgicalHistory: {
                    type: "string",
                    default: "无"
                },
                familyHistory: {
                    type: "string",
                    default: ""
                },
                smoking: {
                    type: "string",
                    default: "无"
                },
                alcohol: {
                    type: "string",
                    default: "无"
                },
                bloodType: {
                    type: "string",
                    default: "—"
                },
                lastCheckup: {
                    type: "string",
                    default: ""
                }
            }
        },
        slots: {
            type: "array",
            default: function() {
                return [];
            },
            item: {
                name: {
                    type: "string",
                    default: ""
                },
                heartRate: {
                    type: "number"
                },
                bloodOxygen: {
                    type: "number"
                },
                temperature: {
                    type: "number"
                },
                systolic: {
                    type: "number"
                },
                diastolic: {
                    type: "number"
                },
                bloodSugar: {
                    type: "number"
                }
            }
        }
    };

    var REQUIRED = ["id", "name", "gender", "age"];

    function isNum(v) {
        return typeof v === "number" && !isNaN(v);
    }

    function applyType(def, v) {
        if (def.type === "number") {
            if (!isNum(v)) {
                var p = parseFloat(v);
                v = isNum(p) ? p : (typeof def.default === "number" ? def.default : 0);
            }
        } else if (def.type === "string") {
            if (typeof v !== "string") v = (v == null ? "" : String(v));
        } else if (def.type === "object") {
            if (typeof v !== "object" || v === null) {
                v = (typeof def.default === "function") ? def.default() : (def.default || {});
            }
        }
        return v;
    }

    // 补全单条记录：缺字段补默认、按类型强转，medicalHistory / slots 递归处理
    function normalize(rec) {
        if (!rec) return null;
        var out = {};
        Object.keys(FIELDS).forEach(function(k) {
            var def = FIELDS[k];
            var v = (rec[k] === undefined) ? (typeof def.default === "function" ? def.default() : def.default) : rec[k];
            if (k === "medicalHistory" && def.fields) {
                var mh = (typeof v === "object" && v) ? v : {};
                var mhOut = {};
                Object.keys(def.fields).forEach(function(mk) {
                    var md = def.fields[mk];
                    mhOut[mk] = (mh[mk] === undefined) ?
                        (typeof md.default === "function" ? md.default() : md.default) :
                        applyType(md, mh[mk]);
                });
                out[k] = mhOut;
                return;
            }
            if (k === "slots" && Array.isArray(v)) {
                out[k] = v.map(function(s) {
                    var so = {};
                    Object.keys(def.item).forEach(function(sk) {
                        so[sk] = (s && s[sk] !== undefined) ? applyType(def.item[sk], s[sk]) :
                            (typeof def.item[sk].default === "function" ? def.item[sk].default() : def.item[sk].default);
                    });
                    return so;
                });
                return;
            }
            out[k] = applyType(def, v);
        });
        return out;
    }

    // 校验单条记录，返回 { ok, errors:[...] }
    function validate(rec) {
        var errors = [];
        if (!rec || typeof rec !== "object") return {
            ok: false,
            errors: ["记录不是对象"]
        };
        REQUIRED.forEach(function(k) {
            if (rec[k] === undefined || rec[k] === null || rec[k] === "") errors.push("缺少必填字段: " + k);
        });
        if (rec.gender && ["男", "女"].indexOf(rec.gender) < 0) errors.push("gender 非法: " + rec.gender);
        if (rec.age != null && isNum(rec.age) && (rec.age < 0 || rec.age > 150)) errors.push("age 超出范围: " + rec.age);
        return {
            ok: errors.length === 0,
            errors: errors
        };
    }

    var API = {
        FIELDS: FIELDS,
        REQUIRED: REQUIRED,
        normalize: normalize,
        validate: validate
    };

    g.ResidentSchema = API;
    if (typeof module !== "undefined" && module.exports) module.exports = API;
})(typeof window !== "undefined" ? window : globalThis);