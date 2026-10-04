/* 工作人员控制台 · 事件：预警列表、工作摘要、事件详情与处置、新预警提示。 */
(() => {
  'use strict';
  const S = window.StaffConsole;
  const { h, time, mount, empty, staff, el, seen, options, name, openEvents } = S;

  S.eventList = function eventList(id, list) {
    CareUI.patch(
      id,
      list.length
        ? list
            .slice()
            .sort(
              (a, b) =>
                Number(b.severity === 'danger') - Number(a.severity === 'danger') ||
                Number(CareAPI.now() > Date.parse(b.dueAt)) - Number(CareAPI.now() > Date.parse(a.dueAt)) ||
                Date.parse(a.createdAt) - Date.parse(b.createdAt),
            )
            .map((e) => CareUI.eventCard(e, S.state, S.masked))
            .join('')
        : empty('当前范围没有事件'),
    );
  };

  S.announceBehindDialog = function announceBehindDialog(count) {
    document.querySelectorAll('.dialog-alert-note').forEach((note) => {
      note.hidden = count === 0;
      note.textContent = count ? '另有 ' + count + ' 条新预警等待查看；关闭本窗口后会提示。' : '';
    });
  };

  S.openEvent = function openEvent(id, note = '') {
    const e = S.state.events.find((x) => x.id === id);
    if (!e) return;
    S.eventId = id;
    S.eventRevision = e.revision;
    const p = S.state.residents[e.residentId];
    const controls = S.eventControls(e);
    const steps = [
        ['new', '待接单'],
        ['claimed', e.state === 'escalated' ? '已转交' : '已接单'],
        ['processing', '处理中'],
        ['terminal', '已结案'],
      ],
      stage =
        e.state === 'resolved' || e.state === 'false_alarm'
          ? 'terminal'
          : e.state === 'escalated'
            ? 'claimed'
            : e.state;
    const stepIndex = steps.findIndex(([key]) => key === stage);
    mount('eventBody', S.renderEventBody(e, p, note, controls, steps, stepIndex));
    if (el('residentDialog').open) {
      el('residentDialog').close();
      S.activeResidentId = null;
    }
    if (el('dangerModal').open) el('dangerModal').close();
    if (!el('eventDialog').open) el('eventDialog').showModal();
    S.setBusy(S.busy);
  };

  S.renderWorkCount = function renderWorkCount([key, label, count], selected) {
    return (
      `<button class="work-count" data-todo="${h(key)}" aria-pressed="${selected === key}"><strong>${count}</strong><span>` +
      `${h(label)}</span></button>`
    );
  };

  S.renderEscalationOptions = function renderEscalationOptions(e) {
    return e.state === 'processing' && e.owner === S.actor
      ? '<details class="escalation-options"><summary>其他处置选项</summary><button type="button" class="btn"' +
          ' data-event-action="false_alarm">判为误报</button><label>升级接收人<select class="input" id="escalateTo">' +
          `${options(CareAPI.STAFF.find((s) => s.id !== S.actor)?.id || '')}</select></label>` +
          '<button type="button" class="btn" data-event-action="escalated" ' +
          `${CareAPI.STAFF.some((s) => s.id !== S.actor) ? '' : 'disabled'}>升级给其他人员</button></details>`
      : '';
  };

  S.renderEventEditor = function renderEventEditor(e, note, controls) {
    return CareAPI.isOpen(e)
      ? '<label>处置记录<textarea class="input" id="eventNote" rows="3" maxlength="1000" placeholder="记录联系、核实与处理结' +
          `果">${h(note)}</textarea></label><p class="care-error" id="eventError" role="alert" hidden></p><div class="eve` +
          `nt-actions">${controls}${S.renderEscalationOptions(e)}</div>`
      : `<p class="data-note">此事件已结案，处置记录见下方时间线。</p>`;
  };

  S.renderDispositionStep = function renderDispositionStep([key, label], i, stepIndex) {
    return (
      `<li class="${i === stepIndex ? 'current' : i < stepIndex ? 'is-complete' : ''}" ` +
      `${i === stepIndex ? 'aria-current="step"' : ''}>${label}</li>`
    );
  };

  S.renderTimelineEntry = function renderTimelineEntry(t) {
    return (
      `<li><strong>${h(t.action)}</strong><span>${h(time(t.at))} · ` +
      `${h(t.actor === 'system' ? '系统' : staff(t.actor))}</span><p>${h(t.note)}</p></li>`
    );
  };

  S.renderAlerts = function renderAlerts() {
    const filter = el('eventFilter').value,
      type = el('typeFilter').value,
      query = el('eventSearch').value.trim().toLocaleLowerCase();
    const matches = (e) =>
      !query ||
      S.state.residents[e.residentId].name.toLocaleLowerCase().includes(query) ||
      e.detail.toLocaleLowerCase().includes(query);
    const events = S.state.events.filter(
      (e) =>
        (filter === 'all' || (filter === 'open' && CareAPI.isOpen(e)) || e.state === filter) &&
        (!type || e.type === type) &&
        matches(e),
    );
    el('eventHeading').textContent = `预警事件 · ${events.length} 件`;
    S.eventList('allEvents', events);
  };

  S.renderWorkSummary = function renderWorkSummary() {
    const pending = openEvents(),
      mine = pending.filter((e) => e.owner === S.actor),
      unassigned = pending.filter((e) => !e.owner),
      overdue = pending.filter((e) => CareAPI.now() > Date.parse(e.dueAt));
    const selected = el('todoFilter').value;
    CareUI.patch(
      'workCounts',
      [
        ['all', '待处理', pending.length],
        ['mine', '我的待办', mine.length],
        ['unassigned', '未分配', unassigned.length],
        ['overdue', '超时', overdue.length],
      ]
        .map((item) => S.renderWorkCount(item, selected))
        .join(''),
    );
    const filters = {
        all: pending,
        mine,
        unassigned,
        overdue,
      },
      todo = filters[selected];
    el('todoHeading').textContent = `${el('todoFilter').selectedOptions[0].textContent} · ${todo.length} 件`;
    S.eventList('todoEvents', todo);
  };

  S.eventControls = function eventControls(e) {
    const action = (key, label) =>
      '<button type="button" class="btn ' +
      `${['claim', 'processing', 'resolved'].includes(key) ? 'btn-primary' : ''}" data-event-action="${key}">` +
      `${label}</button>`;
    let controls = '';
    if (e.state === 'new') controls = action('claim', '接单');
    else if (e.owner === S.actor && ['claimed', 'escalated'].includes(e.state))
      controls = action('processing', '开始处理');
    else if (e.owner === S.actor && e.state === 'processing') controls = action('resolved', '完成处置');
    else if (CareAPI.isOpen(e)) controls = '<p>由 ' + h(staff(e.owner)) + ' 跟进；当前人员不能代其提交处置。</p>';
    return controls;
  };

  S.renderEventBody = function renderEventBody(e, p, note, controls, steps, stepIndex) {
    return (
      `<h2 id="eventTitle">${h(name(p))} · ${h(CareAPI.TYPES[e.type])}</h2><ol class="disposition-steps"` +
      ` aria-label="处置进度">${steps.map((step, i) => S.renderDispositionStep(step, i, stepIndex)).join('')}</ol><section c` +
      `lass="care-context-grid"><h3>事件与责任</h3><p class="event-detail">${h(e.detail)}</p><p>` +
      `${h(CareAPI.STATES[e.state])} · 当前负责人 ${h(staff(e.owner))}</p><p>居民责任人 ${h(staff(p.responsible))}</p><p>发现 ` +
      `${h(time(e.createdAt))} · 处理时限 ${h(time(e.dueAt))}</p>${CareUI.contacts(p, S.masked)}</section><sectio` +
      `n class="signal-status"><h3>信号状态</h3><p>${e.sourceRecoveredAt ? '信号已恢复，仍需人工复核结案。' : '异常信号仍待核实。'}</p></section>` +
      `${S.renderEventEditor(e, note, controls)}<h3>处理时间线</h3><ol class="event-timeline">` +
      `${e.history.map(S.renderTimelineEntry).join('')}</ol>`
    );
  };

  S.presentAlerts = async function presentAlerts(fresh) {
    const ordered = [...fresh].sort((a, b) => (a.severity === 'danger' ? 0 : 1) - (b.severity === 'danger' ? 0 : 1)),
      dangerCount = ordered.filter((e) => e.severity === 'danger').length;
    el('dangerModal').dataset.severity = dangerCount ? 'danger' : 'warning';
    el('dangerTitle').textContent = dangerCount ? '有紧急预警需要处理' : '有新的预警需要处理';
    el('newAlertText').textContent =
      fresh.length +
      ' 条新增或变化的预警' +
      (dangerCount ? '（其中 ' + dangerCount + ' 条紧急）' : '') +
      '：' +
      ordered
        .map(
          (e) =>
            (e.severity === 'danger' ? '【紧急】' : '') +
            name(S.state.residents[e.residentId]) +
            ' · ' +
            CareAPI.TYPES[e.type],
        )
        .join('；');
    if (!el('dangerModal').open) el('dangerModal').showModal();
    await CareAPI.markPresented(
      fresh.map((e) => e.id),
      S.actor,
      fresh.map((e) => e.notification.cycle || 1),
    );
    fresh.forEach((e) => seen.add(e.id + ':' + (e.notification.cycle || 1)));
  };

  S.wireActions = function wireActions() {
    el('viewNewAlerts').onclick = () => {
      el('dangerModal').close();
      S.navigate('alerts');
    };
    el('dismissNewAlerts').onclick = () => el('dangerModal').close();
    document.addEventListener('click', S.handleDocumentClick);
    el('residentDialog').addEventListener('submit', S.submitResident);
    el('exportEvents').onclick = S.exportEvents;
  };

  S.wireFilters = function wireFilters() {
    el('currentStaff').onchange = () => {
      S.actor = el('currentStaff').value;
      S.render();
    };
    for (const id of ['todoFilter', 'eventFilter', 'typeFilter']) el(id).onchange = S.render;
    el('eventSearch').oninput = S.render;
    el('resetEventFilters').onclick = () => {
      el('eventSearch').value = '';
      el('eventFilter').value = 'open';
      el('typeFilter').value = '';
      S.render();
      el('eventSearch').focus();
    };
    let residentSearchTimer;
    el('residentSearch').oninput = () => {
      S.page = 1;
      clearTimeout(residentSearchTimer);
      residentSearchTimer = setTimeout(() => S.renderResidents(true), 250);
    };
  };

})();
