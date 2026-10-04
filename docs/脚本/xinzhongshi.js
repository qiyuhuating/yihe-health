/* 居民端"时令"提示：农历日期 + 当前节气时节 + 一句问候。
   纯展示、无网络请求；节气以每年大致日期（±1 天）划分，因此只说"某某时节"，不标具体节气当天。 */
(() => {
  'use strict';

  const TERMS = [
    [1, 5, '小寒'],
    [1, 20, '大寒'],
    [2, 4, '立春'],
    [2, 19, '雨水'],
    [3, 5, '惊蛰'],
    [3, 20, '春分'],
    [4, 5, '清明'],
    [4, 20, '谷雨'],
    [5, 5, '立夏'],
    [5, 21, '小满'],
    [6, 5, '芒种'],
    [6, 21, '夏至'],
    [7, 7, '小暑'],
    [7, 22, '大暑'],
    [8, 7, '立秋'],
    [8, 23, '处暑'],
    [9, 7, '白露'],
    [9, 23, '秋分'],
    [10, 8, '寒露'],
    [10, 23, '霜降'],
    [11, 7, '立冬'],
    [11, 22, '小雪'],
    [12, 7, '大雪'],
    [12, 22, '冬至'],
  ];
  const GREET = {
    春: '春和景明，出门走走，脚步放慢些。',
    夏: '暑热当令，多喝温水，正午少出门。',
    秋: '秋高气爽，早晚微凉，及时添件衣裳。',
    冬: '冬日宜暖，起身慢一点，屋里常通风。',
  };
  function term(date) {
    const key = (date.getMonth() + 1) * 100 + date.getDate();
    let current = TERMS[TERMS.length - 1];
    for (const item of TERMS) if (item[0] * 100 + item[1] <= key) current = item;
    return current[2];
  }
  function season(date) {
    const key = (date.getMonth() + 1) * 100 + date.getDate();
    return key >= 204 && key < 505 ? '春' : key >= 505 && key < 807 ? '夏' : key >= 807 && key < 1107 ? '秋' : '冬';
  }
  const DAYS = [
    '初一',
    '初二',
    '初三',
    '初四',
    '初五',
    '初六',
    '初七',
    '初八',
    '初九',
    '初十',
    '十一',
    '十二',
    '十三',
    '十四',
    '十五',
    '十六',
    '十七',
    '十八',
    '十九',
    '二十',
    '廿一',
    '廿二',
    '廿三',
    '廿四',
    '廿五',
    '廿六',
    '廿七',
    '廿八',
    '廿九',
    '三十',
  ];
  function lunar(date) {
    try {
      const parts = new Intl.DateTimeFormat('zh-CN-u-ca-chinese', {
        month: 'long',
        day: 'numeric',
      }).formatToParts(date);
      const month = parts.find((part) => part.type === 'month')?.value,
        day = Number(parts.find((part) => part.type === 'day')?.value);
      return month && DAYS[day - 1] ? month.replace(/\s/g, '') + DAYS[day - 1] : '';
    } catch (_) {
      return '';
    }
  }
  function render() {
    const node = document.getElementById('xzSeason');
    if (!node) return;
    const now = new Date(),
      parts = [];
    const l = lunar(now);
    node.textContent = '';
    if (l) parts.push('农历' + l);
    const head = document.createElement('b');
    head.textContent = term(now) + '时节';
    node.append(parts.length ? parts.join(' · ') + ' · ' : '', head, '　' + GREET[season(now)]);
  }
  render();
  setInterval(render, 30 * 60 * 1000);
})();

