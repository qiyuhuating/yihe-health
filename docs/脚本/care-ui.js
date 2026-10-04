window.CareUI = (() => {
  const h = escapeHtml;
  const time = (value) =>
    value && Number.isFinite(new Date(value).getTime())
      ? new Date(value).toLocaleString('zh-CN', {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
        })
      : '未记录';
  const staff = (id) => CareAPI.STAFF.find((s) => s.id === id)?.name || '未分配';
  const mount = (id, markup) => {
    document.getElementById(id).innerHTML = markup;
  };
  /* patch(): in-place update for lists, cards and counters. Same final DOM as mount(), but nodes whose markup did
     not change are kept (focus, scroll, <details> state and event bindings survive), changed nodes are updated
     where they are, and cards are matched by key so inserting one does not rebuild the others.
     Use mount() for dialog bodies and forms, which are meant to be rebuilt from scratch. */
  const KEYED = '[data-key],[data-event],[data-resident],[data-quick-claim]';
  const keyOf = (node) => {
    if (node.nodeType !== 1) return null;
    const own = node.getAttribute('data-key');
    if (own) return own;
    const hit = node.matches(KEYED) ? node : node.querySelector(KEYED);
    return hit ? hit.getAttribute('data-key') || hit.getAttribute('data-event') || hit.getAttribute('data-resident') || hit.getAttribute('data-quick-claim') : null;
  };
  const focusHint = (node) => {
    for (const attr of ['data-quick-claim', 'data-event', 'data-resident', 'data-filter', 'data-page', 'id'])
      if (node.hasAttribute?.(attr)) return '[' + attr + '="' + CSS.escape(node.getAttribute(attr)) + '"]';
    return null;
  };
  function syncAttributes(from, to) {
    for (const { name, value } of [...to.attributes]) if (from.getAttribute(name) !== value) from.setAttribute(name, value);
    for (const { name } of [...from.attributes]) {
      if (to.hasAttribute(name)) continue;
      if (name === 'open' && from.tagName === 'DETAILS') continue; // a panel the user opened stays open
      from.removeAttribute(name);
    }
  }
  function morph(from, to) {
    if (from.nodeType !== to.nodeType || from.nodeName !== to.nodeName) return false;
    if (from.nodeType === 3) {
      if (from.nodeValue !== to.nodeValue) from.nodeValue = to.nodeValue;
      return true;
    }
    if (from.nodeType !== 1) return from.isEqualNode(to);
    if (from.isEqualNode(to)) return true;
    syncAttributes(from, to);
    reconcile(from, [...to.childNodes]);
    return true;
  }
  function reconcile(parent, wanted) {
    const current = [...parent.childNodes],
      byKey = new Map();
    for (const node of current) {
      const key = keyOf(node);
      if (key !== null && !byKey.has(key)) byKey.set(key, node);
    }
    const used = new Set(),
      result = [];
    wanted.forEach((next, index) => {
      const key = keyOf(next);
      let prev = key !== null ? byKey.get(key) : current[index];
      if (prev && (used.has(prev) || (key === null && keyOf(prev) !== null))) prev = null;
      if (prev && morph(prev, next)) {
        used.add(prev);
        result.push(prev);
      } else result.push(next);
    });
    for (const node of current) if (!used.has(node)) node.remove();
    let cursor = parent.firstChild;
    for (const node of result) {
      if (node === cursor) cursor = cursor.nextSibling;
      else parent.insertBefore(node, cursor);
    }
  }
  const patch = (id, markup) => {
    const root = document.getElementById(id),
      active = document.activeElement,
      hint = active && root.contains(active) && active !== root ? focusHint(active) : null;
    const scratch = document.createElement('template');
    scratch.innerHTML = markup;
    reconcile(root, [...scratch.content.childNodes]);
    if (hint && !root.contains(document.activeElement)) root.querySelector(hint)?.focus({ preventScroll: true });
  };
  const empty = (text) => '<p class="empty">' + h(text) + '</p>';
  const device = (p) =>
    CareAPI.mode === 'http'
      ? {
          online: '设备在线',
          offline: '离线 / 数据可能过期',
          unknown: '设备状态未知',
        }[p.deviceState] || '设备状态未知'
      : CareAPI.now() - Date.parse(p.lastSeen) > 300000
        ? '离线 / 数据可能过期'
        : '模拟设备在线';
  const metric = (value) => (Number.isFinite(value) ? String(value) : '—');
  function recovery(retry) {
    const { error, button, loadingNotice, inline, notices } = createRecoveryElements();
    let blocked = false,
      loadingActive = false;
    function setInert(value) {
      document
        .querySelectorAll('.staff-care .view,#eventBody,#residentBody,#residentProfileForm,#contactForm,#todayDoses')
        .forEach((n) => (n.inert = value));
    }
    function block(value, closeDialogs = true) {
      blocked = value;
      document.body.toggleAttribute('data-care-unavailable', value);
      setInert(value);
      if (value) {
        if (closeDialogs) document.querySelectorAll('dialog[open]').forEach((n) => n.close());
        const title = document.getElementById('statusTitle');
        if (title) title.textContent = '状态暂不可用，请重新读取';
      }
    }
    function show(e) {
      loadingActive = false;
      loadingNotice.hidden = true;
      error.setAttribute('role', 'alert');
      error.textContent = e.message || '读取失败，请重试';
      error.hidden = false;
      button.hidden = false;
      inline.forEach((node) => {
        node.hidden = false;
      });
      notices.forEach((node) => {
        node.textContent = e.message || '读取失败，请重试';
        node.hidden = false;
      });
      if (['DATA_INVALID', 'STORAGE_UNAVAILABLE', 'ENV_UNSUPPORTED'].includes(e.code)) block(true);
      if (
        [
          'INVALID_RESPONSE',
          'NETWORK_ERROR',
          'REQUEST_TIMEOUT',
          'SERVICE_UNAVAILABLE',
          'UNAUTHENTICATED',
          'FORBIDDEN',
          'SESSION_REQUIRED',
        ].includes(e.code)
      )
        block(true, false);
    }
    function clear() {
      loadingActive = false;
      loadingNotice.hidden = true;
      block(false);
      error.setAttribute('role', 'alert');
      error.hidden = true;
      button.hidden = true;
      inline.forEach((node) => {
        node.hidden = true;
      });
      notices.forEach((node) => {
        node.hidden = true;
      });
    }
    button.onclick = async () => {
      button.disabled = true;
      try {
        await retry();
        clear();
      } catch (e) {
        show(e);
      } finally {
        button.disabled = false;
      }
    };
    inline.forEach((node) => {
      node.onclick = async () => {
        node.disabled = true;
        try {
          await button.onclick();
        } finally {
          node.disabled = false;
        }
      };
    });
    return {
      show,
      clear,
      loading: () => {
        blocked = false;
        document.body.removeAttribute('data-care-unavailable');
        setInert(true);
        loadingActive = true;
        loadingNotice.hidden = false;
        error.hidden = true;
        button.hidden = true;
        const title = document.getElementById('statusTitle');
        if (title) title.textContent = '正在读取今日状态';
      },
      restored: () => {
        if (blocked || loadingActive) clear();
      },
    };
  }
  function trend(p) {
    if (p.trend.length < 2) return empty('采样不足两次，暂不能判断趋势');
    return (
      '<div class="care-table-wrap"><table><caption>' +
      (CareAPI.mode === 'http' ? '最近采样' : '最近模拟采样') +
      '</caption><thead><tr><th>时间</th><th>心率 bpm</th><th>血氧 %</th><th>血压 mmHg</th></tr></thead><tbody>' +
      p.trend
        .slice(-6)
        .map(
          (t) =>
            `<tr><td>${h(time(t.at))}</td><td>${metric(t.heartRate)}</td><td>${metric(t.bloodOxygen)}</td><td>` +
            `${metric(t.systolic)}/${metric(t.diastolic)}</td></tr>`,
        )
        .join('') +
      '</tbody></table></div>'
    );
  }
  function direction(p) {
    if (p.trend.length < 2) return '趋势：采样不足';
    const [a, b] = p.trend.slice(-2),
      delta = b.bloodOxygen - a.bloodOxygen;
    if (!Number.isFinite(a.bloodOxygen) || !Number.isFinite(b.bloodOxygen)) return '趋势：数据不足，无法比较';
    return (
      '血氧较上次 ' +
      (delta > 0 ? '↑ ' + delta : delta < 0 ? '↓ ' + Math.abs(delta) : '持平') +
      ' · ' +
      time(a.at) +
      ' → ' +
      time(b.at)
    );
  }
  function contacts(p, masked = false) {
    if (!p.contacts.length) return empty('尚未登记紧急联系人，请到“我的”或联系工作人员补充');
    return p.contacts
      .map(
        (c) =>
          `<div class="contact-row"><span>${h(displayName(c.name, masked))}</span>` + `${renderContactAction(c)}</div>`,
      )
      .join('');
  }
  function vitals(p) {
    const stale =
      CareAPI.mode === 'http' ? p.deviceState === 'offline' : CareAPI.now() - Date.parse(p.lastSeen) > 300000;
    const statusOf = (key) =>
      CareAPI.mode === 'http'
        ? Number.isFinite(p[key])
          ? p.metricStates?.[key] || 'unknown'
          : 'unknown'
        : Metrics.metricStatus(key, p[key]);
    return [
      ['heartRate', '心率', metric(p.heartRate), 'bpm'],
      ['bloodOxygen', '血氧', metric(p.bloodOxygen), '%'],
      ['temperature', '体温', metric(p.temperature), '°C'],
      ['systolic', '血压', metric(p.systolic) + '/' + metric(p.diastolic), 'mmHg'],
    ]
      .map(([key, label, value, unit]) => {
        const levels = key === 'systolic' ? [statusOf(key), statusOf('diastolic')] : [statusOf(key)];
        const status = levels.includes('danger')
          ? 'danger'
          : stale
            ? 'stale'
            : levels.includes('unknown')
              ? 'unknown'
              : levels.includes('warning')
                ? 'warning'
                : 'normal';
        const text = {
          stale: '离线前采样',
          unknown: '数据不足，请核实采样',
          danger: '需及时核实',
          warning: '需关注',
          normal: CareAPI.mode === 'http' ? '参考范围内' : '演示范围内',
        }[status];
        return (
          `<div class="vital-card" data-status="${status}"><div class="vital-label">${h(label)} <span class="vi` +
          `tal-unit">${h(unit)}</span></div><div class="vital-number">${h(value)}</div><span class="vital-statu` +
          `s">${text}${stale && status === 'danger' ? ' · 离线前危险采样，当前状态未知' : ''}</span></div>`
        );
      })
      .join('');
  }
  function record(p) {
    return [
      ['慢病', p.chronic],
      ['既往病史', p.medicalHistory?.pastHistory],
      ['过敏史', p.medicalHistory?.allergies],
      ['档案用药', p.medicalHistory?.medications],
    ]
      .map(
        ([label, value]) =>
          `<div class="rec-row"><strong>${h(label)}</strong><span>${h(value || '未记录')}</span></div>`,
      )
      .join('');
  }
  function eventCard(e, s, masked = false) {
    const p = s.residents[e.residentId];
    const open = CareAPI.isOpen(e),
      minutes = Math.ceil((Date.parse(e.dueAt) - CareAPI.now()) / 60000);
    const deadline = open
      ? minutes <= 0
        ? '已超时 ' + Math.abs(minutes) + ' 分钟'
        : '剩余 ' + minutes + ' 分钟'
      : '已归档';
    const urgency = !open ? 'closed' : minutes <= 0 ? 'over' : minutes <= 5 ? 'soon' : 'ok';
    const who = displayName(p.name, masked) + '，' + CareAPI.TYPES[e.type];
    const quick =
      open && e.state === 'new'
        ? `<button class="btn btn-primary" data-quick-claim="${h(e.id)}" aria-label="接单：${h(who)}">接单</button>`
        : '';
    return (
      `<article class="care-event" data-state="${h(e.state)}" data-severity="${h(e.severity)}" data-urgency=` +
      `"${urgency}"><div class="event-identity"><div class="event-tags"><span class="severity-tag">` +
      `${e.severity === 'danger' ? '紧急' : '关注'}</span><span class="badge">${h(CareAPI.STATES[e.state])}</span></div><h` +
      `3>${h(displayName(p.name, masked))}</h3><span class="event-type">${h(CareAPI.TYPES[e.type])}</span></div><d` +
      `iv class="event-detail"><p>${h(e.detail)}</p><div class="event-meta">发现 ${h(time(e.createdAt))}</div><div clas` +
      `s="signal-note">${e.sourceRecoveredAt ? '信号恢复 · 仍需人工复核' : '待核实的异常信号'}</div></div><div` +
      ` class="event-owner"><span class="event-meta">当前负责人</span><strong>${h(staff(e.owner))}</strong><span ` +
      `class="event-deadline ${open && minutes <= 0 ? 'is-overdue' : ''}" title="处理时限 ${h(time(e.dueAt))}">` +
      `${h(deadline)}</span><span class="event-meta">责任人 ${h(staff(p.responsible))}</span></div><div` +
      ` class="event-action">${quick}<button class="btn" data-event="${h(e.id)}" aria-label="` +
      `${h(displayName(p.name, masked))}，${h(CareAPI.TYPES[e.type])}，${open ? '进入处置' : '查看留痕'}">` +
      `${open ? '进入处置' : '查看留痕'} <span aria-hidden="true">→</span></button></div></article>`
    );
  }
  function poll(task, onError, { background = false } = {}) {
    let timer = null,
      running = false,
      stopped = false,
      failures = 0;
    const delay = CareAPI.mode === 'http' ? YIHE_RUNTIME_CONFIG.pollIntervalMs : 5000;
    // Alarm consoles keep polling in a background tab (slower) so a new alert is not missed.
    const schedule = () => {
      clearTimeout(timer);
      if (!stopped)
        timer = setTimeout(
          tick,
          Math.min(120000, Math.max(background && document.hidden ? 30000 : 0, delay * 2 ** failures)),
        );
    };
    async function tick() {
      if (running || stopped) return;
      if ((document.hidden && !background) || navigator.onLine === false) {
        schedule();
        return;
      }
      running = true;
      try {
        await task();
        failures = 0;
      } catch (e) {
        failures = Math.min(4, failures + 1);
        onError(e);
      } finally {
        running = false;
        schedule();
      }
    }
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) tick();
    });
    window.addEventListener('online', tick);
    window.addEventListener('offline', () => {
      if (CareAPI.mode === 'http')
        onError(
          Object.assign(Error('网络已断开，恢复连接后会重新读取。'), {
            code: 'NETWORK_ERROR',
          }),
        );
    });
    window.addEventListener('pagehide', () => {
      stopped = true;
      clearTimeout(timer);
    });
    window.addEventListener('pageshow', (e) => {
      if (e.persisted) {
        stopped = false;
        tick();
      }
    });
    schedule();
  }
  let titleBase = null;
  function badge(count) {
    if (titleBase === null) titleBase = document.title.replace(/^\(\d+\)\s*/, '');
    if (document.title.startsWith('【')) return; // the lock screen owns the title while shielded
    document.title = count > 0 ? '(' + count + ') ' + titleBase : titleBase;
  }
  function createRecoveryElements() {
    const error = document.getElementById('careError'),
      button = document.createElement('button');
    const loadingNotice = document.createElement('p');
    loadingNotice.hidden = true;
    loadingNotice.className = 'care-error care-loading';
    loadingNotice.setAttribute('role', 'status');
    loadingNotice.setAttribute('aria-live', 'polite');
    loadingNotice.textContent = '正在读取最新记录…';
    button.id = 'retryCare';
    button.type = 'button';
    button.className = 'btn';
    button.textContent = '重新读取';
    button.hidden = true;
    error.after(loadingNotice, button);
    const inline = [...document.querySelectorAll('[data-care-retry]')];
    const notices = inline.map((node) => {
      const notice = document.createElement('p');
      notice.hidden = true;
      notice.className = 'care-error';
      notice.setAttribute('role', 'alert');
      node.after(notice);
      return notice;
    });
    return {
      error,
      button,
      loadingNotice,
      inline,
      notices,
    };
  }
  function renderContactAction(c) {
    return /^\+?[\d -]{5,22}$/.test(c.phone)
      ? `<a class="btn" href="tel:${h(c.phone.replace(/[ -]/g, ''))}">拨打 ${h(c.phone)}</a>`
      : '<span>号码需要核对</span>';
  }
  return {
    h,
    time,
    staff,
    mount,
    patch,
    empty,
    device,
    metric,
    recovery,
    trend,
    direction,
    contacts,
    vitals,
    record,
    eventCard,
    poll,
    badge,
  };
})();
