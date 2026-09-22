/* theme.js — 在首屏绘制前应用主题，避免深色模式闪烁（FOUC）
   读取 localStorage('yihe-theme')；首次访问跟随系统偏好。 */
(function() {
    "use strict";
    var saved = null;
    try {
        saved = localStorage.getItem("yihe-theme");
    } catch (e) {}
    var theme = saved;
    if (!theme) {
        var mq = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)");
        theme = (mq && mq.matches) ? "dark" : "light";
    }
    document.documentElement.setAttribute("data-theme", theme);
})();