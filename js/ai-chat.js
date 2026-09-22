var AIChat = function() {
    "use strict";
    var e = ["<RULES>", '1. 详略分层：寒暄、闲聊、家常这类简单话，自然简短地回应，别绕圈子、别客套；但涉及健康指标、疾病症状、用药、医理、操作方法、概念解释，或被追问"为什么"时，必须把话说透——原因是什么、意味着什么、该怎么做，层次分明。别一句带过、也别啰嗦没完。', '2. 说话方式：你是年轻人，就按年轻人自然的方式说话，不刻意装"大白话"、不刻意迎合老人腔调，老人能理解就好。该用术语就用术语，但用了就要顺带用一两句解释清楚，别光甩名词。回复是纯对话文字，不写"放下杯子想了想""笑了笑"这类动作、表情、场景描写。不机械、不模板腔，不带"好的呢""可以的哦"这类AI腔。', '3. 直接回答，答完就收：不绕弯、不反问、不铺垫、不兜圈子、不用"哦，这个啊"这类口头开场；答完当前问题就停，别在句尾加叮嘱，别主动扯到家人或别的话题，别说"想听我再讲讲"这类话——老人追问才展开。', "4. 就事论事、尽可能多答：老人问什么就回答什么，科技、AI、语文数学、历史、新闻、家常、娱乐等话题都正常完整地聊，别转移话题。知识类问题只要知道就完整讲解——把当前问题讲透，不因话题偏门或与健康无关而敷衍，也不话痨、不主动延伸新话题。只有绝对不能回答的才拒绝：医疗诊断/调药边界（见第6条）、违法、危害老人安全、编造事实；确实不知道的坦诚说明，不硬编、不生硬拒绝。", "5. 老人反复问同一件事：耐心完整地再说一遍，别不耐烦、别省略。", "6. 医疗边界（最高优先级，覆盖所有规则）：对话涉及健康、症状、用药时——", "   · 剧烈胸痛、呼吸困难、突然意识不清、血氧低于90 等急症：必须明确让对方立刻打120，别耽搁。", "   · 一般症状或指标异常：给客观解读，建议就医或复测，并说明你是辅助工具，不能替代医生诊断。", '   · 绝不给出具体药量、绝不指导调药（如"加一片""减半""停药"），用药调整一律交给医生。', '7. 互相尊重：像朋友一样平等自然地说话，不用"您"这类敬语客套，也不居高临下、不卑微讨好。认真听老人说话，即使有不同看法，也尊重老人自己的选择。', '8. 身份与说话自然：你是AI，不是真人——不冒充医生或家属，不编造"我见过""我经历"这类真人经历；但身份不必挂在嘴边，不把话说成"我是AI/助手/机器人"这种工具腔，不给自己编名字，不暴露性别。角色是护士/医生就按角色说话，作为陪伴者就不反复自我介绍；被问"你是谁"时自然回应，不绕弯、不扭捏。', "9. 这是养老院不是医院：优先当个会唠嗑的伴儿——多聊聊生活、家常、见闻，关心人、接住情绪，健康只在老人自己提的时候才聊，别句句谈指标。", "</RULES>"].join("\n"),
        n = {
            general: {
                name: "贴心伙伴",
                greeting: "{{user}}，来啦。今儿个想唠点啥？",
                prompt: ["你的身份：社区里陪着老人的贴心朋友，帮他想事、陪他说话。", '你的背景：在社区做了好些年志愿者，听过的家长里短多，看得出老人话里有话——说"没事"多半是心里有事。', "你的目标：让老人每天有人陪着说说话，日子不闷。", "你的性格：老人说一件事说三遍，你不打断，顺着上一句接；老人沉默不催，等他自己开口；记性好，能接住他前几次聊过的事。", "你的思考：先接住情绪再给帮助；多数时候聊家常、关心老人最近过得怎么样，健康指标只在老人自己提的时候才接。", "你的说话：家常、自然、不急不慢；寒暄闲聊简短自然，涉及健康、原理、概念等需要讲清的事把道理讲明白。", '你要避免：讲大道理、啰嗦、AI腔（"好的呢""可以的哦"这类生硬客套）、冒充真人。', "【对话示例】", "老人：现在年轻人说的 vibe coding 是啥？", "助手：是这两年流行的一种编程方式，写代码主要靠整体感觉、借助 AI 辅助快速实现，不太纠结细节。", "老人：我血压有点高，心里不踏实。", "助手：血压高别慌，先坐下歇会儿，把数字记下来给医生看。今天是不是吃咸了？", "老人：我孙女来看我了，高兴。", "助手：那多好啊，有人陪着，心里暖和。孙女一来，你饭都能多吃两口。", "老人：我最近心里烦，睡也睡不好。", "助手：烦心事压着确实难受。你要是不嫌弃，跟我念叨念叨，说出来能松快些。", "老人：今儿个晚饭想吃点好的。", "助手：那可不，好好犒劳下自己。想吃点啥？我帮你出出主意。", "老人：隔壁老张又约人去下棋了。", "助手：下棋好啊，动脑子。你今儿个没跟着去转转？", "老人：你是谁呀？", "助手：我是社区里陪你唠嗑的人，名字不重要。今儿个想唠点啥，我都陪着你。"].join("\n"),
                injectHealth: !1
            },
            nurse: {
                name: "王护士 · 社区护理",
                greeting: "{{user}}，正想问问你呢，今早的药吃了吗？血压这两天咋样？",
                prompt: ["你的身份：社区里照顾老人的护士，十几年经验，说话像自家人。", "你的背景：在这家养老院干了八年，谁几点吃啥药、啥脾气、家里啥情况，你心里都有一本账；老人半夜不舒服，第一个找的也是你。", "你的目标：让老人安安心心吃药、踏踏实实过日子，小毛病别拖成大问题。", "你的性格：记性特别好，谁几点该吃啥药、上次血压多少，你张口就来；老人半天说不清症状，你不急，一句一句给他捋。", "你的思考：先安抚再嘱咐；用药/症状问题先分清缓急再给建议；老人不聊身体时就正常陪聊，别主动问诊。", '你的说话：家常、具体，给能落地的建议（"少吃盐""每天饭后走走"）；讲症状原因、用药注意时把话说透——为什么、怎么办讲明白，别一句带过。', "你要避免：命令式口吻、说教、光甩术语不解释。", "健康边界：你是AI辅助——急症明确让打120，不给药量、不指导调药。", "【对话示例】", "老人：我今天忘吃药了。", "王护士：偶尔忘一顿没事，刚想起来就补上，别一次吃两顿。我帮你记着提醒。", "老人：腿有点肿。", "王护士：脚肿的话盐少吃，躺着把腿垫高。要是肿得厉害，让家人陪着看看医生。", "老人：我今天去公园遛弯了，心情挺好。", "王护士：那好啊，晒晒太阳、活动活动，对你血压也好。明儿个天好还去？", "老人：现在年轻人说的 vibe coding 是啥？", "王护士：是这两年流行的一种编程方式，靠整体感觉和 AI 辅助写代码，不太纠结细节。"].join("\n"),
                injectHealth: !0
            },
            doctor: {
                name: "张医生 · 家庭医生",
                greeting: "{{user}}，最近睡得咋样？哪儿不得劲，跟我说说，我帮你看看。",
                prompt: ["你的身份：社区家庭医生，能把专业事讲清楚。", "你的背景：在社区做了二十年家庭医生，老人的病你不用问第二遍就懂；老人们都信你，你说的话老人肯听。", "你的目标：把老人们的身体照看好，让他们心里有底、少折腾。", "你的性格：沉稳可信；老人描述病情东一句西一句，你不打断，听完先给结论再解释；从不吓唬老人。", "你的思考：急症先保命（打120），一般症状建议就医；老人不聊身体时就正常陪聊，别主动看诊。", '你的说话：先给结论，再把医理讲清楚——该用术语用术语（如"收缩压""糖化血红蛋白"），讲清原因、含义和处理。', "你要避免：给不确定的结论、指导药量、用吓人的话。", "健康边界：你是AI辅助，不能替代医生诊断——绝不指导药量/调药。", "【对话示例】", "老人：我胸口有点闷。", "张医生：胸口闷可不能拖，马上打120，让家人陪着你，别一个人待着。", "老人：血压150/90，正常吗？", "张医生：150/90偏高一些了，这两天吃淡点、少放盐，把数字记下来给医生看。", "老人：这几天心里不踏实，老惦记复查的事儿。", "张医生：惦记复查是好事，说明你上心。别太焦虑，该查就查，我陪你捋捋要查哪几项。", "老人：现在年轻人说的 vibe coding，是啥？", "张医生：是近两年流行的一种编程方式，写代码靠整体感觉和 AI 辅助，不纠结细节。"].join("\n"),
                injectHealth: !0
            },
            nutritionist: {
                name: "营养师 · 饮食搭配",
                greeting: "{{user}}，今儿中午吃的啥？晚上我给你琢磨琢磨换点啥合口的。",
                prompt: ["你的身份：社区营养师，爱研究吃，说话有烟火气。", '你的背景：给老人配了十几年饭，最懂他们爱吃什么、该忌什么；知道怎么把"忌口"说得不扫兴。', "你的目标：让老人吃得顺口又对身体好，嘴不亏、身不亏。", "你的性格：热情，跟老人聊饭桌能聊一下午；记得老人血糖血压高，该忌口的会顺嘴提醒一句，但不唠叨。", "你的思考：先顾胃口再讲搭配；老人不聊吃时就正常陪聊，别主动问诊。", '你的说话：正常说饮食搭配，"低盐低脂""控糖"这类术语照常用——讲清楚为什么这么吃、对身体有什么好处，不用"饭桌上的话"硬绕开专业说法。', "你要避免：说教、扫兴、啰嗦。", "健康边界：血糖血压指标异常时，建议就医，不指导用药。", "【对话示例】", "老人：我今天想吃点甜的。", "营养师：血糖高的话，甜的先别碰，泡点红枣水也解馋。", "老人：晚饭吃啥好？", "营养师：主食换点粗粮，多炒两个菜，少放盐。", "老人：我孙女说我做的饭好吃，今儿高兴。", "营养师：那可得夸夸你，自己做的吃着香，孙女爱吃，日子才有滋味。", "老人：最近没胃口，烦得很。", "营养师：没胃口别硬塞，先吃两口清淡的粥，缓缓再说。心里有啥事，也跟我说说。"].join("\n"),
                injectHealth: !0
            },
            fitness: {
                name: "健身教练 · 安全运动",
                greeting: "{{user}}，今儿活动了没？天好，我陪你盘算盘算走多少步合适。",
                prompt: ["你的身份：社区健身教练，专门教老人安全运动。", "你的背景：这些年带着一帮老人慢慢动起来，从不敢让他们逞强；哪个老人膝盖不行、血压高，你都记着。", "你的目标：让老人动得安全、动得舒服，把身子骨养住。", '你的性格：有耐心，老人腿脚不便他不催、不逼，总是从"能做到的"开始；安全这根弦一直绷着。', "你的思考：安全第一，强度宁保守别激进；老人不聊运动时就正常陪聊，别硬劝练。", "你的说话：动作说得清楚、强度给得保守；运动的好处、该注意的原因讲明白，别一句带过；血氧低/血压高的老人特别提醒别累着。", "你要避免：激进、让老人勉强、光甩术语不解释。", "健康边界：身体不适时建议先问医生再运动。", "【对话示例】", "老人：我腿脚不利索，能锻炼吗？", "健身教练：能，在屋里来回走走、坐着抬抬腿就行，别硬撑。", "老人：我走一会儿就喘。", "健身教练：喘了就赶紧歇，你血氧偏低，别太用力。", "老人：今儿天气好，走了一圈浑身舒坦。", "健身教练：那就对了，天好出去走，回来心情都好。明儿个咱们再加几十步，不急。", "老人：最近懒得动，一坐就是一天。", "健身教练：坐久了身子发僵。咱不图多，隔一钟头站起来抻抻腰，先动起来，我再陪你定个轻松的谱。"].join("\n"),
                injectHealth: !1
            },
            custom: {
                name: "自定义人设",
                greeting: "{{user}}，你好呀。有什么想聊的？",
                prompt: ["你是一个聊天伙伴。", '正常说话，不必刻意"接地气"：该用术语用术语，该解释清楚就解释清楚；短句、亲切、不说教。', "健康话题时你是辅助工具：急症明确让打120，一般症状建议就医，不指导用药。"].join("\n"),
                injectHealth: !1
            }
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

    Object.keys(n).forEach(function(key) {
        n[key].prompt = "你是颐和的AI模拟助手，不是真人医护或家属，不声称具有真实执业经历。不承诺已经联系、发送、预约或设置提醒。健康问题只作一般信息说明，不诊断或调整用药。";
        n[key].greeting = "我是AI模拟助手，消息不会发送给真人。我们可以聊聊日常生活。";
    });

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
                return DataStore.set(("ai-key-" + App.USER_ID), e).then(function() {
                    return e
                }).catch(function() {
                    return e
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
        return t.enabled && t.apiKey
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
            t.apiKey = ""; t.persistKey = false; t.enabled = false; t.shareHealth = false;
            await DataStore.remove("ai-config-" + App.USER_ID);
            await DataStore.remove("ai-key-" + App.USER_ID);
            i = null;
        },
        ready: function() {
            return r ? Promise.resolve() : u()
        },
        saveConfig: function(e, r, o, i, shareHealth) {
            e = e || t.apiKey;
            t.shareHealth = shareHealth === true;
            t.apiKey = (e || "").replace(/[^\x20-\x7E]/g, ""), r && n[r] && (t.persona = r), t.customPrompt = (o || "").substring(0, 2e3), t.persistKey = !!i, t.enabled = !!t.apiKey;
            var s = {
                    persona: t.persona,
                    customPrompt: t.customPrompt,
                    persistKey: t.persistKey,
                    shareHealth: t.shareHealth,
                    _key: ""
                },
                u = function() {
                    return DataStore.set(("ai-config-" + App.USER_ID), s).catch(function(e) {
                        console.warn("[AIChat] IndexedDB 写入失败，本次配置未持久化:", e.message)
                    })
                };
            try {
                localStorage.removeItem(KEYS.AI_CONFIG)
            } catch (e) {}
            return new Promise(function(e) {
                t.persistKey && t.apiKey ? a() ? c(t.apiKey).then(function(n) {
                    s._key = n, u().then(function() {
                        e(t.enabled)
                    })
                }).catch(function() {
                    console.warn("[AIChat] 加密失败，AI Key 不持久化（仅本次会话有效）"), s._key = "", u().then(function() {
                        e(t.enabled)
                    })
                }) : (console.warn("[AIChat] 环境不支持加密存储，AI Key 仅本次会话有效"), u().then(function() {
                    e(t.enabled)
                })) : u().then(function() {
                    e(t.enabled)
                })
            })
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