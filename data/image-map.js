/* 颐和康养 · 图文映射表（单一事实来源 / Single Source of Truth）
 * ------------------------------------------------------------------
 * 【为什么要有这个文件】
 *   历史问题：配图取自 loremflickr 等"按标签返回随机图"的外链服务，
 *   同一 tag 每次返回的照片都不同，图片与文案的语义关联无法保证，
 *   离线还打不开。→ 图文错配是必然而非偶然。
 *
 * 【现行规则】
 *   1. 所有配图一律使用本地 SVG 插画：assets/img/{域}-{语义槽}.svg
 *   2. 域（domain）枚举：
 *        scene  场景意象      fac   设施空间
 *        svc    服务项目      team  团队人物
 *        guide  入住指引      news  资讯封面
 *   3. 页面 <img> 必须同时写 src（静态兜底，无 JS 也能显示）
 *      与 data-img="域.槽"（语义键）。
 *   4. YIHE_IMG.apply() 在 DOMContentLoaded 后校验二者是否一致：
 *      不一致 → 自动纠正为映射表的值，并在控制台 warn，便于开发期发现。
 *   5. 新增配图流程：先在 REG 登记键位与 alt → 再在 tools/gen_illus.py
 *      写插画 → 最后页面引用。禁止直接在 HTML 里写陌生图片地址。
 *   6. 校验：python tools/check_images.py
 * ------------------------------------------------------------------ */
