/* ============================================================
   site.js — 颐和·智慧康养 机构官网 共享框架与交互（T0 增强版）
   职责：注入顶栏/页脚（含主题切换与字号控制）、当前页高亮、移动端菜单、
        滚动进度、顶栏收缩、返回顶部、移动端悬浮拨号、资讯搜索与空状态、
        图片淡入、FAQ 手风琴、站内图标、留言表单校验与本地提交、结构化数据。
   约定：严格 CSP（script-src 'self'）；不写内联样式；仅操作 DOM。
   ============================================================ */
(function() {
    "use strict";

    document.documentElement.classList.add("js");

    /* 网络配图加载失败时的优雅降级 */
    document.addEventListener("error", function(e) {
        var t = e.target;
        if (t && t.tagName === "IMG") {
            t.classList.add("img-fallback");
        }
    }, true);

    /* ---------- 0. 图标系统（描边，统一 1.8 线宽，无 emoji） ---------- */
    var P = {
        home: '<path d="M3 11l9-7 9 7"/><path d="M5 10v9a1 1 0 0 0 1 1h4v-6h4v6h4a1 1 0 0 0 1-1v-9"/>',
        intro: '<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 4-6 8-6s8 2 8 6"/>',
        service: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><circle cx="17.5" cy="17.5" r="3.5"/>',
        news: '<path d="M4 5h13a2 2 0 0 1 2 2v12a2 2 0 0 0 2-2V7"/><path d="M4 5v14a2 2 0 0 0 2 2h13"/><path d="M7 9h7M7 13h7M7 17h4"/>',
        guide: '<path d="M9 4h6a2 2 0 0 1 2 2v14l-5-3-5 3V6a2 2 0 0 1 2-2z"/><path d="M9 4H7a2 2 0 0 0-2 2v0"/>',
        contact: '<path d="M4 5h16v14H4z"/><path d="M4 7l8 6 8-6"/>',
        message: '<path d="M21 12a8 8 0 0 1-11.3 7.3L4 21l1.7-5.7A8 8 0 1 1 21 12z"/>',
        phone: '<path d="M5 4h3l2 5-2 1a11 11 0 0 0 5 5l1-2 5 2v3a2 2 0 0 1-2 2A16 16 0 0 1 3 6a2 2 0 0 1 2-2z"/>',
        location: '<path d="M12 21s7-6 7-11a7 7 0 1 0-14 0c0 5 7 11 7 11z"/><circle cx="12" cy="10" r="2.5"/>',
        clock: '<circle cx="12" cy="12" r="8"/><path d="M12 8v4l3 2"/>',
        check: '<path d="M5 12l5 5 9-11"/>',
        arrowRight: '<path d="M5 12h14M13 6l6 6-6 6"/>',
        menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
        close: '<path d="M6 6l12 12M18 6L6 18"/>',
        heart: '<path d="M12 20s-7-4.5-9.5-9C1 8 3 4.5 6.5 4.5 9 4.5 12 7 12 7s3-2.5 5.5-2.5C21 4.5 23 8 21.5 11 19 15.5 12 20 12 20z"/>',
        shield: '<path d="M12 3l7 3v6c0 4.5-3 8-7 9-4-1-7-4.5-7-9V6z"/>',
        food: '<path d="M5 3v8a2 2 0 0 0 4 0V3M7 11v10M17 3c-2 0-3 3-3 7s1 5 3 5v6"/>',
        rehab: '<path d="M4 18l7-7 3 3-7 7z"/><path d="M14 11l5-5M18 4l3 3"/>',
        chat: '<path d="M4 5h16v11H9l-5 4z"/>',
        users: '<circle cx="9" cy="8" r="3.2"/><path d="M3 20c0-3.3 2.7-5 6-5s6 1.7 6 5"/><path d="M16 5a3 3 0 0 1 0 6M21 20c0-2.6-1.6-4.2-4-4.8"/>',
        leaf: '<path d="M5 19c0-8 6-14 14-14 0 8-6 14-14 14z"/><path d="M5 19c4-4 7-7 11-9"/>',
        sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/>',
        moon: '<path d="M21 12.8A8.5 8.5 0 1 1 11.2 3 6.5 6.5 0 0 0 21 12.8z"/>',
        search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
        mail: '<path d="M3 6h18v12H3z"/><path d="M3 7l9 6 9-6"/>',
        star: '<path d="M12 3l2.6 5.6L21 9.3l-4.5 4.3 1.1 6.4L12 17l-5.6 3 1.1-6.4L3 9.3l6.4-.7z"/>',
        care: '<path d="M12 21s-7-4.5-9.5-9C1 8 3 4.5 6.5 4.5 9 4.5 12 7 12 7s3-2.5 5.5-2.5C21 4.5 23 8 21.5 11 19 15.5 12 21 12 21z"/><path d="M8 12h8M12 8v8"/>',
        stairs: '<path d="M4 20h4v-4h4v-4h4V8h4"/>',
        brain: '<path d="M9 4a3 3 0 0 0-3 3 3 3 0 0 0-1 5 3 3 0 0 0 2 5 3 3 0 0 0 3 2V4z"/><path d="M15 4a3 3 0 0 1 3 3 3 3 0 0 1 1 5 3 3 0 0 1-2 5 3 3 0 0 1-3 2V4z"/>',
        ambulance: '<rect x="2" y="7" width="13" height="10" rx="1"/><path d="M15 10h4l3 3v4h-7z"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="19" r="2"/><path d="M9 11v3M7.5 12.5h3"/>',
        flower: '<circle cx="12" cy="12" r="3"/><path d="M12 9a3 3 0 0 1 0-5 3 3 0 0 1 0 5zM12 15a3 3 0 0 0 0 5 3 3 0 0 0 0-5zM9 12a3 3 0 0 0-5 0 3 3 0 0 0 5 0zM15 12a3 3 0 0 1 5 0 3 3 0 0 1-5 0z"/>',
        inbox: '<path d="M3 13h5l2 3h4l2-3h5"/><path d="M5 5h14l2 8v5a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-5z"/>',
        cloud: '<path d="M18 18H6a5 5 0 0 1-.9-9.92A6.5 6.5 0 0 1 17.7 8 4.5 4.5 0 0 1 18 18z"/>'
    };

    function siteIcon(name, size, color) {
        var inner = P[name] || P.star;
        var s = size || 24,
            c = color || "currentColor";
        return '<svg viewBox="0 0 24 24" width="' + s + '" height="' + s + '" fill="none" ' +
            'stroke="' + c + '" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
            inner + '</svg>';
    }

    function paintIcons(root) {
        (root || document).querySelectorAll("[data-s-icon]").forEach(function(el) {
            var sz = el.getAttribute("data-size") || 24;
            el.innerHTML = siteIcon(el.getAttribute("data-s-icon"), sz);
        });
    }

    /* ---------- 1. 导航与页脚结构 ---------- */
    var NAV = [{
        href: "home.html",
        label: "首页",
        icon: "home"
    }, {
        href: "about.html",
        label: "机构简介",
        icon: "intro"
    }, {
        href: "services.html",
        label: "服务内容",
        icon: "service"
    }, {
        href: "news.html",
        label: "养老资讯",
        icon: "news"
    }, {
        href: "guide.html",
        label: "入住指南",
        icon: "guide"
    }, {
        href: "contact.html",
        label: "联系我们",
        icon: "contact"
    }];
    var APP_URL = "index.html";

    function currentFile() {
        var p = location.pathname.split("/").pop();
        return p || "home.html";
    }

    function buildHeader() {
        var cur = currentFile();
        var links = NAV.map(function(n) {
            var active = (n.href === cur) ? " active" : "";
            return '<a href="' + n.href + '" class="' + active.trim() + '" data-nav>' + n.label + '</a>';
        }).join("");
        var isDark = document.documentElement.getAttribute("data-theme") === "dark";
        return '' +
            '<header class="site-header">' +
            '<div class="container">' +
            '<nav class="nav" aria-label="主导航">' +
            '<a class="brand" href="home.html" aria-label="颐和康养 首页">' +
            '<span class="brand-seal">颐</span>' +
            '<span><span class="brand-name">颐和·智慧康养</span><br><span class="brand-sub">YIHE SENIOR CARE</span></span>' +
            '</a>' +
            '<button class="nav-toggle" id="navToggle" aria-label="展开菜单" aria-expanded="false">' +
            siteIcon("menu", 24) + '</button>' +
            '<div class="nav-links" id="navLinks">' + links +
            '<a class="nav-cta" href="' + APP_URL + '">进入康养平台</a>' +
            '</div>' +
            '<div class="nav-tools">' +
            '<div class="font-ctrl" role="group" aria-label="字号调节">' +
            '<button type="button" data-font="down" aria-label="减小字号">A−</button>' +
            '<button type="button" data-font="up" aria-label="增大字号">A＋</button>' +
            '</div>' +
            '<button class="tool-btn" id="themeToggle" type="button" aria-label="切换深色模式" aria-pressed="' + (isDark ? "true" : "false") + '">' +
            siteIcon(isDark ? "sun" : "moon", 20) + '</button>' +
            '</div>' +
            '</nav></div></header>';
    }

    function buildFooter() {
        var y = new Date().getFullYear();
        return '' +
            '<footer class="site-footer">' +
            '<div class="container">' +
            '<div class="foot-grid">' +
            '<div class="foot-brand">' +
            '<a class="brand" href="home.html" style="color:#fff"><span class="brand-seal">颐</span>' +
            '<span><span class="brand-name" style="color:#fff">颐和·智慧康养</span><br>' +
            '<span class="brand-sub" style="color:#B9AC96">YIHE SENIOR CARE</span></span></a>' +
            '<p>以“如家般温暖、如医般专业”为理念，为长者提供生活照料、医疗护理与精神关怀一体的品质康养服务。</p>' +
            '</div>' +
            '<div class="foot-col"><h5>快速导航</h5><ul>' +
            '<li><a href="home.html">首页</a></li>' +
            '<li><a href="about.html">机构简介</a></li>' +
            '<li><a href="services.html">服务内容</a></li>' +
            '<li><a href="news.html">养老资讯</a></li>' +
            '</ul></div>' +
            '<div class="foot-col"><h5>了解更多</h5><ul>' +
            '<li><a href="guide.html">入住指南</a></li>' +
            '<li><a href="contact.html">联系我们</a></li>' +
            '<li><a href="contact.html#message">在线留言咨询</a></li>' +
            '<li><a href="index.html">居民康养平台</a></li>' +
            '</ul></div>' +
            '<div class="foot-col foot-contact"><h5>联系机构</h5>' +
            '<p>' + siteIcon("location", 18) + ' 杭州市拱墅区和睦街道颐和路 18 号</p>' +
            '<p>' + siteIcon("phone", 18) + ' <a href="tel:4008231998">400-823-1998</a></p>' +
            '<p>' + siteIcon("clock", 18) + ' 参观接待 每日 9:00–17:30</p>' +
            '</div>' +
            '</div>' +
            '<div class="foot-bottom">' +
            '<span>© ' + y + ' 颐和康养服务中心 · 浙ICP备 1800XXXX 号</span>' +
            '<span><a href="about.html">关于我们</a><a href="guide.html">入住流程</a><a href="contact.html">隐私政策</a></span>' +
            '</div>' +
            '</div></footer>';
    }

    /* ---------- 2. 注入 & 绑定 ---------- */
    function inject() {
        var h = document.getElementById("site-header");
        var f = document.getElementById("site-footer");
        if (h) h.innerHTML = buildHeader();
        if (f) f.innerHTML = buildFooter();
        paintIcons(document);

        var toggle = document.getElementById("navToggle");
        var links = document.getElementById("navLinks");
        if (toggle && links) {
            toggle.addEventListener("click", function() {
                var open = links.classList.toggle("open");
                toggle.setAttribute("aria-expanded", open ? "true" : "false");
                toggle.innerHTML = siteIcon(open ? "close" : "menu", 24);
            });
            links.addEventListener("click", function(e) {
                if (e.target.closest("a") && links.classList.contains("open")) {
                    links.classList.remove("open");
                    toggle.setAttribute("aria-expanded", "false");
                    toggle.innerHTML = siteIcon("menu", 24);
                }
            });
        }

        bindThemeToggle();
        bindFontCtrl();
    }

    /* ---------- 3. 主题切换（持久化） ---------- */
    function bindThemeToggle() {
        var btn = document.getElementById("themeToggle");
        if (!btn) return;
        btn.addEventListener("click", function() {
            var dark = document.documentElement.getAttribute("data-theme") === "dark";
            var next = dark ? "light" : "dark";
            document.documentElement.setAttribute("data-theme", next);
            try {
                localStorage.setItem("yihe-theme", next);
            } catch (e) {}
            btn.setAttribute("aria-pressed", next === "dark" ? "true" : "false");
            btn.innerHTML = siteIcon(next === "dark" ? "sun" : "moon", 20);
        });
    }

    /* ---------- 4. 字号调节（老年友好，持久化） ---------- */
    function bindFontCtrl() {
        var root = document.documentElement;
        var saved = 1;
        try {
            saved = parseFloat(localStorage.getItem("yihe-text-scale")) || 1;
        } catch (e) {}
        saved = Math.min(1.35, Math.max(0.9, saved));
        root.style.setProperty("--text-scale", saved);
        document.querySelectorAll("[data-font]").forEach(function(b) {
            b.addEventListener("click", function() {
                var cur = parseFloat(getComputedStyle(root).getPropertyValue("--text-scale")) || 1;
                var step = b.getAttribute("data-font") === "up" ? 0.1 : -0.1;
                var v = Math.min(1.35, Math.max(0.9, Math.round((cur + step) * 100) / 100));
                root.style.setProperty("--text-scale", v);
                try {
                    localStorage.setItem("yihe-text-scale", v);
                } catch (e) {}
            });
        });
    }

    /* ---------- 5. 顶栏滚动收缩 ----------
       注：站点已移除全部浮动按钮 / 悬浮控件（返回顶部、浮动电话、滚动进度条）。
       页面不再有任何遮挡内容的固定浮层，仅保留顶栏本身的轻微收缩反馈。 */
    function bindScrollFx() {
        var header = document.querySelector(".site-header");
        if (!header) return;
        var ticking = false;

        function onScroll() {
            if (ticking) return;
            ticking = true;
            requestAnimationFrame(function() {
                var y = window.scrollY || window.pageYOffset;
                header.classList.toggle("scrolled", y > 20);
                ticking = false;
            });
        }
        window.addEventListener("scroll", onScroll, {
            passive: true
        });
        onScroll();
    }

    /* ---------- 5b. 图文映射校验（防止配图与文案错配） ---------- */
    function applyImageMap() {
        if (!window.YIHE_IMG) return;
        var r = window.YIHE_IMG.apply(document);
        if (r.missing && window.console && console.warn) {
            console.warn("[image-map] 有 " + r.missing + " 处配图未在映射表中登记。");
        }
    }

    /* ---------- 6. 图片淡入（渐进增强） ---------- */
    function bindImageFade() {
        var imgs = document.querySelectorAll(".zig-media img,.news-thumb img,.article-cover img,.hero-card img");
        imgs.forEach(function(img) {
            img.classList.add("fade");
            if (img.complete && img.naturalWidth > 0) {
                img.classList.add("loaded");
            } else {
                img.addEventListener("load", function() {
                    img.classList.add("loaded");
                });
                img.addEventListener("error", function() {
                    img.classList.add("loaded");
                });
            }
        });
    }

    /* ---------- 7. 滚动入场 ---------- */
    function reveal() {
        var els = document.querySelectorAll(".reveal");
        if (!els.length) return;
        if (!("IntersectionObserver" in window)) {
            els.forEach(function(e) {
                e.classList.add("in");
            });
            return;
        }
        var io = new IntersectionObserver(function(entries) {
            entries.forEach(function(en) {
                if (en.isIntersecting) {
                    en.target.classList.add("in");
                    io.unobserve(en.target);
                }
            });
        }, {
            threshold: 0.12,
            rootMargin: "0px 0px -8% 0px"
        });
        els.forEach(function(e) {
            io.observe(e);
        });
    }

    /* ---------- 8. FAQ 手风琴 ---------- */
    function bindFaq() {
        document.querySelectorAll(".faq-item").forEach(function(item) {
            var q = item.querySelector(".faq-q");
            var a = item.querySelector(".faq-a");
            if (!q || !a) return;
            q.addEventListener("click", function() {
                var open = item.classList.toggle("open");
                q.setAttribute("aria-expanded", open ? "true" : "false");
                a.style.maxHeight = open ? (a.scrollHeight + "px") : "0px";
            });
        });
    }

    /* ---------- 9. 资讯：分类筛选 + 关键词搜索 + 空状态 ---------- */
    function bindNewsFilter() {
        var bar = document.querySelector(".filter-bar");
        var search = document.getElementById("newsSearch");
        var grid = document.querySelector(".news-grid");
        if (!grid) return;
        var cards = Array.prototype.slice.call(grid.querySelectorAll(".news-card"));

        // 空状态节点（缺失则创建）
        var empty = grid.parentNode.querySelector(".empty-state");
        if (!empty) {
            empty = document.createElement("div");
            empty.className = "empty-state";
            empty.innerHTML = '<span class="es-ico">' + siteIcon("inbox", 28) + '</span>' +
                '<h3>没有找到匹配的资讯</h3><p>换个关键词，或点“全部”查看所有养老资讯。</p>';
            grid.insertAdjacentElement("afterend", empty);
        }

        var curCat = "全部";
        var curQ = "";

        function apply() {
            var shown = 0;
            cards.forEach(function(c) {
                var cat = c.getAttribute("data-cat") || "";
                var hay = (c.textContent || "").toLowerCase();
                var okCat = (curCat === "全部") || (cat === curCat);
                var okQ = !curQ || hay.indexOf(curQ) !== -1;
                var ok = okCat && okQ;
                c.style.display = ok ? "" : "none";
                if (ok) shown++;
            });
            empty.classList.toggle("show", shown === 0);
        }

        if (bar) {
            bar.addEventListener("click", function(e) {
                var btn = e.target.closest("button");
                if (!btn) return;
                curCat = btn.getAttribute("data-cat");
                bar.querySelectorAll("button").forEach(function(b) {
                    b.classList.remove("active");
                });
                btn.classList.add("active");
                apply();
            });
        }
        if (search) {
            search.addEventListener("input", function() {
                curQ = (search.value || "").trim().toLowerCase();
                apply();
            });
        }
    }

    /* ---------- 10. 留言表单校验 + 本地提交 ---------- */
    function bindForms() {
        document.querySelectorAll("form[data-form]").forEach(function(form) {
            var saveKey = form.getAttribute("data-form");
            form.addEventListener("submit", function(e) {
                e.preventDefault();
                var ok = true;
                form.querySelectorAll(".field").forEach(function(field) {
                    var input = field.querySelector("input,textarea,select");
                    if (!input) return;
                    var val = (input.value || "").trim();
                    var bad = false;
                    if (input.hasAttribute("required") && !val) bad = true;
                    if (!bad && input.type === "tel" && val) {
                        if (!/^1[3-9]\d{9}$/.test(val) && !/^\d{3,4}-?\d{7,8}$/.test(val)) bad = true;
                    }
                    if (!bad && input.type === "email" && val) {
                        if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(val)) bad = true;
                    }
                    if (!bad && input.getAttribute("minlength") && val.length < +input.getAttribute("minlength")) bad = true;
                    field.classList.toggle("invalid", bad);
                    if (bad) ok = false;
                });
                if (!ok) {
                    var firstBad = form.querySelector(".field.invalid input, .field.invalid textarea, .field.invalid select");
                    if (firstBad) firstBad.focus();
                    return;
                }
                var data = {};
                form.querySelectorAll("input,textarea,select").forEach(function(i) {
                    if (i.name) data[i.name] = i.value.trim();
                });
                data._at = new Date().toISOString();
                try {
                    var arr = JSON.parse(localStorage.getItem(saveKey) || "[]");
                    arr.push(data);
                    localStorage.setItem(saveKey, JSON.stringify(arr));
                } catch (e) { alert("本机保存失败，留言没有保存。请检查浏览器存储空间。"); return; }
                var success = form.parentElement.querySelector(".form-success");
                if (!success) success = form.querySelector(".form-success");
                if (success) success.classList.add("show");
                form.reset();
                if (success) success.scrollIntoView({
                    behavior: "smooth",
                    block: "center"
                });
            });
        });
    }

    /* ---------- 11. 结构化数据（JSON-LD，SEO） ---------- */
    function injectJsonLd() {
        if (document.getElementById("yihe-ld")) return;
        var data = {
            "@context": "https://schema.org",
            "@type": "Organization",
            "name": "颐和·智慧康养",
            "description": "杭州拱墅区医养结合品质养老机构，提供生活照料、医疗护理、康复理疗、营养膳食与精神关怀一体的长者服务。",
            "address": {
                "@type": "PostalAddress",
                "addressLocality": "杭州市拱墅区",
                "addressRegion": "浙江省",
                "streetAddress": "和睦街道颐和路 18 号"
            },
            "telephone": "+86-400-823-1998",
            "url": "https://yihe.example.com"
        };
        var s = document.createElement("script");
        s.type = "application/ld+json";
        s.id = "yihe-ld";
        s.textContent = JSON.stringify(data);
        document.head.appendChild(s);
    }

    /* ---------- 11b. 云纹分隔带（自动插入 section 之间） ---------- */
    function insertCloudDividers() {
        var secs = document.querySelectorAll("main > .section");
        for (var i = 0; i < secs.length - 1; i++) {
            var cur = secs[i],
                next = secs[i + 1];
            /* 仅在两个实色/交替背景之间插入，跳过紧邻的 tint 段 */
            if (cur.classList.contains("section--tight") || next.classList.contains("section--tight")) continue;
            if (cur.classList.contains("section--tint") && next.classList.contains("section--tint")) continue;
            var div = document.createElement("div");
            div.className = "cloud-divider";
            div.innerHTML = siteIcon("cloud", 28);
            next.parentNode.insertBefore(div, next);
        }
    }

    /* ---------- 12. 启动 ---------- */
    function start() {
        inject();
        applyImageMap();
        bindScrollFx();
        bindImageFade();
        reveal();
        bindFaq();
        bindNewsFilter();
        bindForms();
        injectJsonLd();
        paintIcons(document);
        insertCloudDividers();
    }
    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", start);
    } else {
        start();
    }
    window.siteIcon = siteIcon;
})();