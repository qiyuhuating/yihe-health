/**
 * common.js — 共享工具模块
 * 个人端 & 管理端共用
 * v2.1 新增：消除 personal.js 与 app.js 之间的重复代码
 */

/* 快捷 DOM 选择器（注意：Chrome DevTools 中 $ 也是 querySelector 别名，行为一致） */
function $(s) {
    return document.querySelector(s);
}

function $$(s) {
    return document.querySelectorAll(s);
}

/* 安全 DOM 获取：失败时 warn 而非静默崩溃，返回 null 让调用方自行判断 */
function getEl(id) {
    var el = document.getElementById(id);
    if (!el) console.warn('[DOM] 元素 #' + id + ' 未找到，请检查 HTML 是否被修改');
    return el;
}

/* 当前时间格式化 HH:MM */
function nowTime() {
    return new Date().toLocaleTimeString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit'
    });
}

/* Toast 通知 */
function showToast(msg) {
    var t = document.createElement("div");
    t.className = "error-toast";
    t.setAttribute("role", "status");
    t.setAttribute("aria-live", "polite");
    t.textContent = msg;
    document.body.appendChild(t);
    setTimeout(function() {
        t.remove();
    }, 4000);
}

/* HTML 转义（防 XSS） */
function escapeHtml(str) {
    if (!str && str !== 0) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

/* 简单模式匹配（chat-engine 意图匹配用） */
function _msgHas(msg, pattern) {
    if (!msg) return false;
    var pats = pattern.split('|');
    for (var i = 0; i < pats.length; i++) {
        if (msg.indexOf(pats[i]) >= 0) return true;
    }
    return false;
}

/* localStorage 持久化 */
function loadJSON(key, def) {
    try {
        var r = localStorage.getItem(key);
        return r ? JSON.parse(r) : def;
    } catch (e) {
        return def;
    }
}

function saveJSON(key, val) {
    try {
        localStorage.setItem(key, JSON.stringify(val));
        return true;
    } catch (e) {
        console.warn('localStorage write failed:', key, e.message);
        showToast('保存失败，请检查浏览器存储空间');
        return false;
    }
}

/* ===== 按用户命名的存储 key（与趋势 hm-trends-{uid} / 步数 hm-steps-{uid} 既有约定一致） ===== */
function userKey(base, uid) {
    return base + '-' + (uid == null || uid === '' ? 0 : uid);
}
/* Only read records whose owner is known; never assign legacy global records to a user. */
function loadUserData(base, uid, def) { return loadJSON(userKey(base, uid), def); }

/* 消息格式化：处理换行 + 代码块 ``` (必须在 escapeHtml 之前调用) */
function formatMsg(text) {
    if (!text) return '';
    var parts = String(text).split(/(```\w*\n[\s\S]*?\n```)/g);
    for (var i = 0; i < parts.length; i++) {
        if (/^```/.test(parts[i])) {
            // 代码块：提取语言标签和内容，escapeHtml 后包在 pre/code 中
            var lines = parts[i].split('\n');
            var lang = lines[0].replace(/```/, '') || '';
            var code = lines.slice(1, -1).join('\n');
            parts[i] = '<pre><code' + (lang ? ' class="language-' + escapeHtml(lang) + '"' : '') + '>' + escapeHtml(code) + '</code></pre>';
        } else {
            // 普通文本：escape + \n → <br>
            parts[i] = escapeHtml(parts[i]).replace(/\n/g, '<br>');
        }
    }
    return parts.join('');
}

/* 头像图片读取（personal.js & app.js 共用，消除 3 处重复 FileReader 逻辑） */
function readImageFile(file, callback, onError) {
    if (!file) return;
    if (!/^image\/(jpeg|png|webp|gif)$/.test(file.type)) {
        showToast('请选择 JPG、PNG、WEBP 或 GIF 图片');
        if (onError) onError(new Error('图片格式不支持'));
        return;
    }
    if (file.size > 5 * 1024 * 1024) {
        showToast('头像不能超过 5MB');
        if (onError) onError(new Error('图片超过 5MB'));
        return;
    }
    var reader = new FileReader();
    reader.onload = function() {
        callback(reader.result);
    };
    reader.onerror = function() {
        showToast('头像读取失败，请重试');
        if (onError) onError(new Error('头像读取失败'));
    };
    reader.readAsDataURL(file);
}