(function (g) {
  "use strict";

  var BASE = "assets/img/";

  /* key: "域.槽" —— file 省略扩展名与目录，由 BASE 拼接 */
  var REG = {
    /* ---------- scene 场景意象 ---------- */
    "scene.courtyard": {
      file: "scene-courtyard", w: 1600, h: 900,
      alt: "颐和康养中式庭院插画：月洞门、连廊与草木，照护员陪长者散步",
      use: "首页 Hero 主视觉"
    },
    "scene.corridor": {
      file: "scene-corridor", w: 800, h: 525,
      alt: "无障碍连廊插画：日光透过廊柱洒在地面，长者拄杖缓行",
      use: "首页 Hero 卡片 · 庭院里的日光与草木"
    },

    /* ---------- fac 设施空间 ---------- */
    "fac.room": {
      file: "fac-room", w: 800, h: 600,
      alt: "适老居室插画：床边扶手、床头紧急呼叫按钮与冰裂纹窗格",
      use: "关于我们 · 居住房间"
    },
    "fac.hall": {
      file: "fac-hall", w: 800, h: 600,
      alt: "公共活动空间插画：中式屋檐与连廊下，长者围坐交谈",
      use: "关于我们 · 公共与活动空间"
    },

    /* ---------- svc 服务项目 ---------- */
    "svc.daily": {
      file: "svc-daily", w: 800, h: 600,
      alt: "生活照料插画：照护员推轮椅协助长者出行",
      use: "服务 · 生活照料"
    },
    "svc.medical": {
      file: "svc-medical", w: 800, h: 600,
      alt: "医疗护理插画：电子血压计、袖带与听诊器",
      use: "服务 · 医疗护理"
    },
    "svc.rehab": {
      file: "svc-rehab", w: 800, h: 600,
      alt: "康复理疗插画：长者在平行杠间进行步行训练",
      use: "服务 · 康复理疗"
    },
    "svc.meal": {
      file: "svc-meal", w: 800, h: 600,
      alt: "营养膳食插画：热气腾腾的主食碗、两碟配菜与筷子",
      use: "服务 · 营养膳食"
    },
    "svc.culture": {
      file: "svc-culture", w: 800, h: 600,
      alt: "文化娱乐插画：宣纸上的墨字、毛笔与砚台",
      use: "服务 · 文化娱乐"
    },
    "svc.cognitive": {
      file: "svc-cognitive", w: 800, h: 600,
      alt: "认知症照护插画：头部轮廓中的记忆回路，旁有老照片与音符",
      use: "服务 · 认知症照护"
    },

    /* ---------- team 团队人物 ---------- */
    "team.care": {
      file: "team-care", w: 800, h: 600,
      alt: "照护团队插画：护理人员与长者并肩而立，上方有关怀弧线",
      use: "首页 / 关于我们 · 团队介绍"
    },

    /* ---------- guide 入住指引 ---------- */
    "guide.checklist": {
      file: "guide-checklist", w: 800, h: 600,
      alt: "入住准备插画：行李箱、勾选清单与药盒",
      use: "入住指南 · 建议携带"
    },

    /* ---------- news 资讯封面（键位 = 文章 id） ---------- */
    "news.n1": {
      file: "news-n1", w: 1200, h: 680,
      alt: "长期护理保险插画：盾牌与对勾，两侧为盖章的政策文件",
      use: "资讯 n1 · 长护险试点扩围"
    },
    "news.n2": {
      file: "news-n2", w: 1200, h: 680,
      alt: "入秋护养插画：秋日落叶、暖阳与一盏热茶",
      use: "资讯 n2 · 换季护养"
    },
    "news.n3": {
      file: "news-n3", w: 1200, h: 680,
      alt: "银龄学堂插画：书法宣纸与毛笔，旁为正在视频通话的手机",
      use: "资讯 n3 · 银龄学堂开课"
    },
    "news.n4": {
      file: "news-n4", w: 1200, h: 680,
      alt: "端午邻里宴插画：三只粽子摆在长桌上，四周挂着泛黄的老照片与艾草",
      use: "资讯 n4 · 端午邻里宴"
    },
    "news.n5": {
      file: "news-n5", w: 1200, h: 680,
      alt: "居家防跌倒插画：房屋剖面中的扶手、夜间地灯与防滑垫，旁有警示标志",
      use: "资讯 n5 · 预防跌倒十处改造"
    },
    "news.n6": {
      file: "news-n6", w: 1200, h: 680,
      alt: "补贴申领插画：勾选完成的申请表、红色印章、钱袋与政务窗口",
      use: "资讯 n6 · 养老服务补贴申领"
    }
  };

  /* 相对路径前缀：所有官网页面均在根目录，故统一为 BASE */
  function src(key) {
    var it = REG[key];
    return it ? BASE + it.file + ".svg" : "";
  }

  function alt(key) {
    var it = REG[key];
    return it ? it.alt : "";
  }

  function get(key) { return REG[key] || null; }

  /* 校验并纠正页面中的 <img data-img="..."> */
  function apply(root) {
    var scope = root || document;
    var list = scope.querySelectorAll("img[data-img]");
    var fixed = 0, missing = 0;
    for (var i = 0; i < list.length; i++) {
      var el = list[i], key = el.getAttribute("data-img"), it = REG[key];
      if (!it) {
        missing++;
        if (g.console && console.warn) {
          console.warn("[image-map] 未登记的语义键：" + key + "，请先在 data/image-map.js 中登记。", el);
        }
        continue;
      }
      var want = src(key);
      var cur = el.getAttribute("src") || "";
      if (cur.replace(/^\.\//, "") !== want) {
        el.setAttribute("src", want);
        fixed++;
        if (cur && g.console && console.warn) {
          console.warn("[image-map] 图文错配已自动纠正：" + key + "\n  期望 " + want + "\n  实际 " + cur, el);
        }
      }
      if (!el.getAttribute("alt")) el.setAttribute("alt", it.alt);
      if (!el.getAttribute("width")) { el.setAttribute("width", it.w); el.setAttribute("height", it.h); }
      if (!el.getAttribute("decoding")) el.setAttribute("decoding", "async");
      if (!el.hasAttribute("loading")) el.setAttribute("loading", "lazy");
    }
    return { total: list.length, fixed: fixed, missing: missing };
  }

  g.YIHE_IMG = { BASE: BASE, REG: REG, src: src, alt: alt, get: get, apply: apply };
})(window);
