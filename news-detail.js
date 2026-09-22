/* news-detail.js — 养老资讯详情渲染（读取 ?id，渲染正文与相关推荐）
   依赖：data/news-site.js（window.YIHE_NEWS）、site.js（window.siteIcon）
   约定：CSP 合规，仅外部脚本；内容为本机构数据，可信，直接拼接。 */
(function() {
    "use strict";

    function esc(s) {
        return String(s == null ? "" : s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    }

    function arrow() {
        return window.siteIcon ? window.siteIcon("arrowRight", 18) : "›";
    }

    function back() {
        return window.siteIcon ? window.siteIcon("arrowRight", 18) : "‹";
    }

    function blockHtml(b) {
        if (typeof b === "string") return "<p>" + esc(b) + "</p>";
        if (b && b.h) return "<h2>" + esc(b.h) + "</h2><p>" + esc(b.p) + "</p>";
        if (b && b.quote) return "<blockquote>" + esc(b.quote) + "</blockquote>";
        return "";
    }

    function cardHtml(a) {
        return '' +
            '<a class="news-card" href="news-detail.html?id=' + a.id + '" data-cat="' + esc(a.cat) + '">' +
            '<div class="news-thumb"><img src="' + a.cover + '" alt="' + esc(a.title) + '" loading="lazy" decoding="async"></div>' +
            '<div class="news-body">' +
            '<div class="news-meta"><span class="chip ' + (a.catCls || "") + '">' + esc(a.cat) + '</span><span>' + esc(a.date) + '</span></div>' +
            '<h3>' + esc(a.title) + '</h3>' +
            '<p>' + esc(a.excerpt) + '</p>' +
            '<span class="news-more">阅读全文 ' + arrow() + '</span>' +
            '</div></a>';
    }

    function render() {
        var all = (window.YIHE_NEWS || []);
        var params = new URLSearchParams(location.search);
        var id = params.get("id");
        var art = null;
        for (var i = 0; i < all.length; i++)
            if (all[i].id === id) {
                art = all[i];
                break;
            }

        var box = document.getElementById("article");
        var rel = document.getElementById("related");
        if (!box) return;

        if (!art) {
            box.innerHTML = '<a class="back-link" href="news.html">' + back() + ' 返回资讯列表</a>' +
                '<h1>没有找到这篇资讯</h1><p style="color:var(--ink-2);margin-top:12px">它可能已下架或链接有误。' +
                '<a href="news.html" style="color:var(--accent)">回到养老资讯 ›</a></p>';
            if (rel) rel.closest("section").style.display = "none";
            return;
        }

        document.title = art.title + " | 颐和·智慧康养";
        var meta = '<div class="article-meta"><span class="chip ' + (art.catCls || "") + '">' + esc(art.cat) +
            '</span><span>' + esc(art.date) + '</span></div>';
        var body = art.body.map(blockHtml).join("");
        box.innerHTML =
            '<a class="back-link" href="news.html">' + back() + ' 返回资讯列表</a>' +
            '<div class="article-head">' + meta + '<h1>' + esc(art.title) + '</h1></div>' +
            '<div class="article-cover"><img src="' + art.cover + '" alt="' + esc(art.title) + '" loading="lazy" decoding="async"></div>' +
            '<div class="article-body">' + body +
            '<p style="font-family:var(--font-sans);font-size:.95rem;color:var(--ink-3);margin-top:28px">本文为颐和康养资讯科普，不构成医疗或法律建议。具体政策以当地主管部门最新公告为准。</p>' +
            '</div>';

        // 相关推荐：优先同分类，其次补足至 3 条
        if (rel) {
            var others = all.filter(function(x) {
                return x.id !== art.id;
            });
            var same = others.filter(function(x) {
                return x.cat === art.cat;
            });
            var rest = others.filter(function(x) {
                return x.cat !== art.cat;
            });
            var pick = same.concat(rest).slice(0, 3);
            rel.innerHTML = pick.map(cardHtml).join("");
        }
    }

    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", render);
    else render();
})();