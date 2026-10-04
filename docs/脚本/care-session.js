/* Server session stays in memory; cookies are managed by the browser. */
window.CareSession = (() => {
  'use strict';

  let current = null,
    checking = null,
    idleTimer = null,
    sessionGeneration = 0,
    shielded = false,
    localLast = Date.now(),
    lastWrite = 0;
  const IDLE_MS = 15 * 60000,
    ACTIVITY_KEY = 'yihe-staff-activity',
    SHIELD_KEY = 'yihe-staff-shield';
  /* Idle handling is a privacy shield, not a logout: the server session and polling stay alive so alarms
     still arrive; content is hidden until the password is entered again. Activity is shared across tabs
     through localStorage and measured with the wall clock so system sleep counts as idle time. */
  function storedActivity() {
    try {
      const value = Number(localStorage.getItem(ACTIVITY_KEY));
      return Number.isFinite(value) && value > 0 ? value : 0;
    } catch (_) {
      return 0;
    }
  }
  function touch() {
    const now = Date.now();
    localLast = now;
    if (now - lastWrite > 3000) {
      lastWrite = now;
      try {
        localStorage.setItem(ACTIVITY_KEY, String(now));
      } catch (_) {}
    }
  }
  function setShieldFlag(on) {
    try {
      if (on) sessionStorage.setItem(SHIELD_KEY, '1');
      else sessionStorage.removeItem(SHIELD_KEY);
    } catch (_) {}
  }
  function lockWorkspace() {
    window.dispatchEvent(new CustomEvent('care:before-session-lock'));
    document.querySelector('#staffApp')?.setAttribute('hidden', '');
    document.querySelector('main')?.setAttribute('inert', '');
    document.querySelectorAll('dialog[open]').forEach((dialog) => dialog.close());
  }
  function shield() {
    if (shielded || current?.role !== 'staff') return;
    shielded = true;
    setShieldFlag(true);
    lockWorkspace();
    window.dispatchEvent(new CustomEvent('care:shielded'));
  }
  async function resumeFromOtherTab() {
    // Another tab of this browser was unlocked with the password after this one was shielded.
    try {
      const user = accept(
        await request('/session', {
          authenticated: false,
        }),
      );
      if (user?.role === 'staff') {
        shielded = false;
        setShieldFlag(false);
        window.dispatchEvent(
          new CustomEvent('care:unshielded', {
            detail: user,
          }),
        );
      }
    } catch (_) {}
  }
  function checkIdle() {
    if (current?.role !== 'staff') return;
    const last = Math.max(storedActivity(), shielded ? 0 : localLast),
      remaining = IDLE_MS - (Date.now() - last);
    if (shielded) {
      if (storedActivity() && Date.now() - storedActivity() < IDLE_MS) resumeFromOtherTab();
      return;
    }
    if (remaining <= 0) shield();
    else {
      clearTimeout(idleTimer);
      idleTimer = setTimeout(checkIdle, Math.min(remaining, 30000));
    }
  }
  function activity() {
    if (current?.role === 'staff' && !shielded) {
      touch();
      checkIdle();
    }
  }
  for (const type of ['pointerdown', 'keydown'])
    window.addEventListener(type, (event) => {
      if (event.isTrusted) activity();
    });
  window.addEventListener('pageshow', checkIdle);
  window.addEventListener('visibilitychange', checkIdle);
  window.addEventListener('storage', (event) => {
    if (event.key === ACTIVITY_KEY) checkIdle();
  });
  const { request, failure } = CareTransport;
  function accept(value) {
    const user = value?.user;
    if (
      !value ||
      typeof value.csrfToken !== 'string' ||
      !value.csrfToken ||
      (user !== null &&
        (!user ||
          typeof user.name !== 'string' ||
          !user.name ||
          !['resident', 'staff'].includes(user.role) ||
          (user.role === 'resident'
            ? !Number.isSafeInteger(user.residentId) || user.residentId < 1
            : typeof user.staffId !== 'string' || !user.staffId)))
    ) {
      throw failure('INVALID_RESPONSE', '登录服务返回的会话格式无效。');
    }
    current = user;
    CareTransport.setCsrfToken(value.csrfToken);
    return user;
  }
  function read() {
    if (!checking) {
      const generation = sessionGeneration;
      checking = request('/session', {
        authenticated: false,
      })
        .then((value) => {
          if (generation !== sessionGeneration) throw failure('UNAUTHENTICATED', '登录已失效，请重新登录。');
          return accept(value);
        })
        .finally(() => {
          checking = null;
        });
    }
    return checking;
  }
  async function login(role, username, password) {
    await read();
    const user = accept(
      await request('/session', {
        method: 'POST',
        body: {
          role,
          username,
          password,
        },
        authenticated: false,
      }),
    );
    if (user?.role !== role) {
      try {
        await request('/session', {
          method: 'DELETE',
          authenticated: false,
        });
      } catch (_) {
        current = null;
        CareTransport.setCsrfToken('');
        throw failure('SESSION_REVOKE_FAILED', '账号入口不符，退出结果未确认。请重新读取会话后重试。');
      }
      current = null;
      CareTransport.setCsrfToken('');
      throw failure('FORBIDDEN', '该账号不能访问此入口，已退出该会话。');
    }
    activity();
    try {
      localStorage.setItem('yihe-session-epoch', String(Date.now()));
    } catch (_) {}
    return user;
  }
  async function requireRole(role) {
    const user = await read();
    if (!user) throw failure('UNAUTHENTICATED', '请先登录。');
    if (user.role !== role) throw failure('FORBIDDEN', '该账号不能访问此入口，请切换账号。');
    return user;
  }
  async function logout() {
    await read();
    await request('/session', {
      method: 'DELETE',
      authenticated: false,
    });
    current = null;
    clearTimeout(idleTimer);
    CareTransport.setCsrfToken('');
  }
  let baseTitle = null;
  function clearShieldTitle() {
    if (baseTitle !== null) {
      document.title = baseTitle;
      baseTitle = null;
    }
  }
  function shieldNotice(count) {
    const node = document.getElementById('shieldNotice');
    if (!node) return;
    if (count > 0) {
      node.textContent = '有 ' + count + ' 条预警等待处理，请解锁查看。';
      node.hidden = false;
      if (baseTitle === null) baseTitle = document.title;
      document.title = '【' + count + ' 条预警】' + baseTitle;
    } else {
      node.hidden = true;
      node.textContent = '';
      clearShieldTitle();
    }
  }
  function staffGate() {
    const el = (id) => document.getElementById(id),
      callbacks = [];
    let ready = null,
      pending = false;
    const card = el('adminLoginOverlay').querySelector('.login-card'),
      title = el('adminLoginTitle'),
      sub = card.querySelector('.login-sub'),
      notice = el('shieldNotice'),
      originalTitle = title.textContent,
      originalSub = sub.textContent;
    const nameField = el('adminLoginName'),
      passwordField = el('adminLoginPassword'),
      button = el('adminLoginSubmit'),
      label = button.querySelector('[data-login-label]'),
      originalLabel = label?.textContent;
    const clearHint = () => {
      el('adminLoginHint').hidden = true;
      for (const field of [nameField, passwordField]) {
        field.removeAttribute('aria-invalid');
        field.removeAttribute('aria-describedby');
      }
    };
    const hint = (message, invalidFields = [], focusField = null) => {
      const node = el('adminLoginHint');
      node.textContent = message;
      node.hidden = false;
      for (const field of [nameField, passwordField]) field.setAttribute('aria-describedby', node.id);
      for (const field of invalidFields) field.setAttribute('aria-invalid', 'true');
      (focusField || passwordField).focus();
    };
    function publish(user) {
      if (user?.role !== 'staff') return;
      ready = user;
      shielded = false;
      setShieldFlag(false);
      touch();
      activity();
      el('adminLoginOverlay').hidden = true;
      passwordField.value = '';
      clearHint();
      title.textContent = originalTitle;
      sub.textContent = originalSub;
      notice.hidden = true;
      notice.textContent = '';
      clearShieldTitle();
      callbacks.forEach((cb) => Promise.resolve(cb(user)).catch((e) => hint(e.message)));
      window.dispatchEvent(new CustomEvent('care:authenticated'));
    }
    async function submit() {
      if (pending) return;
      const name = nameField.value.trim(),
        password = passwordField.value;
      clearHint();
      if (!name || !password) {
        const missing = !name ? nameField : passwordField;
        hint(!name ? '请输入账号' : '请输入密码', [missing], missing);
        return;
      }
      pending = true;
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      if (label) label.textContent = '正在验证…';
      try {
        publish(await login('staff', name, password));
      } catch (e) {
        hint(e.message, e.code === 'UNAUTHENTICATED' ? [nameField, passwordField] : [], passwordField);
      } finally {
        pending = false;
        button.disabled = false;
        button.removeAttribute('aria-busy');
        if (label) label.textContent = originalLabel;
      }
    }
    button.addEventListener('click', submit);
    nameField.addEventListener('input', clearHint);
    passwordField.addEventListener('input', clearHint);
    passwordField.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        submit();
      }
    });
    nameField.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        passwordField.focus();
      }
    });
    nameField.focus();
    el('btnAdminLogout').addEventListener('click', async () => {
      el('btnAdminLogout').disabled = true;
      try {
        await logout();
        location.reload();
      } catch (e) {
        el('careError').textContent = '退出未完成：' + e.message;
        el('careError').hidden = false;
      } finally {
        el('btnAdminLogout').disabled = false;
      }
    });
    function showShield() {
      ready = null;
      title.textContent = '页面已锁定';
      sub.textContent = '为保护居民信息，内容已隐藏。请重新输入密码解锁；有紧急预警时会在此提示。';
      el('adminLoginOverlay').hidden = false;
      nameField.value = current?.username || nameField.value;
      (nameField.value ? passwordField : nameField).focus();
    }
    window.addEventListener('care:shielded', showShield);
    window.addEventListener('care:unshielded', (event) => publish(event.detail));
    read()
      .then((user) => {
        if (user && user.role !== 'staff') {
          hint('当前是居民账号，请使用工作人员账号登录。');
          return;
        }
        const stored = storedActivity(),
          stale = user && stored && Date.now() - stored > IDLE_MS;
        let flagged = false;
        try {
          flagged = sessionStorage.getItem(SHIELD_KEY) === '1';
        } catch (_) {}
        if (user && (flagged || stale)) {
          shielded = true;
          setShieldFlag(true);
          showShield();
          return;
        } // reload never bypasses the shield
        publish(user);
      })
      .catch((e) => hint(e.message));
    window.addEventListener('care:unauthenticated', () => {
      ready = null;
      title.textContent = originalTitle;
      sub.textContent = originalSub;
      notice.hidden = true;
      clearShieldTitle();
      el('adminLoginOverlay').hidden = false;
      hint('登录已失效；草稿仅保留在当前页面，请使用原账号重新登录。', [], nameField);
    });
    return {
      onReady: (cb) => {
        callbacks.push(cb);
        if (ready) Promise.resolve(cb(ready)).catch((e) => hint(e.message));
      },
      getLogin: () => ready,
      isLoggedIn: () => !!ready,
      isShielded: () => shielded,
      shieldNotice,
    };
  }
  /* Cross-tab consistency: a login in any tab replaces the shared session cookie, so every other tab must pick
     up the new CSRF token (or lock if the account changed) instead of failing on its next submit. */
  const EPOCH_KEY = 'yihe-session-epoch';
  const sameIdentity = (a, b) => !!a && !!b && a.role === b.role && (a.role === 'staff' ? a.staffId === b.staffId : a.residentId === b.residentId);
  async function adoptServerSession(staleToken = null) {
    let value;
    try {
      value = await request('/session', { authenticated: false });
    } catch (_) {
      return 'denied';
    }
    const user = value?.user ?? null;
    if (!user || (current && !sameIdentity(current, user))) {
      window.dispatchEvent(new CustomEvent('care:unauthenticated'));
      return 'unauthenticated';
    }
    if (staleToken !== null && value.csrfToken === staleToken) return 'denied';
    accept(value);
    return 'retry';
  }
  CareTransport.setCsrfRecovery((stale) => (current ? adoptServerSession(stale) : 'denied'));
  window.addEventListener('storage', (event) => {
    if (event.key === EPOCH_KEY && current) adoptServerSession();
  });
  let residentReauth = null;
  window.addEventListener('care:unauthenticated', () => {
    // Snapshot drafts before any listener hides the workspace or closes dialogs.
    window.dispatchEvent(new CustomEvent('care:before-session-lock'));
    sessionGeneration++;
    shielded = false;
    setShieldFlag(false);
    current = null;
    clearTimeout(idleTimer);
    CareTransport.setCsrfToken('');
    document.querySelector('#staffApp')?.setAttribute('hidden', '');
    document.querySelector('main')?.setAttribute('inert', '');
    document.querySelectorAll('dialog[open]').forEach((dialog) => dialog.close());
    if (document.querySelector('#staffApp') || document.querySelector('#loginForm')) return;
    if (!residentReauth) {
      residentReauth = createResidentReauth();
    }
    residentReauth.showModal();
  });
  return {
    read,
    login,
    requireRole,
    logout,
    staffGate,
    isShielded: () => shielded,
    shieldNotice,
    get current() {
      return current;
    },
  };
  function createResidentReauth() {
    const dialog = document.createElement('dialog'),
      form = document.createElement('form'),
      heading = document.createElement('h2'),
      hint = document.createElement('p');
    dialog.className = 'care-dialog';
    heading.textContent = '登录已失效';
    hint.textContent = '草稿仅保留在当前页面，请使用原账号重新登录。';
    hint.setAttribute('role', 'alert');
    const name = document.createElement('input'),
      password = document.createElement('input'),
      button = document.createElement('button');
    name.className = 'input';
    password.className = 'input';
    button.className = 'btn btn-primary';
    name.autocomplete = 'username';
    password.type = 'password';
    password.autocomplete = 'current-password';
    button.textContent = '重新登录';
    button.type = 'submit';
    form.append(heading, hint);
    for (const [label, field] of [
      ['账号', name],
      ['密码', password],
    ]) {
      appendReauthField(form, label, field);
    }
    form.append(button);
    dialog.append(form);
    document.body.append(dialog);
    dialog.addEventListener('cancel', (event) => event.preventDefault());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      button.disabled = true;
      try {
        await login('resident', name.value.trim(), password.value);
        password.value = '';
        dialog.close();
        window.dispatchEvent(new CustomEvent('care:authenticated'));
      } catch (e) {
        hint.textContent = e.message;
        password.value = '';
      } finally {
        button.disabled = false;
      }
    });
    return dialog;
  }
  function appendReauthField(form, label, field) {
    const node = document.createElement('label');
    node.textContent = label;
    node.append(field);
    form.append(node);
  }
})();
