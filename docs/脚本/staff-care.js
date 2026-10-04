/* 工作人员控制台 · 启动：导航、全局点击分发、会话恢复与登录后启动。 */
(() => {
  'use strict';
  const S = window.StaffConsole;
  const { h, mount, el, titles, remote, recovery, error, options } = S;

  S.closeNav = function closeNav(restoreFocus = true) {
    const panel = el('menuPanel'),
      trigger = el('hamburgerBtn'),
      wasOpen = trigger.getAttribute('aria-expanded') === 'true',
      mobile = window.matchMedia('(max-width: 1023px)').matches;
    panel.classList.remove('open');
    panel.inert = mobile;
    el('menuOverlay').classList.remove('open');
    el('menuOverlay').inert = true;
    document.querySelector('.console-workspace').inert = false;
    trigger.setAttribute('aria-expanded', 'false');
    if (restoreFocus && wasOpen) trigger.focus();
  };

  S.navigate = function navigate(next) {
    if (!titles[next]) next = 'summary';
    S.view = next;
    S.closeNav(false);
    document.querySelectorAll('.view').forEach((x) => x.classList.toggle('active', x.id === 'view-' + next));
    document.querySelectorAll('.menu-item').forEach((x) => {
      x.classList.toggle('active', x.dataset.view === next);
      x.setAttribute('aria-current', x.dataset.view === next ? 'page' : 'false');
    });
    el('topbarTitle').textContent = titles[next];
    history.replaceState(null, '', '#' + next);
    S.render();
    el('topbarTitle').focus();
  };

  S.wire = function wire() {
    S.wireResidentDrafts();
    S.wireNavigation();
    S.wireFilters();
    S.wireSettings();
    S.wireActions();
    S.wireStorage();
  };

  window.addEventListener('care:before-session-lock', () => {
    S.suspendedDraft = S.suspendedDraft || {
      actor: S.actor,
      eventOpen: el('eventDialog').open,
      residentOpen: el('residentDialog').open,
      residentId: S.activeResidentId,
    };
  });

  AdminLogin.onReady(async (session) => {
    if (S.initialized) {
      await S.resumeStaffSession(session);
      return;
    }
    await S.startStaffSession(session);
  });

  S.handleDocumentClick = function handleDocumentClick(event) {
    const b = event.target.closest('button');
    if (!b) return;
    if (b.hasAttribute('data-form-reload')) {
      S.reloadResidentForm(b);
      return;
    }
    if (b.dataset.quickClaim) {
      const target = S.state.events.find((x) => x.id === b.dataset.quickClaim);
      if (target) S.perform(b, () => CareAPI.transition(target.id, 'claim', S.actor, '', target.revision));
      return;
    }
    if (b.dataset.event) S.openEvent(b.dataset.event);
    if (b.dataset.resident) S.openResident(Number(b.dataset.resident));
    if (b.dataset.todo) {
      el('todoFilter').value = b.dataset.todo;
      S.render();
    }
    if (b.dataset.page) {
      S.page += b.dataset.page === 'next' ? 1 : -1;
      S.renderResidents(true);
    }
    if (b.dataset.sim) S.perform(b, () => CareAPI.simulate(Number(el('simulationResident').value), b.dataset.sim));
    if (b.dataset.eventAction)
      S.perform(
        b,
        () =>
          CareAPI.transition(
            S.eventId,
            b.dataset.eventAction,
            S.actor,
            el('eventNote')?.value || '',
            S.eventRevision,
            el('escalateTo')?.value,
          ),
        () => S.openEvent(S.eventId, el('eventNote')?.value || ''),
      );
  };

  S.wireNavigation = function wireNavigation() {
    document
      .querySelectorAll('[data-view]')
      .forEach((b) => b.addEventListener('click', () => S.navigate(b.dataset.view)));
    const navQuery = window.matchMedia('(max-width: 1023px)'),
      panel = el('menuPanel'),
      overlay = el('menuOverlay'),
      workspace = document.querySelector('.console-workspace');
    panel.inert = navQuery.matches;
    overlay.inert = true;
    workspace.inert = false;
    const onNavBreakpoint = (event) => {
      if (!event.matches) {
        S.closeNav(false);
        panel.inert = false;
        workspace.inert = false;
      } else {
        const isOpen = el('hamburgerBtn').getAttribute('aria-expanded') === 'true';
        panel.inert = !isOpen;
        overlay.inert = !isOpen;
        workspace.inert = isOpen;
      }
    };
    if (navQuery.addEventListener) navQuery.addEventListener('change', onNavBreakpoint);
    else navQuery.addListener(onNavBreakpoint);
    el('hamburgerBtn').onclick = () => {
      if (!navQuery.matches) return;
      panel.inert = false;
      panel.classList.add('open');
      overlay.inert = false;
      overlay.classList.add('open');
      workspace.inert = true;
      el('hamburgerBtn').setAttribute('aria-expanded', 'true');
      panel.querySelector('.menu-item')?.focus();
    };
    el('menuOverlay').onclick = () => S.closeNav();
    el('menuClose').onclick = () => S.closeNav();
    document.addEventListener('keydown', (e) => S.handleNavKeydown(e, navQuery, panel));
  };

  S.handleNavKeydown = function handleNavKeydown(e, navQuery, panel) {
    const open = navQuery.matches && el('hamburgerBtn').getAttribute('aria-expanded') === 'true';
    if (e.key === 'Escape' && open) {
      S.closeNav();
      return;
    }
    if (e.key === 'Tab' && open) {
      const focusable = [
          ...panel.querySelectorAll(
            'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])' +
              ',[tabindex]:not([tabindex="-1"])',
          ),
        ].filter((node) => !node.hidden),
        first = focusable[0],
        last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first?.focus();
      }
    }
  };

  S.resumeStaffSession = async function resumeStaffSession(session) {
    if (remote && session.staffId !== S.actor) {
      mount('eventBody', '');
      mount('residentBody', '');
      location.reload();
      return;
    }
    el('staffApp').hidden = false;
    el('staffApp').inert = false;
    document.querySelector('main').inert = false;
    try {
      await S.refresh(true);
      if (S.suspendedDraft?.actor === S.actor) {
        if (S.suspendedDraft.eventOpen) el('eventDialog').showModal();
        else if (S.suspendedDraft.residentOpen) {
          S.activeResidentId = S.suspendedDraft.residentId;
          el('residentDialog').showModal();
        }
      }
      S.suspendedDraft = null;
    } catch (e) {
      error(e);
    }
    return;
  };

  S.startStaffSession = async function startStaffSession(session) {
    S.initialized = true;
    el('staffApp').hidden = false;
    el('staffApp').inert = false;
    if (remote) {
      S.actor = session.staffId;
      el('accountDescription').textContent = '当前账号：' + session.name;
    }
    recovery.loading();
    try {
      S.masked = !!loadJSON('hm-settings', {}).mask;
      el('settingMask').checked = S.masked;
      mount('currentStaff', options(S.actor));
      mount(
        'typeFilter',
        '<option value="">全部类型</option>' +
          Object.entries(CareAPI.TYPES)
            .map(([key, label]) => `<option value="${h(key)}">${h(label)}</option>`)
            .join(''),
      );
      S.wire();
      const initial = await CareAPI.init();
      await S.refresh(true, initial);
      S.navigate(location.hash.slice(1) || 'summary');
    } catch (e) {
      error(e);
    }
    CareUI.poll(
      async () => {
        if (S.busy || (remote && !CareSession.current)) return;
        if (!remote) await CareAPI.scan();
        await S.refresh();
      },
      error,
      {
        background: true,
      },
    );
  };

})();