/* 头像 DOM 渲染（personal.js & app.js 共用） */
function applyAvatarImg(imgEl, fallbackEl, src) {
    if (!imgEl || !fallbackEl) return;
    if (src) {
        imgEl.src = src;
        imgEl.classList.add('show');
        fallbackEl.classList.add('hidden');
    } else {
        imgEl.classList.remove('show');
        fallbackEl.classList.remove('hidden');
    }
}

/* ===== SVG 图标系统 — 替代全部 emoji ===== */
function _svg(src) {
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(src);
}

/* 新中式首字头像 — 姓名哈希选色 + 毛笔首字 */
var AVATAR_COLORS = [{
    c: '#9E3B2E',
    d: '#7A2D22'
}, {
    c: '#5E7B57',
    d: '#46613F'
}, {
    c: '#B07A2E',
    d: '#8A6022'
}, {
    c: '#4A6B8A',
    d: '#344E66'
}, {
    c: '#7B5A8C',
    d: '#5A3F66'
}, {
    c: '#8A5A44',
    d: '#66442F'
}, {
    c: '#5A7B8A',
    d: '#3F5A66'
}, {
    c: '#8A6B3D',
    d: '#66502A'
}];

/** 取名字首字（去除常见称谓前缀，保证首字为姓或名） */
function firstCharOf(name) {
    if (!name) return '用';
    var s = String(name).trim();
    var prefixes = ['张医生', '王护士', '李医生', '刘医生', '陈医生', '赵医生', '孙医生', '周医生', '吴医生', '郑医生', '家属', '家人', '本人'];
    for (var i = 0; i < prefixes.length; i++) {
        if (s.indexOf(prefixes[i]) === 0) {
            s = s.substring(prefixes[i].length);
            break;
        }
    }
    s = s.trim();
    if (!s) return name.trim().charAt(0) || '用';
    return s.charAt(0);
}