/* ---------- 第三轮：四档字号 / 密度、偏好区、一键拨打家人、显示密码 ---------- */
(() => {
  'use strict';

  const KEY = 'yihe-text-scale',
    // [标签, 倍率, 说明]：紧凑面向年轻用户（一屏更多信息），大 / 特大面向长者。
    PRESETS = [
      ['紧凑', 0.9, '更小的字号与间距，一屏看到更多'],
      ['标准', 1, '默认字号'],
      ['大', 1.15, '更大更舒展'],
      ['特大', 1.3, '最大字号，适合远距离阅读'],
    ];
  const root = document.documentElement;
  const el = (tag, props = {}, ...kids) => {
    const node = document.createElement(tag);
    Object.assign(node, props);
    node.append(...kids);
    return node;
  };
  const nearest = (value) =>
    PRESETS.reduce((best, step) => (Math.abs(step[1] - value) < Math.abs(best[1] - value) ? step : best))[1];
  const densityOf = (value) => (value <= 0.95 ? 'compact' : value >= 1.15 ? 'roomy' : 'normal');
  const stored = () => {
    try {
      return nearest(parseFloat(localStorage.getItem(KEY)) || 1);
    } catch (_) {
      return 1;
    }
  };
  function applyScale(value) {
    root.style.setProperty('--text-scale', String(value));
    root.dataset.textScale = String(value);
    root.dataset.density = densityOf(value);
    document.querySelectorAll('.xz-fontctl button').forEach((button) => {
      button.setAttribute('aria-pressed', String(Number(button.dataset.scale) === value));
    });
  }
  function fontControl() {
    const group = el('div', { className: 'xz-fontctl', role: 'group' });
    group.setAttribute('aria-label', '字号');
    for (const [label, value, hint] of PRESETS) {
      const button = el('button', { type: 'button', textContent: label, title: hint });
      button.dataset.scale = String(value);
      button.setAttribute('aria-pressed', 'false');
      button.addEventListener('click', () => {
        applyScale(value);
        try {
          localStorage.setItem(KEY, String(value));
        } catch (_) {}
      });
      group.append(button);
    }
    return group;
  }
  const label = () => el('span', { className: 'xz-tools-label', textContent: '字号' });

  function mountResident() {
    const topbar = document.querySelector('.home-topbar');
    if (!topbar || topbar.querySelector('.xz-prefs')) return;
    const prefs = el('div', { className: 'xz-prefs' }, label(), fontControl());
    const theme = document.getElementById('themeToggleMe');
    if (theme) prefs.append(theme);
    topbar.append(prefs);
  }
  function mountStaff() {
    const bar = document.querySelector('.topbar');
    if (bar && !bar.querySelector('.xz-fontctl')) bar.querySelector('.staff-selector')?.before(fontControl());
    const panel = document.getElementById('settingDarkMode')?.closest('.panel');
    if (panel && !panel.querySelector('.xz-tools'))
      panel.querySelector('h3')?.after(el('div', { className: 'xz-tools' }, label(), fontControl()));
  }
  function initFamilyCall() {
    const jump = document.querySelector('.resident-contact-jump'),
      box = document.getElementById('homeContacts');
    if (!jump || !box) return;
    const update = () => {
      const link = box.querySelector('a[href^="tel:"]');
      if (!link) {
        jump.classList.remove('xz-call-family');
        jump.setAttribute('href', '#emergencyContacts');
        jump.textContent = '联系家人 / 紧急联系';
        jump.setAttribute('aria-label', '联系家人 / 紧急联系');
        return;
      }
      const row = link.closest('.contact-row') || link.parentElement;
      const who =
        (row?.firstElementChild && row.firstElementChild !== link ? row.firstElementChild.textContent : '').trim() ||
        '家人';
      const text = '打电话给 ' + who;
      jump.classList.add('xz-call-family');
      if (jump.getAttribute('href') !== link.getAttribute('href')) jump.setAttribute('href', link.getAttribute('href'));
      if (jump.textContent !== text) {
        jump.textContent = text;
        jump.setAttribute('aria-label', text + '，由手机发起拨号');
      }
    };
    new MutationObserver(update).observe(box, {
      childList: true,
      subtree: true,
    });
    update();
  }
  function addPasswordToggle(input) {
    if (!input || input.dataset.xzEye) return;
    input.dataset.xzEye = '1';
    const wrap = el('span', {
      className: 'xz-field',
    });
    input.before(wrap);
    wrap.append(input);
    const button = el('button', {
      type: 'button',
      className: 'xz-eye',
      textContent: '显示密码',
    });
    button.setAttribute('aria-pressed', 'false');
    button.setAttribute('aria-controls', input.id);
    button.addEventListener('click', () => {
      const show = input.type === 'password';
      input.type = show ? 'text' : 'password';
      button.textContent = show ? '隐藏密码' : '显示密码';
      button.setAttribute('aria-pressed', String(show));
      input.focus();
    });
    wrap.append(button);
  }
  const start = () => {
    applyScale(stored());
    if (document.body.classList.contains('resident-care')) {
      mountResident();
      initFamilyCall();
    }
    if (document.body.classList.contains('staff-care')) mountStaff();
    applyScale(stored());
    addPasswordToggle(document.getElementById('loginPassword'));
    addPasswordToggle(document.getElementById('adminLoginPassword'));
  };
  window.addEventListener('storage', (event) => {
    if (event.key === KEY) applyScale(stored());
  });
  applyScale(stored());
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
  else start();
})();
