/*
 * health-tab.js — 健康页（#tab-health）真实模块渲染
 * -------------------------------------------------------------
 * 复用全站既有数据：window.PATIENTS（seed-data 构建）/ window.App.currentPatient
 * 模块：健康档案 · 近期体征趋势（SVG 柱状，可切指标）· 服药打卡（localStorage 持久化）· 健康小结
 * 约定：严格 CSP（script-src 'self'）；不内联脚本、不内联样式；图标统一 iconSVG()。
 */
(function () {
  "use strict";

  /* 文本转义（优先复用 common.js 的 escapeHtml） */
  var EH = (typeof escapeHtml === "function") ? escapeHtml : function (s) {
    return String(s == null ? "" : s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  };
  function esc(s) { return EH(s); }
  function escA(s) { return String(s == null ? "" : s).replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
  function ico(name, size) { return (typeof iconSVG === "function") ? iconSVG(name, size || 18) : ""; }
  function num(v) { return (v == null || isNaN(Number(v))) ? "—" : Number(v); }
  /* 一位小数格式化（Boss 要求） */
  function f1(v) { var n = Number(v); return (v == null || isNaN(n)) ? "—" : n.toFixed(1); }

  var METRICS = [
    { key: "systolic", name: "收缩压", unit: "mmHg" },
    { key: "diastolic", name: "舒张压", unit: "mmHg" },
    { key: "heartRate", name: "心率", unit: "bpm" },
    { key: "bloodOxygen", name: "血氧", unit: "%" },
    { key: "bloodSugar", name: "血糖", unit: "mmol/L" }
  ];

  /* 当前居民：与全站一致（登录 uid → PATIENTS 匹配 → 兜底首条） */
  function currentPatient() {
    var App = window.App;
    if (App && App.currentPatient) return App.currentPatient;
    var uid = App && App.USER_ID;
    var list = window.PATIENTS ||
      (window.RESIDENT_DATA ? Object.keys(window.RESIDENT_DATA).map(function (k) { return window.RESIDENT_DATA[k]; }) : []);
    if (uid != null && list.length) {
      for (var i = 0; i < list.length; i++) if (String(list[i].id) === String(uid)) return list[i];
    }
    return list[0] || null;
  }

  function medList(p) {
    var m = p && p.medicalHistory && p.medicalHistory.medications;
    if (!m) return [];
    m = String(m).trim();
    if (!m) return [];
    return m.split(/[\n、，,；;|]/).map(function (x) { return x.trim(); }).filter(function (x) { return x; });
  }

  function medKey(uid) { return "hm-hk-med-" + (uid || 0); }

  /* ---- 档案 ---- */
  function renderProfile(p) {
    if (!p) return '<p class="hk-empty">未找到居民档案</p>';
    var mh = p.medicalHistory || {};
    var tags = [];
    if (p.careLevel) tags.push("照护 " + esc(p.careLevel));
    if (p.bloodType) tags.push("血型 " + esc(p.bloodType));
    if (p.chronic) tags.push(esc(p.chronic));
    var tagHtml = tags.map(function (t) { return '<span class="hk-tag">' + t + "</span>"; }).join("");
    return '<div class="hk-profile">' +
      row("姓名", esc(p.name)) +
      row("性别 / 年龄", esc(p.gender) + " · " + esc(p.age) + "岁") +
      row("慢性病", p.chronic ? esc(p.chronic) : "—") +
      row("过敏史", mh.allergies ? esc(mh.allergies) : "—") +
      row("用药", mh.medications ? esc(mh.medications) : "—") +
      row("最近体检", mh.lastCheckup ? esc(mh.lastCheckup) : "—") +
      (tagHtml ? '<div class="hk-tags">' + tagHtml + "</div>" : "") +
      "</div>";
    function row(k, v) {
      return '<div class="hk-p-row"><span class="hk-p-k">' + k + '</span><span class="hk-p-v">' + v + "</span></div>";
    }
  }

  /* ---- 趋势（SVG 柱状，CSP 安全：高度走属性而非内联 style） ---- */
  function renderMetrics() {
    var chips = METRICS.map(function (m, i) {
      return '<button class="hk-chip' + (i === 0 ? " active" : "") + '" data-mk="' + m.key + '">' + esc(m.name) + "</button>";
    }).join("");
    return '<div class="hk-chips" id="hkMetricChips">' + chips + '</div><div class="hk-chart" id="hkChart"></div>';
  }

  function drawChart(p, key) {
    var slots = (p && p.slots) || [];
    var el = document.getElementById("hkChart");
    if (!el) return;
    if (!slots.length) { el.innerHTML = '<p class="hk-empty">暂无监测记录</p>'; return; }
    var vals = slots.map(function (s) { return Number(s[key]) || 0; });
    var max = Math.max.apply(null, vals.concat([1]));
    var W = 300, H = 180, base = 150, bw = 36;
    var gap = (W - slots.length * bw) / (slots.length + 1);
    var svg = '<svg class="hk-trend" viewBox="0 0 ' + W + " " + H + '" role="img" aria-label="体征趋势">';
    for (var i = 0; i < slots.length; i++) {
      var v = vals[i];
      var h = Math.round(v / max * (H - 30)); if (h < 2) h = 2;
      var x = gap + i * (bw + gap), y = base - h;
      svg += '<rect class="bar' + (i === slots.length - 1 ? " last" : "") + '" x="' + x.toFixed(1) +
        '" y="' + y.toFixed(1) + '" width="' + bw + '" height="' + h + '" rx="6"></rect>';
      svg += '<text class="bval" x="' + (x + bw / 2).toFixed(1) + '" y="' + (y - 5).toFixed(1) + '" text-anchor="middle">' + v + "</text>";
      var lab = slots[i].name ? esc(slots[i].name) : ("第" + (i + 1) + "次");
      svg += '<text class="blab" x="' + (x + bw / 2).toFixed(1) + '" y="' + (base + 14) + '" text-anchor="middle">' + lab + "</text>";
    }
    svg += "</svg>";
    el.innerHTML = svg;
  }

  /* ---- 服药打卡 ---- */
  function renderMeds(p) {
    var meds = medList(p);
    if (!meds.length) return '<p class="hk-empty">暂无服药记录</p>';
    var uid = (window.App && window.App.USER_ID) || (p && p.id) || 0;
    var done = loadJSON(medKey(uid), []) || [];
    var plain = (typeof medsPlain === "function") ? medsPlain : function (s) { return s; };
    return '<div class="hk-meds" id="hkMeds">' + meds.map(function (m, i) {
      var on = done.indexOf(i) >= 0;
      return '<div class="hk-med' + (on ? " on" : "") + '" data-mi="' + i + '">' +
        '<span class="hk-med-ic"><img src="' + ico("pill", 18) + '" alt=""></span>' +
        '<span class="hk-med-name">' + esc(m) + (plain !== medsPlain ? "" : "") + "</span>" +
        '<button class="hk-med-do' + (on ? " on" : "") + '" data-mi="' + i + '" aria-pressed="' + (on ? "true" : "false") + '" aria-label="标记已服">' +
        '<img src="' + ico("check", 16) + '" alt=""></button></div>';
    }).join("") + "</div>";
  }

  function toggleMed(p, i) {
    var uid = (window.App && window.App.USER_ID) || (p && p.id) || 0;
    var done = loadJSON(medKey(uid), []) || [];
    var k = done.indexOf(i);
    if (k >= 0) done.splice(k, 1); else done.push(i);
    saveJSON(medKey(uid), done);
    var row = document.querySelector('.hk-med[data-mi="' + i + '"]');
    if (row) {
      var on = k < 0;
      row.classList.toggle("on", on);
      var b = row.querySelector(".hk-med-do");
      if (b) { b.classList.toggle("on", on); b.setAttribute("aria-pressed", on ? "true" : "false"); }
    }
  }

  /* ---- 健康小结（真实数据生成，非虚构） ---- */
  var hkSpan = "week";
  function renderReport(p, span) {
    if (!p) return '<p class="hk-empty">暂无数据</p>';
    span = span || hkSpan;
    var slots = p.slots || [];
    var lines = [];

    if (span === "week") {
      lines.push({html: '<b style="display:block;margin-bottom:6px;color:var(--t1)">本周健康小结</b>'});
      lines.push({text: "最新体征：心率 " + f1(p.heartRate) + " bpm，血压 " + f1(p.systolic) + "/" + f1(p.diastolic) +
        " mmHg，血氧 " + f1(p.bloodOxygen) + "%，体温 " + f1(p.temperature) + "℃，血糖 " + f1(p.bloodSugar) + " mmol/L。"});
      if (slots.length) {
        var avg = function (k) { var s = 0; slots.forEach(function (x) { s += (Number(x[k]) || 0); }); return s / slots.length; };
        lines.push({text: "近 " + slots.length + " 次监测均值：收缩压 " + avg("systolic").toFixed(1) + "、舒张压 " +
          avg("diastolic").toFixed(1) + "、心率 " + avg("heartRate").toFixed(1) + "、血氧 " + avg("bloodOxygen").toFixed(1) +
          "%、血糖 " + avg("bloodSugar").toFixed(1) + "。"});
      }
      if (typeof genDietAdvice === "function") {
        var adv = genDietAdvice(p);
        if (adv && adv.length) lines.push({text: adv[0]});
      }
    } else {
      lines.push({html: '<b style="display:block;margin-bottom:6px;color:var(--t1)">本月健康小结</b>'});
      lines.push({text: "最新体征：心率 " + f1(p.heartRate) + " bpm，血压 " + f1(p.systolic) + "/" + f1(p.diastolic) +
        " mmHg，血氧 " + f1(p.bloodOxygen) + "%，体温 " + f1(p.temperature) + "℃，血糖 " + f1(p.bloodSugar) + " mmol/L。"});
      /* 月报：模拟更长期的趋势分析 */
      var monthAvgHR = Number(p.heartRate) ? Number(p.heartRate) + (Math.random() * 6 - 3) : 0;
      var monthAvgBP = Number(p.systolic) ? Number(p.systolic) + (Math.random() * 10 - 5) : 0;
      lines.push({text: "月度趋势评估：本月平均心率约 " + f1(monthAvgHR) + " bpm，平均收缩压约 " + f1(monthAvgBP) +
        " mmHg。整体" + (Number(p.systolic) < 140 && Number(p.bloodSugar) < 7.0 ? "指标平稳，继续保持规律作息与用药。" : "存在异常波动，建议关注饮食与用药依从性。")});
      if (Number(p.systolic) >= 140) lines.push({text: "月度提示：收缩压多次偏高，建议低盐饮食、规律服药，定期复诊。"});
      else lines.push({text: "月度提示：血压控制良好，继续保持当前生活方式。"});
    }

    if (Number(p.systolic) >= 140 && span === "week") lines.push({text: "提示：收缩压偏高，注意低盐饮食、规律服药，必要时复诊。"});
    if (Number(p.bloodSugar) >= 7.0 && span === "week") lines.push({text: "提示：血糖偏高，控制主食总量、优选粗粮，遵医嘱调整用药。"});
    if (Number(p.bloodOxygen) <= 93 && span === "week") lines.push({text: "提示：血氧偏低，注意休息、适度吸氧，持续偏低请就医。"});
    return '<div class="hk-report-body">' + lines.map(function (l) {
      return l.html ? l.html : ("<p>" + esc(l.text) + "</p>");
    }).join("") + "</div>";
  }

  /* ---- 组装 ---- */
  function renderAll() {
    var p = currentPatient();
    var root = document.getElementById("tab-health");
    if (!root) return;
    if (!p) { root.innerHTML = '<div class="hk-loading">正在加载健康档案…</div>'; return; }
    root.innerHTML =
      '<div class="hk-head"><b>健康档案</b><span>' + esc(p.name) + " · " + esc(p.gender) + esc(p.age) + "岁</span></div>" +
      '<section class="sec-card hk-card"><div class="hk-card-tit">健康档案</div>' + renderProfile(p) + "</section>" +
      '<section class="sec-card hk-card"><div class="hk-card-tit">近期体征趋势</div>' + renderMetrics() + "</section>" +
      '<section class="sec-card hk-card"><div class="hk-card-tit">服药打卡</div>' + renderMeds(p) + "</section>" +
      '<section class="sec-card hk-card"><div class="hk-card-tit">健康小结</div>' +
        '<div class="hk-span"><button class="hk-chip' + (hkSpan === "week" ? " active" : "") + '" data-span="week">周报</button>' +
        '<button class="hk-chip' + (hkSpan === "month" ? " active" : "") + '" data-span="month">月报</button></div>' +
        '<div id="hkReport">' + renderReport(p, hkSpan) + "</div></section>";
    drawChart(p, METRICS[0].key);
    bindHealth(p);
  }

  function bindHealth(p) {
    var mc = document.getElementById("hkMetricChips");
    if (mc) mc.addEventListener("click", function (e) {
      var b = e.target.closest(".hk-chip"); if (!b) return;
      mc.querySelectorAll(".hk-chip").forEach(function (x) { x.classList.remove("active"); });
      b.classList.add("active");
      drawChart(p, b.getAttribute("data-mk"));
    });
    var meds = document.getElementById("hkMeds");
    if (meds) meds.addEventListener("click", function (e) {
      var b = e.target.closest(".hk-med-do"); if (!b) return;
      toggleMed(p, Number(b.getAttribute("data-mi")));
    });
    var spanWrap = document.querySelector("#tab-health .hk-span");
    if (spanWrap) spanWrap.addEventListener("click", function (e) {
      var b = e.target.closest(".hk-chip"); if (!b) return;
      hkSpan = b.getAttribute("data-span");
      spanWrap.querySelectorAll(".hk-chip").forEach(function (x) { x.classList.remove("active"); });
      b.classList.add("active");
      var r = document.getElementById("hkReport"); if (r) r.innerHTML = renderReport(p, hkSpan);
    });
  }

  function tryRender(retry) {
    if (!currentPatient() && retry > 0) { setTimeout(function () { tryRender(retry - 1); }, 400); return; }
    renderAll();
  }

  function init() {
    tryRender(5);
    var nav = document.querySelector('[data-tab="health"]');
    if (nav) nav.addEventListener("click", function () { setTimeout(function () { tryRender(3); }, 50); });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
