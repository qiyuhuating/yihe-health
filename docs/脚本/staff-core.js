/* 工作人员控制台 · 核心：共享状态、读取与刷新、写操作入口。
   所有模块通过 window.StaffConsole（下称 S）共享状态与函数；控制台逻辑按职责拆为 core / events / residents / map / settings / boot 六个文件。 */
(() => {
  'use strict';
  const S = (window.StaffConsole = {});

  const { h, time, mount, empty, staff } = CareUI,
    el = (id) => document.getElementById(id);

  Object.assign(S, { h, time, mount, empty, staff, el });

  const titles = {
    summary: '工作台',
    alerts: '预警',
    residents: '居民',
    map: '地图',
    more: '更多',
  };

  Object.assign(S, { titles });
  const remote = CareAPI.mode === 'http';
  Object.assign(S, { remote });
  S.state = undefined;
  S.view = 'summary';
  S.actor = 'staff-1';
  S.masked = false;
  S.page = 1;
  S.eventId = null;
  S.eventRevision = null;
  S.activeResidentId = null;
  S.busy = false;
  S.initialized = false;
  S.revision = -1;
  S.renderedMinute = -1;
  S.refreshRequest = 0;
  S.doseIntent = null;
  S.suspendedDraft = null;
  const seen = new Set();
  Object.assign(S, { seen });

  S.setBusy = function setBusy(value) {
    S.busy = value;
    document
      .querySelectorAll(
        '#eventBody input,#eventBody textarea,#eventBody select,#eventBody [data-event-action],#residentBody' +
          ' input,#residentBody select,#residentBody button[type="submit"],#currentStaff,[data-sim]',
      )
      .forEach((node) => (node.disabled = value));
    if (remote) el('currentStaff').disabled = true;
    if (!CareAPI.STAFF.some((s) => s.id !== S.actor))
      document.querySelector('[data-event-action="escalated"]')?.setAttribute('disabled', '');
  };

  const recovery = CareUI.recovery(async () => {
    const note = el('eventNote')?.value || '',
      eventOpen = el('eventDialog').open;
    const residentOpen = el('residentDialog').open ? S.activeResidentId : null;
    const initial = await CareAPI.init();
    await S.refresh(true, initial);
    if (eventOpen) S.openEvent(S.eventId, note);
    else if (residentOpen !== null) {
      if (S.state.residents[residentOpen]) {
        S.openResident(residentOpen);
      } else {
        el('residentDialog').close();
        S.activeResidentId = null;
      }
    }
  });

  Object.assign(S, { recovery });
  const error = recovery.show;
  Object.assign(S, { error });

  const options = (value) =>
    CareAPI.STAFF.map(
      (s) => `<option value="${h(s.id)}" ${s.id === value ? 'selected' : ''}>${h(s.name)}</option>`,
    ).join('');

  Object.assign(S, { options });
  const name = (p) => displayName(p.name, S.masked);
  Object.assign(S, { name });
  const openEvents = () => S.state.events.filter(CareAPI.isOpen);
  Object.assign(S, { openEvents });

  S.render = function render() {
    if (!S.state) return;
    const focusTarget = document.activeElement,
      focusView = focusTarget.closest?.('.view'),
      focused = focusTarget?.dataset?.todo
        ? ['todo', focusTarget.dataset.todo]
        : focusTarget?.dataset?.event
          ? ['event', focusTarget.dataset.event]
          : focusTarget?.dataset?.resident
            ? ['resident', focusTarget.dataset.resident]
            : null;
    S.renderWorkSummary();
    S.renderAlerts();
    if (S.view === 'residents') S.renderResidents();
    if (S.view === 'map') S.renderMap();
    if (S.view === 'more') S.renderMore();
    if (focused && !focusTarget.isConnected) {
      const scope = focusView?.isConnected ? focusView : document,
        node = scope.querySelector(`[data-${focused[0]}="${CSS.escape(String(focused[1]))}"]`);
      node?.focus();
    }
  };

  S.refresh = async function refresh(force = false, snapshotValue = null) {
    const request = ++S.refreshRequest,
      next = snapshotValue || (await CareAPI.snapshot());
    if (request !== S.refreshRequest) return;
    if (remote) {
      if (!CareAPI.STAFF.some((person) => person.id === S.actor))
        throw Object.assign(Error('当前工作人员不在服务返回的授权人员列表中。'), {
          code: 'FORBIDDEN',
        });
      mount('currentStaff', options(S.actor));
      el('currentStaff').disabled = true;
    }
    const minute = Math.floor(CareAPI.now() / 60000);
    if (
      force ||
      next.revision !== S.revision ||
      minute !== S.renderedMinute ||
      document.body.hasAttribute('data-care-unavailable')
    ) {
      S.state = next;
      S.revision = next.revision;
      S.renderedMinute = minute;
      S.render();
    }
    recovery.restored();
    const fresh = S.state.events.filter(
      (e) =>
        CareAPI.isOpen(e) &&
        (!e.owner || e.owner === S.actor) &&
        !seen.has(e.id + ':' + e.notification.cycle) &&
        e.notification.cycle > (e.notification.acknowledgedCycle || 0),
    );
    CareUI.badge(S.state.events.filter((e) => CareAPI.isOpen(e) && (!e.owner || e.owner === S.actor)).length);
    // While the screen is shielded nothing can be displayed, so nothing is marked as presented.
    if (window.CareSession?.isShielded?.()) {
      window.CareSession.shieldNotice(fresh.length);
      return;
    }
    const editing = el('residentDialog').open || el('eventDialog').open,
      urgent = fresh.filter((e) => e.severity === 'danger');
    // Urgent alerts always surface, on top of an open edit dialog (it stays open, drafts untouched).
    // Other alerts never interrupt editing; they are announced inside the dialog and shown once it closes.
    if (fresh.length && (!editing || urgent.length)) await S.presentAlerts(editing ? urgent : fresh);
    S.announceBehindDialog(editing ? fresh.filter((e) => e.severity !== 'danger' || !urgent.includes(e)).length : 0);
  };

  S.perform = async function perform(button, fn, after, onError) {
    if (S.busy) return;
    S.setBusy(true);
    button.disabled = true;
    let saved = false;
    try {
      const result = await fn();
      saved = true;
      const snapshotValue = result && result.residents && Array.isArray(result.events) ? result : null;
      await S.refresh(true, snapshotValue);
      if (after) after();
      recovery.clear();
      showToast('操作已完成');
    } catch (e) {
      if (onError && onError(e)) return;
      if (saved) e.message = '操作已完成，但最新记录读取失败。' + e.message;
      const local = el('eventDialog').open ? el('eventError') : el('residentDialog').open ? el('residentError') : null;
      if (local) {
        local.textContent = e.message;
        local.hidden = false;
      } else error(e);
      if (e.code) error(e);
      if (button.id === 'simToggle' && S.state) button.checked = S.state.simulator;
    } finally {
      S.setBusy(false);
      if (button.isConnected) button.disabled = false;
    }
  };

  S.handleStorage = function handleStorage(event) {
    if (event.key === CareAPI.KEY) S.refresh(true).catch(error);
    if (event.key === null) location.reload();
    if (event.key === 'hm-settings') {
      S.masked = !!loadJSON('hm-settings', {}).mask;
      el('settingMask').checked = S.masked;
      S.render();
      if (el('eventDialog').open) S.openEvent(S.eventId, el('eventNote')?.value || '');
    }
  };

  S.wireStorage = function wireStorage() {
    window.addEventListener('care:change', () => S.refresh().catch(error));
    window.addEventListener('storage', S.handleStorage);
  };

})();
