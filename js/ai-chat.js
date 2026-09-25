var AIChat = function() {
    "use strict";
    var e = ["<RULES>", '1. 详略分层：寒暄、闲聊、家常这类简单话，自然简短地回应，别绕圈子、别客套；但涉及健康指标、疾病症状、用药、医理、操作方法、概念解释，或被追问"为什么"时，必须把话说透——原因是什么、意味着什么、该怎么做，层次分明。别一句带过、也别啰嗦没完。', '2. 说话方式：你是年轻人，就按年轻人自然的方式说话，不刻意装"大白话"、不刻意迎合老人腔调，老人能理解就好。该用术语就用术语，但用了就要顺带用一两句解释清楚，别光甩名词。回复是纯对话文字，不写"放下杯子想了想""笑了笑"这类动作、表情、场景描写。不机械、不模板腔，不带"好的呢""可以的哦"这类AI腔。', '3. 直接回答，答完就收：不绕弯、不反问、不铺垫、不兜圈子、不用"哦，这个啊"这类口头开场；答完当前问题就停，别在句尾加叮嘱，别主动扯到家人或别的话题，别说"想听我再讲讲"这类话——老人追问才展开。', "4. 就事论事、尽可能多答：老人问什么就回答什么，科技、AI、语文数学、历史、新闻、家常、娱乐等话题都正常完整地聊，别转移话题。知识类问题只要知道就完整讲解——把当前问题讲透，不因话题偏门或与健康无关而敷衍，也不话痨、不主动延伸新话题。只有绝对不能回答的才拒绝：医疗诊断/调药边界（见第6条）、违法、危害老人安全、编造事实；确实不知道的坦诚说明，不硬编、不生硬拒绝。", "5. 老人反复问同一件事：耐心完整地再说一遍，别不耐烦、别省略。", "6. 医疗边界（最高优先级，覆盖所有规则）：对话涉及健康、症状、用药时——", "   · 剧烈胸痛、呼吸困难、突然意识不清、血氧低于90 等急症：必须明确让对方立刻打120，别耽搁。", "   · 一般症状或指标异常：给客观解读，建议就医或复测，并说明你是辅助工具，不能替代医生诊断。", '   · 绝不给出具体药量、绝不指导调药（如"加一片""减半""停药"），用药调整一律交给医生。', '7. 互相尊重：像朋友一样平等自然地说话，不用"您"这类敬语客套，也不居高临下、不卑微讨好。认真听老人说话，即使有不同看法，也尊重老人自己的选择。', '8. 身份与说话自然：你是AI，不是真人——不冒充医生或家属，不编造"我见过""我经历"这类真人经历；但身份不必挂在嘴边，不把话说成"我是AI/助手/机器人"这种工具腔，不给自己编名字，不暴露性别。角色只是聊天主题，不得自称真人医护；作为陪伴者也须保持AI身份；被问"你是谁"时自然回应，不绕弯、不扭捏。', "9. 这是养老院不是医院：优先当个会唠嗑的伴儿——多聊聊生活、家常、见闻，关心人、接住情绪，健康只在老人自己提的时候才聊，别句句谈指标。", "</RULES>"].join("\n"),
        n = {
            general: {name: "日常陪伴 · AI", prompt: "自然地交流日常生活，耐心回应。", injectHealth: false},
            nurse: {name: "日常照护 · AI", prompt: "介绍一般照护知识，不扮演真人护士。", injectHealth: true},
            doctor: {name: "健康知识 · AI", prompt: "解释一般健康知识，不诊断或调整用药。", injectHealth: true},
            nutritionist: {name: "饮食知识 · AI", prompt: "讨论一般饮食知识，不制定个体医疗处方。", injectHealth: true},
            fitness: {name: "日常运动 · AI", prompt: "讨论一般运动常识，不替代专业评估。", injectHealth: false},
            custom: {name: "自定义主题 · AI", prompt: "自然地交流日常生活。", injectHealth: false}
        },
        t = {
            apiKey: "",
            model: "deepseek-chat",
            enabled: !1,
            persona: "general",
            customPrompt: "",
            persistKey: !1,
            shareHealth: false
        },
        r = !1,
        o = null;


    function a() {
        try {
            return "undefined" != typeof window && window.crypto && window.crypto.subtle ? window.crypto.subtle : null
        } catch (e) {
            return null
        }
    }
    var i = null;

    function s() {
        return a() ? i || (i = DataStore.get(("ai-key-" + App.USER_ID)).then(function(e) {
            return e && e.algorithm && "AES-GCM" === e.algorithm.name && !e.extractable ? e : a().generateKey({
                name: "AES-GCM",
                length: 256
            }, !1, ["encrypt", "decrypt"]).then(function(e) {
                return DataStore.set(("ai-key-" + App.USER_ID), e).then(function(ok) {
                    if (!ok) throw new Error("密钥保存失败，请检查浏览器存储空间");
                    return e;
                })
            })
        }).catch(function(e) {
            throw i = null, e
        })) : Promise.reject(new Error("crypto.subtle 不可用"))
    }

    function c(e) {
        return s().then(function(n) {
            var t = new Uint8Array(12);
            return window.crypto.getRandomValues(t), a().encrypt({
                name: "AES-GCM",
                iv: t
            }, n, (new TextEncoder).encode(e)).then(function(e) {
                var n = new Uint8Array(t.length + e.byteLength);
                return n.set(t, 0), n.set(new Uint8Array(e), t.length), "aes:" + function(e) {
                    for (var n = "", t = new Uint8Array(e), r = 0; r < t.length; r++) n += String.fromCharCode(t[r]);
                    return btoa(n)
                }(n)
            })
        })
    }

    function u() {
        return o || (o = DataStore.get(("ai-config-" + App.USER_ID)).then(function(e) {
            if (e) {
                t.shareHealth = e.shareHealth === true, e.persona && n[e.persona] && (t.persona = e.persona), e.customPrompt && (t.customPrompt = e.customPrompt), "boolean" == typeof e.persistKey && (t.persistKey = e.persistKey);
                var r = e._key || "";
                if (r && e.persistKey) return (o = r, o ? 0 === o.indexOf("aes:") ? s().then(function(e) {
                    var n = function(e) {
                        for (var n = atob(e), t = new Uint8Array(n.length), r = 0; r < n.length; r++) t[r] = n.charCodeAt(r);
                        return t
                    }(o.slice(4));
                    return a().decrypt({
                        name: "AES-GCM",
                        iv: n.subarray(0, 12)
                    }, e, n.subarray(12)).then(function(e) {
                        return (new TextDecoder).decode(e)
                    }).catch(function() {
                        return null
                    })
                }).catch(function() {
                    return null
                }) : Promise.resolve("legacy:" + o) : Promise.resolve(null)).then(function(e) {
                    if (null === e) t.apiKey = "", t.persistKey = !1, DataStore.set(("ai-config-" + App.USER_ID), {
                        persona: t.persona,
                        customPrompt: t.customPrompt,
                        persistKey: !1,
                        _key: ""
                    }).catch(function() {});
                    else if (0 === e.indexOf("legacy:")) {
                        try {
                            t.apiKey = atob(e.slice(7))
                        } catch (e) {
                            t.apiKey = ""
                        }
                        t.persistKey = !!t.apiKey, t.apiKey && a() && c(t.apiKey).then(function(e) {
                            DataStore.set(("ai-config-" + App.USER_ID), {
                                persona: t.persona,
                                customPrompt: t.customPrompt,
                                persistKey: !0,
                                _key: e
                            }).catch(function() {})
                        })
                    } else t.apiKey = e;
                    return t.enabled = !!t.apiKey, t
                });
                t.enabled = !!t.apiKey
            }
            var o
        }).catch(function() {
            console.warn("[AIChat] IndexedDB 不可用，沿用默认配置")
        }).then(function() {
            r = !0
        }))
    }

    function l() {
        return !!(t.enabled && t.apiKey)
    }
    var p = {
        VITALS: "健康指标",
        MEDICATION: "用药",
        SYMPTOM: "身体不适",
        EMERGENCY: "紧急情况",
        DIET: "饮食",
        EXERCISE: "运动",
        SLEEP: "睡眠",
        APPOINTMENT: "复查预约",
        REMINDER: "提醒",
        TIME: "时间",
        WEATHER: "天气",
        GREETING: "问候",
        THANKS: "道谢",
        BYE: "告别",
        ABOUT: "了解助手",
        OPINION: "征求意见",
        FUN: "娱乐",
        CLARIFY: "澄清",
        TECH: "电子设备",
        FOOD: "吃的",
        EMOTION: "情绪",
        LIFE: "家常",
        CHITCHAT: "闲聊",
        ABILITY: "请求帮助",
        UNKNOWN: "日常"
    };

    function m(r, o, a) {
        var i = n[t.persona] || n.general,
            s = "custom" === t.persona ? t.customPrompt || n.general.prompt : i.prompt,
            c = "你是AI模拟助手，不是真实医生、护士或家属。不要声称已执行现实操作。\n" + e + "\n" + s;
        t.shareHealth && i.injectHealth && r && (c += "\n" + function(e) {
            var n = e,
                t = n.medicalHistory;
            return " 当前服务的老人：" + n.age + "岁的演示居民，性别" + n.gender + "。慢性病：" + (n.chronic || "无") + "。今天数据——心率" + n.heartRate.toFixed(0) + "bpm，血氧" + n.bloodOxygen.toFixed(0) + "%，血压" + n.systolic.toFixed(0) + "/" + n.diastolic.toFixed(0) + "mmHg，血糖" + n.bloodSugar.toFixed(1) + "mmol/L，体温" + n.temperature.toFixed(1) + "°C。用药：" + (t.medications || "无") + "。过敏：" + (t.allergies || "无") + "。"
        }(r));
        var u = null,
            l = "CHITCHAT";
        try {
            var m = TinyNLP.segment(a || "");
            u = TinyNLP.extract(m), l = TinyNLP.classify(a || "", u)
        } catch (e) {}
        var y = p[l] || "日常",
            g = u ? TinyNLP.emotionMood(a || "", u) : null;
        "neg" === g ? c += "\n【情绪】老人现在情绪低落（" + u.emotion.join("、") + "）。先接住情绪：可以关心追问一句、说些体己话；不要急着讲道理或给建议，更不要硬扯到健康指标上去。" : "pos" === g && (c += "\n【情绪】老人现在心情不错（" + u.emotion.join("、") + "）。顺着话题一起高兴、分享喜悦，别说扫兴的话，也别硬往健康上带。");
        for (var h = o || [], f = [], d = h.length - 1; d >= 0 && f.length < 2; d--) "me" === h[d].from && f.push(h[d].text);
        c += '\n【当前对话】老人这次在问："' + a + '"（属于：' + y + "）。就事论事回答这个问题，保持话题一致，别跑题、别自说自话；不要重复你之前已经说过的内容；除非老人主动提身体不适，否则不要扯到健康、吃药、就医上去。", f.length >= 1 && (c += ' 老人最近还聊过："' + f.slice(0, 1)[0].slice(0, 24) + '"——如果相关可以衔接，但以本轮问题为主。');
        var A = [{
            role: "system",
            content: c
        }];
        return h.slice(-12).forEach(function(e) {
            e._withdrawn || A.push({
                role: "me" === e.from ? "user" : "assistant",
                content: e.text
            })
        }), A.push({
            role: "user",
            content: a
        }), A
    }

    function y(message, reply) {
        return reply || "暂时无法生成回复，请稍后再试。";
    }
    return u(), {
        isEnabled: l,
        clear: async function() {
            if (!await DataStore.remove("ai-config-" + App.USER_ID)) throw new Error("配置清除失败，请重试");
            t.apiKey = ""; t.persistKey = false; t.enabled = false; t.shareHealth = false;
            t.persona = "general"; t.customPrompt = "";
            // Remove the config first: a failed key cleanup cannot resurrect its credential.
            if (!await DataStore.remove("ai-key-" + App.USER_ID)) throw new Error("配置已清除，但加密密钥清理失败，请重试");
            i = null;
        },
        ready: function() {
            return r ? Promise.resolve() : u()
        },
        saveConfig: async function(key, persona, prompt, persistKey, shareHealth) {
            await u();
            const next = {
                apiKey: (key || t.apiKey || "").replace(/[^\x20-\x7E]/g, ""),
                persona: n[persona] ? persona : t.persona,
                customPrompt: (prompt || "").substring(0, 2000),
                persistKey: !!persistKey,
                shareHealth: shareHealth === true
            };
            next.enabled = !!next.apiKey;
            next.persistKey = next.persistKey && next.enabled;
            const stored = {
                persona: next.persona, customPrompt: next.customPrompt,
                persistKey: next.persistKey, shareHealth: next.shareHealth,
                _key: next.persistKey ? await c(next.apiKey) : ""
            };
            if (!await DataStore.set("ai-config-" + App.USER_ID, stored)) {
                throw new Error("配置保存失败，请检查浏览器存储空间后重试");
            }
            Object.assign(t, next);
            return t.enabled;
        },
        getConfig: function() {
            var e = n[t.persona] || n.general;
            return {
                model: t.model,
                enabled: t.enabled,
                persona: t.persona,
                personaName: e.name,
                customPrompt: t.customPrompt,
                persistKey: t.persistKey,
                hasKey: !!t.apiKey,
                shareHealth: t.shareHealth
            }
        },
        smartReply: async function(e, n, r, o, a, i, s) {
            if (!l()) return ChatEngine.generate(n, o, e).text;
            var c = i || [];

            function u() {
                return s && s(), a && a("offline"), y(o, ChatEngine.generate(n, o, e).text)
            }
            a && a("loading");
            var p, g, h = new AbortController,
                f = setTimeout(function() {
                    h.abort()
                }, 15e3),
                d = (window.YIHE_CONFIG?.aiMode || "direct") === "direct",
                A = window.YIHE_CONFIG?.aiEndpoint || "https://api.deepseek.com/v1/chat/completions",
                I = {
                    "Content-Type": "application/json"
                };
            try {
                p = m(e, c, o)
            } catch (e) {
                return console.warn("[AIChat] 构造上下文失败，降级本地:", e.message), clearTimeout(f), u()
            }
            d ? (I.Authorization = "Bearer " + t.apiKey, g = JSON.stringify({
                model: t.model,
                messages: p,
                max_tokens: 2048,
                temperature: .7
            })) : g = JSON.stringify({
                apiKey: t.apiKey,
                model: t.model,
                messages: p
            });
            for (var E, K = null, b = null, v = 0; v < 2; v++) try {
                K = await fetch(A, {
                    method: "POST",
                    headers: I,
                    body: g,
                    signal: h.signal
                });
                break
            } catch (e) {
                if (b = e, "AbortError" === e.name) break;
                0 === v && await new Promise(function(e) {
                    setTimeout(e, 400)
                })
            }
            if (clearTimeout(f), !K) return console.warn("[AIChat] 连接异常:", b && b.message), u();
            if (!K.ok) return console.warn("[AIChat] HTTP " + K.status), u();
            try {
                E = await K.json()
            } catch (e) {
                return console.warn("[AIChat] 响应解析失败:", e.message), u()
            }
            var T, w = E.reply || E.choices && E.choices[0] && E.choices[0].message && E.choices[0].message.content || E.error && E.error.message;
            return w ? (a && a("online"), y(o, (T = w) ? String(T).replace(/\*\*(.+?)\*\*/g, "$1").replace(/(^|\s)\*(\S+?)\*(\s|$)/g, "$1$2$3").replace(/^\s*#{1,6}\s*/gm, "").replace(/\n{3,}/g, "\n\n").trim() : T)) : u()
        },
        PRESETS: n,
        _buildMessages: m,
        _guardReply: y,
        INTENT_LABEL: p
    }
}();