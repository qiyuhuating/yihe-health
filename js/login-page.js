/*
 * 登录页专用脚本（login.html）
 * -------------------------------------------------------------
 * 设计说明：
 *   login.html 是「独立登录页」，而 js/login.js 是原项目为「个人端内登录浮层」
 *   设计的模块，依赖 window.PATIENTS / Audit / App 等仅在个人端加载的全局变量，
 *   在 login.html 下缺失，会导致校验失败、重定向错页、且会因缺依赖抛错。
 *   因此登录页使用本自包含脚本，直接对接全站唯一数据源 data/residents.js。
 *
 * 会话契约（与 js/login.js / 个人端.html 保持一致）：
 *   成功登录后写入 localStorage["hm-login"] = JSON.stringify({uid, name, ts})
 *   个人端.html 加载时读取该会话，有效则直接进入，无效则跳回本页。
 */
(function () {
  "use strict";

  // 居民数据：取自 data/residents.js（全站单一数据源 RESIDENT_DATA）
  var RES = window.RESIDENT_DATA || {};
  var PATIENTS = Object.keys(RES)
    .map(function (k) { return RES[k]; })
    .sort(function (a, b) { return a.id - b.id; });

  var SESSION_KEY = "hm-login";
  var DEFAULT_PWD = "123456";

  function showError(msg) {
    var hint = document.getElementById("loginHint");
    if (hint) {
      hint.textContent = msg;
      hint.hidden = false;
    }
  }

  function doLogin() {
    var nameEl = document.getElementById("loginName");
    var pwdEl = document.getElementById("loginPassword");
    var name = nameEl ? nameEl.value.trim() : "";
    var pwd = pwdEl ? pwdEl.value : "";

    if (!name) { showError("请输入姓名"); return; }

    var user = PATIENTS.filter(function (p) { return p.name === name; })[0];
    if (!user) { showError("姓名不存在，请核对后重试"); return; }

    if (pwd !== DEFAULT_PWD) { showError("密码错误，请核对后重试"); return; }

    try {
      localStorage.setItem(
        SESSION_KEY,
        JSON.stringify({ uid: user.id, name: user.name, ts: Date.now() })
      );
    } catch (e) { /* localStorage 不可用时忽略，仍尝试跳转 */ }

    // 写入标准会话后跳转个人端
    location.href = "个人端.html";
  }

  // 表单提交（拦截默认刷新，避免冲掉重定向）
  var form = document.getElementById("loginForm");
  if (form) {
    form.addEventListener("submit", function (e) {
      e.preventDefault();
      doLogin();
    });
  }

  // 按钮点击
  var btn = document.getElementById("loginSubmit");
  if (btn) {
    btn.addEventListener("click", function (e) {
      e.preventDefault();
      doLogin();
    });
  }

  // 输入框回车：姓名 -> 密码；密码 -> 提交
  var nameEl = document.getElementById("loginName");
  if (nameEl) {
    nameEl.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        var p = document.getElementById("loginPassword");
        if (p) p.focus();
      }
    });
  }
  var pwdEl = document.getElementById("loginPassword");
  if (pwdEl) {
    pwdEl.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        doLogin();
      }
    });
  }
})();
