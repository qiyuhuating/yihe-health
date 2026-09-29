/* Shared theme preference for public, resident and administrator pages. */
(function() {
    "use strict";
    var KEY = "yihe-theme";
    function valid(value) { return value === "dark" || value === "light"; }
    function resolve() {
        try { var current = localStorage.getItem(KEY); if (valid(current)) return current; } catch (e) {}
        try { var resident = JSON.parse(localStorage.getItem("hm-p-theme")); if (valid(resident)) return resident; } catch (e) {}
        try {
            var admin = JSON.parse(localStorage.getItem("hm-settings"));
            if (admin && typeof admin.darkMode === "boolean") return admin.darkMode ? "dark" : "light";
        } catch (e) {}
        return window.matchMedia && matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    var current = resolve();
    try {
        if (!valid(localStorage.getItem(KEY)) && (localStorage.getItem("hm-p-theme") || localStorage.getItem("hm-settings")))
            localStorage.setItem(KEY, current);
    } catch (e) {}
    function apply(value) {
        current = value;
        document.documentElement.setAttribute("data-theme", value);
        if (document.body) {
            document.body.classList.toggle("dark", value === "dark");
            document.body.classList.toggle("light", value === "light");
        }
        var admin = document.getElementById("settingDarkMode");
        if (admin) admin.checked = value === "dark";
        ["themeToggle", "themeToggleMe"].forEach(function(id) {
            var toggle = document.getElementById(id);
            if (toggle) toggle.setAttribute("aria-pressed", String(value === "dark"));
        });
        window.dispatchEvent(new CustomEvent("yihe:themechange", {detail: value}));
    }
    window.YiheTheme = {
        get: function() { return current; },
        apply: apply,
        set: function(value) {
            if (!valid(value)) return false;
            try { localStorage.setItem(KEY, value); }
            catch (e) {
                apply(current);
                if (typeof showToast === "function") showToast("主题保存失败，请检查浏览器存储空间");
                else window.alert("主题保存失败，请检查浏览器存储空间");
                return false;
            }
            apply(value);
            return true;
        }
    };
    document.documentElement.setAttribute("data-theme", current);
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", function() { apply(current); });
    else apply(current);
    window.addEventListener("storage", function(event) {
        if (event.key === KEY || event.key === null) apply(resolve());
    });
})();
