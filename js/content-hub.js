/*
 * content-hub.js — 养生内容中心 (yangheng.html) 交互逻辑
 * -------------------------------------------------------------
 * 数据驱动：window.YangShengContent（文章库） + window.SolarTerms（节气）
 * 功能：实时搜索 / 分类筛选 / 节气浏览 / 详情滑层 / 收藏（localStorage）
 *       / 空·加载·错误态 / 滚动入场动画
 * 约定：严格 CSP（script-src 'self'），不使用任何内联脚本或内联样式；
 *       图标统一经 common.js 的 iconSVG() 生成，杜绝 emoji。
 */
(function () {
  "use strict";

  /* ---------- 0. 依赖与守卫 ---------- */
  if (typeof YangShengContent === "undefined" || !YangShengContent.LIST) {
    console.error("[养生内容中心] 文章数据未加载，请检查 data/content.js");
    showToastSafe("内容数据加载失败，请稍后重试");
    return;
  }
  var TERMS = (typeof SolarTerms !== "undefined" && SolarTerms.TERMS) ? SolarTerms.TERMS : [];
  var BY_NAME = (typeof SolarTerms !== "undefined" && SolarTerms.BY_NAME) ? SolarTerms.BY_NAME : {};
  var CATS = YangShengContent.CATEGORIES || [];

  /* ---------- 1. 状态 + 持久化 ---------- */
  var BM_KEY = "hm-yangsheng-bm";
  var bookmarks = loadJSON(BM_KEY, []) || [];
  if (!Array.isArray(bookmarks)) bookmarks = [];

  var state = {
    q: "",
    cat: "全部",     // 全部 | 分类名
    term: "全部",    // 全部 | 节气名
    fav: false       // 仅看收藏
  };

  function isBM(id) { return bookmarks.indexOf(id) >= 0; }
  function toggleBM(id) {
    var i = bookmarks.indexOf(id);
    if (i >= 0) bookmarks.splice(i, 1);
    else bookmarks.push(id);
    saveJSON(BM_KEY, bookmarks);
    return isBM(id);
  }

  /* ---------- 2. DOM 引用 ---------- */
  var $search = document.getElementById("chSearch");
  var $clear  = document.getElementById("chClear");
  var $catF   = document.getElementById("catFilters");
  var $termF  = document.getElementById("termFilters");
  var $season = document.getElementById("seasonNote");
  var $list   = document.getElementById("articleList");
  var $empty  = document.getElementById("emptyState");
  var $skel   = document.getElementById("skeleton");
  var $clearF = document.getElementById("clearFilters");
  // 详情滑层
  var $detail = document.getElementById("detail");
  var $dTag   = document.getElementById("detailTag");
  var $dClose = document.getElementById("detailClose");
  var $dTitle = document.getElementById("detailTitle");
  var $dMeta  = document.getElementById("detailMeta");
  var $dBody  = document.getElementById("detailBody");
  var $dTags  = document.getElementById("detailTags");
  var $dBm    = document.getElementById("detailBm");
  var $dBmTxt = document.getElementById("detailBmTxt");

  /* ---------- 3. 工具 ---------- */
  function byId(id) {
    var L = YangShengContent.LIST;
    for (var i = 0; i < L.length; i++) if (L[i].id === id) return L[i];
    return null;
  }
  function showToastSafe(msg) {
    if (typeof showToast === "function") showToast(msg);
    else console.warn(msg);
  }
  function heart(size) {
    return typeof iconSVG === "function" ? iconSVG("heart", size || 18) : "";
  }

  /* ---------- 4. 筛选器构建 ---------- */
  function buildChips() {
    // 分类 + 收藏
    var catHtml = '<button class="ch-chip" data-cat="全部" role="tab">全部</button>';
    for (var i = 0; i < CATS.length; i++) {
      catHtml += '<button class="ch-chip" data-cat="' + escapeAttr(CATS[i]) + '" role="tab">' +
                 escapeHtml(CATS[i]) + '</button>';
    }
    catHtml += '<button class="ch-chip fav" data-cat="__fav" role="tab"><span class="dot"></span>收藏</button>';
    $catF.innerHTML = catHtml;

    // 节气
    var termHtml = '<button class="ch-chip" data-term="全部" role="tab">全部</button>';
    for (var j = 0; j < TERMS.length; j++) {
      termHtml += '<button class="ch-chip" data-term="' + escapeAttr(TERMS[j].name) + '" role="tab">' +
                  escapeHtml(TERMS[j].name) + '</button>';
    }
    $termF.innerHTML = termHtml;
  }

  function syncChips() {
    var cs = $catF.querySelectorAll(".ch-chip");
    for (var i = 0; i < cs.length; i++) {
      var c = cs[i].getAttribute("data-cat");
      var on = state.fav ? (c === "__fav") : (c === state.cat);
      cs[i].classList.toggle("active", on);
    }
    var ts = $termF.querySelectorAll(".ch-chip");
    for (var k = 0; k < ts.length; k++) {
      ts[k].classList.toggle("active", ts[k].getAttribute("data-term") === state.term);
    }
  }

  function escapeAttr(s) {
    return String(s).replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  /* ---------- 5. 当前节气自动研判（数据驱动，非硬编码） ---------- */
  function parseSolar(s) {
    // 形如 "2/3 – 2/17" 或 "12/21 – 1/4"
    var m = String(s).match(/(\d{1,2})\/(\d{1,2})\s*[–-]\s*(\d{1,2})\/(\d{1,2})/);
    if (!m) return null;
    return { m1: +m[1], d1: +m[2], m2: +m[3], d2: +m[4] };
  }
  function currentTermName() {
    if (!TERMS.length) return "全部";
    var now = new Date();
    var mm = now.getMonth() + 1, dd = now.getDate();
    function inRange(r) {
      var a = r.m1 * 100 + r.d1, b = r.m2 * 100 + r.d2, x = mm * 100 + dd;
      if (r.m1 <= r.m2) return x >= a && x <= b;          // 同一年区间
      return x >= a || x <= b;                            // 跨年区间（如冬至）
    }
    for (var i = 0; i < TERMS.length; i++) {
      var r = parseSolar(TERMS[i].solar);
      if (r && inRange(r)) return TERMS[i].name;
    }
    return "全部";
  }

  /* ---------- 6. 列表渲染 ---------- */
  var io = ("IntersectionObserver" in window)
    ? new IntersectionObserver(function (entries) {
        entries.forEach(function (e) {
          if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
        });
      }, { rootMargin: "0px 0px -8% 0px" })
    : null;

  function filtered() {
    var q = state.q.trim().toLowerCase();
    return YangShengContent.LIST.filter(function (a) {
      if (state.fav && !isBM(a.id)) return false;
      if (state.cat !== "全部" && a.category !== state.cat) return false;
      if (state.term !== "全部" && (a.term || "") !== state.term) return false;
      if (q) {
        var hay = (a.title + " " + a.summary + " " + (a.tags || []).join(" ") +
                   " " + a.body + " " + a.category + " " + (a.term || "")).toLowerCase();
        if (hay.indexOf(q) < 0) return false;
      }
      return true;
    });
  }

  function cardHTML(a) {
    var on = isBM(a.id);
    return '<article class="ch-card" data-id="' + escapeAttr(a.id) + '" tabindex="0" role="button" ' +
           'aria-label="' + escapeAttr(a.title) + '">' +
             '<div class="ch-card-top">' +
               '<span class="ch-tag ch-cat-' + escapeAttr(a.category) + '">' + escapeHtml(a.category) + '</span>' +
               '<span class="ch-term">' + escapeHtml(a.term || "顺时") + '</span>' +
             '</div>' +
             '<h3>' + escapeHtml(a.title) + '</h3>' +
             '<p>' + escapeHtml(a.summary) + '</p>' +
             '<div class="ch-card-meta">' +
               '<span>' + (a.readMins || 1) + ' 分钟阅读</span>' +
               '<span class="dot"></span>' +
               '<span>' + escapeHtml(a.author || "") + '</span>' +
               '<button class="ch-bm' + (on ? " on" : "") + '" data-bm="' + escapeAttr(a.id) +
                 '" aria-label="收藏" aria-pressed="' + (on ? "true" : "false") + '">' +
                 '<img src="' + heart(18) + '" alt=""></button>' +
             '</div>' +
           '</article>';
  }

  function render() {
    // 关闭骨架
    if ($skel) $skel.classList.add("hide");

    var list = filtered();
    if (!list.length) {
      $list.innerHTML = "";
      $empty.classList.add("show");
      return;
    }
    $empty.classList.remove("show");

    var html = "";
    for (var i = 0; i < list.length; i++) html += cardHTML(list[i]);
    $list.innerHTML = html;

    // 滚动入场
    var cards = $list.querySelectorAll(".ch-card");
    for (var j = 0; j < cards.length; j++) {
      if (io) io.observe(cards[j]);
      else cards[j].classList.add("in");
    }
  }

  /* ---------- 7. 节气物语 ---------- */
  function renderSeasonNoteFor(name) {
    if (!name || name === "全部") { $season.hidden = true; $season.innerHTML = ""; return; }
    var t = BY_NAME[name];
    if (!t) { $season.hidden = true; $season.innerHTML = ""; return; }
    var yi = (t.yi || []).map(escapeHtml).join("、");
    var ji = (t.ji || []).map(escapeHtml).join("、");
    $season.hidden = false;
    $season.innerHTML =
      '<p class="sn-head"><b>' + escapeHtml(t.name) + '</b> · ' + escapeHtml(t.solar) +
        ' · ' + escapeHtml(t.season + t.phase) + '</p>' +
      '<p class="sn-note">' + escapeHtml(t.note || "") + '</p>' +
      '<p class="sn-yj"><span class="yi">宜　' + yi + '</span><span class="ji">忌　' + ji + '</span></p>';
  }
  function renderSeasonNote() { renderSeasonNoteFor(state.term); }

  /* ---------- 8. 详情滑层 ---------- */
  function openDetail(id) {
    var a = byId(id);
    if (!a) return;
    $dTag.textContent = a.category;
    $dTag.className = "ch-tag ch-cat-" + a.category;
    $dTitle.textContent = a.title;
    $dMeta.innerHTML =
      '<span>' + escapeHtml(a.term || "顺时养生") + '</span><span class="dot"></span>' +
      '<span>' + (a.readMins || 1) + ' 分钟阅读</span><span class="dot"></span>' +
      '<span>' + escapeHtml(a.author || "") + '</span>';
    var paras = String(a.body || "").split("\n");
    var body = "";
    for (var i = 0; i < paras.length; i++) {
      if (paras[i].trim() === "") continue;
      body += "<p>" + escapeHtml(paras[i]) + "</p>";
    }
    $dBody.innerHTML = body;
    $dTags.innerHTML = (a.tags || []).map(function (t) {
      return '<span class="t">' + escapeHtml(t) + "</span>";
    }).join("");

    var on = isBM(a.id);
    $dBm.classList.toggle("on", on);
    $dBmTxt.textContent = on ? "已收藏" : "收藏";
    $detail._id = a.id;

    $detail.classList.add("open");
    $detail.setAttribute("aria-hidden", "false");
    document.body.classList.add("ch-no-scroll");
  }
  function closeDetail() {
    $detail.classList.remove("open");
    $detail.setAttribute("aria-hidden", "true");
    document.body.classList.remove("ch-no-scroll");
  }
  function bmBtn(id, btn, card) {
    var on = toggleBM(id);
    if (btn) { btn.classList.toggle("on", on); btn.setAttribute("aria-pressed", on ? "true" : "false"); }
    if (card) {
      var b = card.querySelector('.ch-bm[data-bm="' + id + '"]');
      if (b) { b.classList.toggle("on", on); b.setAttribute("aria-pressed", on ? "true" : "false"); }
    }
    if ($detail._id === id) { $dBm.classList.toggle("on", on); $dBmTxt.textContent = on ? "已收藏" : "收藏"; }
  }

  /* ---------- 9. 事件绑定 ---------- */
  function bind() {
    // 搜索（输入即筛，含防抖）
    var t = null;
    $search.addEventListener("input", function () {
      state.q = $search.value || "";
      $clear.classList.toggle("show", state.q.length > 0);
      if (t) clearTimeout(t);
      t = setTimeout(render, 140);
    });
    $clear.addEventListener("click", function () {
      $search.value = ""; state.q = ""; $clear.classList.remove("show");
      $search.focus(); render();
    });

    // 分类筛选
    $catF.addEventListener("click", function (e) {
      var chip = e.target.closest(".ch-chip");
      if (!chip) return;
      var c = chip.getAttribute("data-cat");
      if (c === "__fav") state.fav = !state.fav;
      else { state.fav = false; state.cat = c; }
      syncChips(); render();
    });
    // 节气筛选
    $termF.addEventListener("click", function (e) {
      var chip = e.target.closest(".ch-chip");
      if (!chip) return;
      state.term = chip.getAttribute("data-term");
      syncChips(); renderSeasonNote(); render();
    });

    // 列表：卡片点击 / 收藏
    $list.addEventListener("click", function (e) {
      var bm = e.target.closest(".ch-bm");
      if (bm) { e.stopPropagation(); bmBtn(bm.getAttribute("data-bm"), bm, bm.closest(".ch-card")); return; }
      var card = e.target.closest(".ch-card");
      if (card) openDetail(card.getAttribute("data-id"));
    });
    // 键盘可达性
    $list.addEventListener("keydown", function (e) {
      if (e.key !== "Enter" && e.key !== " ") return;
      var card = e.target.closest(".ch-card");
      if (card) { e.preventDefault(); openDetail(card.getAttribute("data-id")); }
    });

    // 详情关闭
    $dClose.addEventListener("click", closeDetail);
    $detail.addEventListener("click", function (e) { if (e.target === $detail) closeDetail(); });
    $dBm.addEventListener("click", function () { if ($detail._id) bmBtn($detail._id, null, null); });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && $detail.classList.contains("open")) closeDetail();
    });

    // 空状态：清除筛选
    $clearF.addEventListener("click", function () {
      state.q = ""; state.cat = "全部"; state.term = "全部"; state.fav = false;
      $search.value = ""; $clear.classList.remove("show");
      syncChips(); renderSeasonNote(); render();
    });
  }

  /* ---------- 10. 启动 ---------- */
  function init() {
    buildChips();
    bind();
    // 默认展示「全部」文章（匹配首页"查看全部"入口）；
    // 同时用当前节气物语作引导提示（数据驱动，不强制筛选）
    state.term = "全部";
    syncChips();
    renderSeasonNoteFor(currentTermName());
    // 短暂呈现加载骨架（真实加载态的占位，模拟数据获取节奏）
    if ($skel) $skel.classList.remove("hide");
    setTimeout(render, 320);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
