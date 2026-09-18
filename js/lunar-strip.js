/*
 * lunar-strip.js — 首页农历干支条（原内联脚本因 CSP 被拦截，移至此外部模块）
 * 依赖：js/solarlunar.min.js（提供 window.solarLunar）
 * 作用：把 #tab-home .lunar-cell 的「干支年 / 农历月日 / 星期」更新为真实日期。
 * 约定：严格 CSP（script-src 'self'）；不使用内联脚本、不使用内联样式。
 */
(function () {
  "use strict";

  function getConv() {
    var L = window.solarLunar;
    if (!L) return null;
    if (typeof L.solar2lunar === "function") return L;
    if (L.default && typeof L.default.solar2lunar === "function") return L.default;
    return null;
  }

  function update() {
    var conv = getConv();
    if (!conv) return;
    var d = new Date();
    var lu;
    try { lu = conv.solar2lunar(d.getFullYear(), d.getMonth() + 1, d.getDate()); }
    catch (e) { return; }
    if (!lu) return;

    var cells = document.querySelectorAll("#tab-home .lunar-cell");
    if (!cells || cells.length < 2) return;

    var v0 = cells[0].querySelector(".value");
    if (v0) v0.textContent = lu.gzYear + "年";

    var v1 = cells[1].querySelector(".value");
    if (v1) v1.textContent = lu.monthCn + lu.dayCn;
    var s1 = cells[1].querySelector(".sub");
    if (s1) s1.textContent = lu.ncWeek || "";

    // 第三个 cell：有节气则显示节气，否则保留原样
    if (cells[2]) {
      var v2 = cells[2].querySelector(".value");
      if (v2 && lu.Term) v2.textContent = lu.Term;
      var s2 = cells[2].querySelector(".sub");
      if (s2 && lu.Animal) s2.textContent = lu.Animal + "年";
    }
  }

  function init() {
    update();
    // 切回首页时再刷新一次（首页可能延迟渲染）
    var homeNav = document.querySelector('[data-tab="home"]');
    if (homeNav) homeNav.addEventListener("click", function () { setTimeout(update, 60); });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  // 供其他模块复用
  window.updateLunarStrip = update;
})();