/** avatarSVG — 新中式圆形首字头像：宣纸白字 + 朱砂/松绿/赭金等东方配色 */
function avatarSVG(name, gender, size) {
    size = size || 64;
    var ch = firstCharOf(name);
    var hash = 0;
    for (var i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    var idx = Math.abs(hash) % AVATAR_COLORS.length;
    var C = AVATAR_COLORS[idx];
    var svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="' + size + '" height="' + size + '">' +
        '<defs><linearGradient id="ag" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="' + C.c + '"/><stop offset="100%" stop-color="' + C.d + '"/></linearGradient>' +
        '<filter id="as"><feDropShadow dx="0" dy="1" stdDeviation="1" flood-color="#000" flood-opacity=".15"/></filter></defs>' +
        '<rect x="1" y="1" width="62" height="62" rx="6" fill="url(#ag)"/>' +
        '<text x="32" y="34" text-anchor="middle" dominant-baseline="middle" font-family="Ma Shan Zheng, STXingkai, KaiTi, STKaiti, serif" font-size="28" fill="#FDF8EF" filter="url(#as)" letter-spacing="1">' + escapeHtml(ch) + '</text>' +
        '</svg>';
    return _svg(svg);
}

/** avatarSVGForRole — 联系人角色头像同样用首字，保持全站头像语言统一 */
function avatarSVGForRole(role, size) {
    var map = {
        doctor: '张医生',
        nurse: '王护士',
        family: '家属'
    };
    return avatarSVG(map[role] || '用', '未知', size || 64);
}

/* ============================================================
   真字体书法头像 — 马善政(Ma Shan Zheng)毛笔字体，内联 SVG
   说明：头像必须用「内联 SVG」注入 DOM，data-URI 形式的 SVG
   不会应用网页字体，因此此前 font-family 实际未生效。
   ============================================================ */
/** calligraphyAvatar — 返回内联 SVG 字符串（宣纸底 + 朱砂细框 + 舒展首字） */
function calligraphyAvatar(name) {
    var ch = firstCharOf(name) || '王';
    return '<svg viewBox="0 0 64 64" xmlns="http://www.w3.org/2000/svg" preserveAspectRatio="xMidYMid meet">' +
        '<defs><linearGradient id="calPaper" x1="0" y1="0" x2="0" y2="1">' +
        '<stop offset="0%" stop-color="#F8F2E6"/><stop offset="100%" stop-color="#EDE3CF"/></linearGradient></defs>' +
        '<rect x="0" y="0" width="64" height="64" rx="6" fill="url(#calPaper)"/>' +
        '<rect x="3" y="3" width="58" height="58" rx="4" fill="none" stroke="#9E3B2E" stroke-opacity=".30" stroke-width="1"/>' +
        '<text x="32" y="35" text-anchor="middle" dominant-baseline="middle" font-family="\'Ma Shan Zheng\',\'Noto Serif SC\',serif" font-size="46" fill="#211C16" letter-spacing="0">' + escapeHtml(ch) + '</text>' +
        '</svg>';
}

/** 取当前登录用户名（优先 App.loginInfo，回退 localStorage hm-login），保证「无论哪个用户登录」都能动态取到名字 */
function getLoginName() {
    try {
        if (window.App && App.loginInfo && App.loginInfo.name) return App.loginInfo.name;
    } catch (e) {}
    try {
        var raw = localStorage.getItem('hm-login');
        if (raw) {
            var o = JSON.parse(raw);
            if (o && o.name) return o.name;
        }
    } catch (e) {}
    return '';
}

/** 把书法头像注入「我的」页头像框（#avatarWrapMe 优先，兼容 #avatarWrap） */
function renderUserCalligraphyAvatar() {
    var wrap = document.getElementById('avatarWrapMe') || document.getElementById('avatarWrap');
    if (!wrap) return;
    var old = wrap.querySelector('.calligraphy-avatar');
    if (old) old.parentNode.removeChild(old);
    var nm = getLoginName() || '用';
    var box = document.createElement('div');
    box.className = 'calligraphy-avatar';
    box.innerHTML = calligraphyAvatar(nm);
    wrap.appendChild(box);
    wrap.classList.add('has-calligraphy');
    var img = document.getElementById('avatarImgMe') || document.getElementById('avatarImg');
    var fb = document.getElementById('avatarFallbackMe') || document.getElementById('avatarFallback');
    if (img) img.classList.remove('show');
    if (fb) fb.classList.add('hidden');
    /* 字体加载完成后重绘一次，避免首屏 FOUT 回退成系统字体 */
    if (document.fonts && document.fonts.ready) {
        document.fonts.ready.then(function() {
            var w = document.getElementById('avatarWrapMe') || document.getElementById('avatarWrap');
            if (w && w.querySelector('.calligraphy-avatar')) {
                var o = w.querySelector('.calligraphy-avatar');
                if (o) o.parentNode.removeChild(o);
                var b = document.createElement('div');
                b.className = 'calligraphy-avatar';
                b.innerHTML = calligraphyAvatar(nm);
                w.appendChild(b);
            }
        });
    }
}

/** 移除书法头像覆盖层（用户上传照片时调用） */
function removeCalligraphyAvatar() {
    var wrap = document.getElementById('avatarWrapMe') || document.getElementById('avatarWrap');
    if (!wrap) return;
    var old = wrap.querySelector('.calligraphy-avatar');
    if (old) old.parentNode.removeChild(old);
    wrap.classList.remove('has-calligraphy');
}

/* ===== 功能图标库 ===== */
var ICONS = {
    home: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M4 11.6 12 5l8 6.6\"/><path d=\"M6 10.4V19h12v-8.6\"/><path d=\"M10 19v-4.4h4V19\"/></svg>",
    chat: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M20 14.2a2.2 2.2 0 0 1-2.2 2.2H8l-4 3.4V6.2A2.2 2.2 0 0 1 6.2 4h11.6A2.2 2.2 0 0 1 20 6.2z\"/><circle cx=\"9.4\" cy=\"11\" r=\"1.1\" fill=\"#3B2F25\"/><circle cx=\"14.6\" cy=\"11\" r=\"1.1\" fill=\"#3B2F25\"/></svg>",
    health: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#5E7B57\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#5E7B57\" stroke-opacity=\".14\" fill=\"#5E7B57\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"12\" r=\"8.6\" stroke-opacity=\".5\"/><path d=\"M12 7.2v9.6M7.2 12h9.6\" stroke-width=\"2.4\"/></svg>",
    person: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"8\" r=\"3.4\"/><path d=\"M6 20c0-3.6 2.7-6 6-6s6 2.4 6 6\"/></svg>",
    sos: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#9E3B2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#9E3B2E\" stroke-opacity=\".14\" fill=\"#9E3B2E\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"12\" r=\"9.4\"/><path d=\"M12 7v6\" stroke-width=\"2.5\"/><circle cx=\"12\" cy=\"17.2\" r=\"1.3\" fill=\"#9E3B2E\" stroke=\"none\"/></svg>",
    pin: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M12 21c4.6-5.1 7-8.6 7-12a7 7 0 1 0-14 0c0 3.4 2.4 6.9 7 12z\"/><circle cx=\"12\" cy=\"9\" r=\"2.6\"/></svg>",
    steps: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#5E7B57\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#5E7B57\" stroke-opacity=\".14\" fill=\"#5E7B57\" fill-opacity=\".05\"/><path d=\"M8.4 6.2c1.7 0 2.8 1.4 2.8 3.1S10.1 12.4 8.4 12.4 5.6 11 5.6 9.3 6.7 6.2 8.4 6.2z\"/><path d=\"M15.6 12.4c1.7 0 2.8 1.4 2.8 3.1s-1.1 3.1-2.8 3.1-2.8-1.4-2.8-3.1 1.1-3.1 2.8-3.1z\"/></svg>",
    search: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"10.5\" cy=\"10.5\" r=\"6.4\"/><path d=\"M15.4 15.4 21 21\"/></svg>",
    moon: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M20 14.6A8.6 8.6 0 1 1 9.4 4 6.6 6.6 0 0 0 20 14.6z\"/><circle cx=\"16.5\" cy=\"7\" r=\".9\" fill=\"#3B2F25\" stroke=\"none\"/><circle cx=\"18.6\" cy=\"10.5\" r=\".6\" fill=\"#3B2F25\" stroke=\"none\"/></svg>",
    sun: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#B07A2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#B07A2E\" stroke-opacity=\".14\" fill=\"#B07A2E\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"12\" r=\"4.6\"/><path d=\"M12 2.5v3.2M12 18.3v3.2M2.5 12h3.2M18.3 12h3.2M5 5l2.3 2.3M16.7 16.7 19 19M19 5l-2.3 2.3M7.3 16.7 5 19\"/></svg>",
    pill: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M5.5 14.5 14.5 5.5a4.2 4.2 0 0 1 6 6L11.5 20.5a4.2 4.2 0 0 1-6-6z\"/><path d=\"M8.5 11.5l5 5\"/></svg>",
    chart: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M3 17 9 11l4 4 8-8\"/><path d=\"M16 7h4v4\"/></svg>",
    diet: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M4 11h16a8 8 0 0 1-16 0z\"/><path d=\"M9 6.2c0-1 .4-2 0-3.2M13 6.2c0-1 .4-2 0-3.2\"/></svg>",
    clipboard: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><rect x=\"6\" y=\"4\" width=\"12\" height=\"17\" rx=\"2\"/><path d=\"M9 4h6v3.2H9z\"/><path d=\"M9 12h6M9 16h4\"/></svg>",
    check: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#5E7B57\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#5E7B57\" stroke-opacity=\".14\" fill=\"#5E7B57\" fill-opacity=\".05\"/><path d=\"M5 12.4 10 17.4 19 7\" stroke-width=\"2.5\"/></svg>",
    clock: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"12\" r=\"9\"/><path d=\"M12 7.2v5l3.4 2\"/></svg>",
    settings: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"12\" cy=\"12\" r=\"3.1\"/><path d=\"M19.4 13a7.6 7.6 0 0 0 0-2l1.7-1.3-1.9-3.3-2 .8a7.5 7.5 0 0 0-1.7-1l-.3-2.1h-3.9l-.3 2.1a7.5 7.5 0 0 0-1.7 1l-2-.8L3.9 9.7 5.6 11a7.6 7.6 0 0 0 0 2l-1.7 1.3 1.9 3.3 2-.8a7.5 7.5 0 0 0 1.7 1l.3 2.1h3.9l.3-2.1a7.5 7.5 0 0 0 1.7-1l2 .8 1.9-3.3z\"/></svg>",
    users: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"9\" cy=\"8\" r=\"3\"/><path d=\"M3.5 20c0-3 2.5-5 5.5-5s5.5 2 5.5 5\"/><path d=\"M16 6.4a3 3 0 0 1 0 6.2\"/><path d=\"M17 14.8c2.6.4 4.2 2.1 4.2 4.6\"/></svg>",
    calendar: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><rect x=\"4\" y=\"5\" width=\"16\" height=\"15\" rx=\"2\"/><path d=\"M4 9.2h16M8 3v4M16 3v4\"/><circle cx=\"12\" cy=\"15\" r=\"1.5\" fill=\"#9E3B2E\" stroke=\"none\"/></svg>",
    edit: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M4 20l1-4L16.5 4.5l3.5 3.5L8 19z\"/><path d=\"M14 7l3 3\"/></svg>",
    ai: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#B07A2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#B07A2E\" stroke-opacity=\".14\" fill=\"#B07A2E\" fill-opacity=\".05\"/><rect x=\"5\" y=\"5\" width=\"14\" height=\"14\" rx=\"3\"/><path d=\"M12 8.6l1.7 3.6L17.4 14l-3.7 1.7L12 19.4l-1.7-3.7L6.6 14l3.7-1.8z\"/></svg>",
    refresh: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M20 12a8 8 0 1 1-2.4-5.7\"/><path d=\"M20 4v4h-4\"/></svg>",
    warn: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#9E3B2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#9E3B2E\" stroke-opacity=\".14\" fill=\"#9E3B2E\" fill-opacity=\".05\"/><path d=\"M12 3.6 21 19H3z\"/><path d=\"M12 9v4.6\" stroke-width=\"2.3\"/><circle cx=\"12\" cy=\"16.6\" r=\"1.3\" fill=\"#9E3B2E\" stroke=\"none\"/></svg>",
    map: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M3 6 9 3l6 3 6-3v15l-6 3-6-3-6 3z\"/><path d=\"M9 3v15M15 6v15\"/></svg>",
    export: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M5 19h14v-9\"/><path d=\"M12 14V4M8 8l4-4 4 4\"/></svg>",
    hospital: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M4 21V9l8-5 8 5v12\"/><path d=\"M9 21v-6h6v6\"/><path d=\"M12 8v4M10 10h4\"/></svg>",
    walk: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><circle cx=\"13\" cy=\"5\" r=\"1.9\"/><path d=\"M11 21l1.6-7L9.6 13l-2.1 3\"/><path d=\"M12.6 14 16 8l4 2\"/></svg>",
    bell: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M18 9a6 6 0 0 0-12 0c0 6-2.5 8-2.5 8h17S18 15 18 9z\"/><path d=\"M10.4 20a1.8 1.8 0 0 0 3.2 0\"/></svg>",
    mail: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><rect x=\"3.5\" y=\"6\" width=\"17\" height=\"12\" rx=\"2\"/><path d=\"M4 7.2l8 6 8-6\"/></svg>",
    close: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M6 6l12 12M18 6 6 18\"/></svg>",
    trash: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M4 7h16\"/><path d=\"M9 7V4h6v3\"/><path d=\"M6 7l1 13h10l1-13\"/><path d=\"M10 11v6M14 11v6\"/></svg>",
    quote: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M7 8c-2 1-3 3-3 5.5C4 17 6 18 8 17c0-2-1-3-1-5 .5-2 2-4 0-4zM16 8c-2 1-3 3-3 5.5 0 3.5 2 4.5 4 3.5 0-2-1-3-1-5 .5-2 2-4 0-4z\"/></svg>",
    heart: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#9E3B2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#9E3B2E\" stroke-opacity=\".14\" fill=\"#9E3B2E\" fill-opacity=\".05\"/><path d=\"M12 20.4C5 15.4 3 12 3 8.8 3 6 5 4 7.6 4c1.7 0 3.3.9 4.4 2.4C13 4.9 14.7 4 16.4 4 19 4 21 6 21 8.8c0 3.2-2 6.6-9 11.6z\" fill=\"#9E3B2E\" fill-opacity=\".14\" stroke-width=\"1.8\"/></svg>",
    oxygen: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#5E7B57\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#5E7B57\" stroke-opacity=\".14\" fill=\"#5E7B57\" fill-opacity=\".05\"/><path d=\"M12 3.4C8 9 6 11.5 6 14.5a6 6 0 0 0 12 0c0-3-2-5.5-6-11z\"/><path d=\"M9.4 14.6c1 1 4 1 5.2 0\"/></svg>",
    temp: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#B07A2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#B07A2E\" stroke-opacity=\".14\" fill=\"#B07A2E\" fill-opacity=\".05\"/><path d=\"M14 14V5a2 2 0 0 0-4 0v9a4 4 0 1 0 4 0z\"/><circle cx=\"12\" cy=\"17.6\" r=\"1.7\" fill=\"#B07A2E\" stroke=\"none\"/></svg>",
    bp: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#9E3B2E\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#9E3B2E\" stroke-opacity=\".14\" fill=\"#9E3B2E\" fill-opacity=\".05\"/><rect x=\"3\" y=\"5\" width=\"18\" height=\"14\" rx=\"2\"/><path d=\"M5 13h3l2-4 2 7 2-5h5\"/></svg>",
    phone: "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"#3B2F25\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><circle cx=\"12\" cy=\"12\" r=\"10.6\" stroke=\"#3B2F25\" stroke-opacity=\".14\" fill=\"#3B2F25\" fill-opacity=\".05\"/><path d=\"M6.5 4H9l1.6 5-2 1.4a11 11 0 0 0 5 5L16 18l3 1.4V21a2 2 0 0 1-2 2A16 16 0 0 1 4.5 6 2 2 0 0 1 6.5 4z\"/></svg>",
};


/** iconSVG — 获取功能图标 SVG data URI */
function iconSVG(name, size, color) {
    size = size || 24;
    var svg = ICONS[name];
    if (!svg) return '';
    if (color) svg = svg.replace(/currentColor/g, color);
    return _svg(svg.replace('<svg ', '<svg width="' + size + '" height="' + size + '" '));
}

/* 药物服用频率翻译：qd/bid/tid/qn/qw/prn 等缩写 → 老人能懂的说法（个人端 & 聊天引擎共用） */
function medsPlain(medications) {
    if (!medications) return '';
    return String(medications)
        .replace(/qd|q\.d\.|每日一次/gi, '每天一次')
        .replace(/bid|b\.i\.d\.|每日两次/gi, '每天两次')
        .replace(/tid|t\.i\.d\.|每日三次/gi, '每天三次')
        .replace(/qn|q\.n\.|每晚一次|睡前/gi, '睡前')
        .replace(/qw|q\.w\.|每周一次/gi, '每周一次')
        .replace(/qod|q\.o\.d\.|隔日一次/gi, '隔天一次')
        .replace(/prn|p\.r\.n\.|按需/gi, '按需');
}

/* 饮食建议生成（个人端 & 聊天引擎共用） */
function genDietAdvice(p) {
    var L = [];
    if (!p || typeof p.bloodSugar !== 'number' || typeof p.systolic !== 'number') return L;
    if (p.bloodSugar >= 7.0) L.push('今天血糖' + p.bloodSugar.toFixed(1) + '，偏高了点。主食换成燕麦荞麦这些粗粮，每顿一小碗就够。多吃绿叶菜和苦瓜，甜点和含糖饮料就先别碰啦。');
    else if (p.bloodSugar >= 6.2) L.push('血糖稍微高了一点点，晚上主食少吃两口，多吃点菜。水果选苹果、柚子这些不太甜的。');

    var dia = typeof p.diastolic === 'number' ? p.diastolic : 0;
    if (p.systolic >= 150) L.push('血压今天' + p.systolic.toFixed(0) + '/' + dia.toFixed(0) + '，有点高了。盐一定要少吃，一天别超过小半啤酒瓶盖的量。多吃香蕉番茄补补钾，咸菜腊肉那些忍住别吃。');
    else if (p.systolic >= 140) L.push('血压比平时高一点，吃清淡些，酱油味精少放就好。');

    if (typeof p.bloodOxygen === 'number' && p.bloodOxygen <= 93) L.push('血氧' + p.bloodOxygen.toFixed(1) + '%，偏低。多吃点瘦牛肉、黑木耳、菠菜这些补铁的食物，再补点维C帮着吸收。');

    // 缓存 chronic 避免重复属性访问（自由文本格式，需 indexOf 子串匹配）
    var ch = p.chronic || '';
    if (ch.indexOf('糖尿病') >= 0) L.push('糖尿病饮食记住三点：定时定量吃，一天三顿加两顿小的；蛋白质多吃鱼虾豆腐；吃饭先吃菜后吃主食。');
    if (ch.indexOf('高血压') >= 0) L.push('高血压适合吃全谷物和低脂奶，每天一小把坚果，红肉少吃点。');
    if (ch.indexOf('脑梗') >= 0) L.push('脑梗康复期饮食：用橄榄油炒菜，每周吃两三次深海鱼，每天几颗核桃或杏仁。');
    if (ch.indexOf('支气管') >= 0 || ch.indexOf('肺') >= 0) L.push('肺不好要多补充蛋白质，鱼蛋豆多吃，蔬菜水果抗氧化，一顿别吃太饱，少食多餐。');

    if (L.length < 2) L.push('今天指标都挺平稳的，继续保持。水喝够三瓶矿泉水的量，蛋白质吃够，蔬菜水果别少，一天一斤菜半斤水果。');
    return L;
}

/* ===== 持久化 Key 集中管理（避免魔法字符串散落各处） =====
 * 持久化边界（刻意二选一，消除"两套并存"的混乱）：
 *  - localStorage（KEYS 下全部 key）：小体积、需同步读写的配置/记录 JSON
 *    （主题、档案、设置、记录、病历、聊天历史等）。
 *  - IndexedDB（storage.js 的 DataStore / AvatarStore）：仅用于体积较大的
 *    生成式数据——头像 dataURL、预警历史日志。
 *  旧注释里的"待迁移到 IndexedDB"已废弃：上述边界是有意为之，并非未完成的迁移。
 */
var KEYS = {
    /* 个人端 */
    THEME: 'hm-p-theme',
    PROFILE: 'hm-p-profile',
    MEDTIMES: 'hm-p-medtimes',
    EMERGENCY: 'hm-p-emergency',
    APPOINTMENTS: 'hm-p-appointments',
    CHECKIN: 'hm-p-checkin',
    TRENDS_PREFIX: 'hm-trends-',
    /* 聊天记录 — localStorage 同步持久化 */
    CHAT_V2: 'hm-chat-v2',
    /* 用户偏好 — 持久化存储 */
    AI_CONFIG: 'hm-ai-config',
    /* 管理端 */
    SETTINGS: 'hm-settings',
    ADMISSIONS: 'hm-admissions',
    PROFILE_ADMIN: 'hm-profile',
    MEDICAL: 'hm-medical'
};