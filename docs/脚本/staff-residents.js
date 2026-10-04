/* 工作人员控制台 · 居民：居民列表与详情、联系人与责任人表单、草稿恢复。 */
(() => {
  'use strict';
  const S = window.StaffConsole;
  const { h, time, mount, empty, staff, el, remote, error, options, name, openEvents } = S;

  S.context = function context(p) {
    const count = openEvents().filter((e) => e.residentId === p.id).length;
    return (
      `<p>血氧 <strong>${CareUI.metric(p.bloodOxygen)}%</strong> · 心率 ${CareUI.metric(p.heartRate)} bpm</p><p clas` +
      `s="data-note">采样 ${h(time(p.updatedAt))} · ${h(CareUI.device(p))}</p><p class="data-note">` +
      `${h(CareUI.direction(p))}</p><p>未完成事件 ${count} · 责任人 ${h(staff(p.responsible))}</p>`
    );
  };

  S.renderResidents = function renderResidents(announcePage = false) {
    const pagination = el('residentPagination'),
      focusedPage = pagination.contains(document.activeElement) ? document.activeElement.dataset.page : null;
    const query = el('residentSearch').value.trim(),
      list = Object.values(S.state.residents).filter((p) => p.name.includes(query));
    const pages = Math.ceil(list.length / 12);
    S.page = Math.max(1, Math.min(S.page, pages || 1));
    CareUI.patch(
      'residentList',
      list.length
        ? list
            .slice((S.page - 1) * 12, S.page * 12)
            .map(S.renderResidentCard)
            .join('')
        : empty('没有符合条件的居民'),
    );
    CareUI.patch(
      'residentPagination',
      pages > 1
        ? `<button class="btn" data-page="prev" ${S.page === 1 ? 'disabled' : ''}>上一页</button><span>${S.page} / ` +
            `${pages}</span><button class="btn" data-page="next" ${S.page === pages ? 'disabled' : ''}>下一页</button>`
        : '',
    );
    if (announcePage)
      el('residentPageStatus').textContent = list.length
        ? `第 ${S.page} 页，共 ${pages} 页；本页显示 ${Math.min(12, Math.max(0, list.length - (S.page - 1) * 12))} 位，共 ${list.length} 位居民。`
        : '未找到符合条件的居民。';
    if (focusedPage) {
      const next = pagination.querySelector(`button[data-page="${focusedPage}"]`);
      if (next && !next.disabled)
        next.focus({
          preventScroll: true,
        });
      else
        pagination.focus({
          preventScroll: true,
        });
    }
  };

  S.openResident = function openResident(id, cleanFormId = null) {
    const p = S.state.residents[id];
    if (!p) return;
    const events = openEvents().filter((e) => e.residentId === p.id);
    const drafts = S.captureResidentDrafts(id, cleanFormId);
    const maintenanceOpen =
      (S.activeResidentId === id && el('residentBody').querySelector('.resident-maintenance')?.open) || false;
    S.activeResidentId = id;
    mount('residentBody', S.renderResidentBody(p, events, maintenanceOpen));
    S.restoreResidentForms(drafts);
    if (el('dangerModal').open) el('dangerModal').close();
    if (!el('residentDialog').open) el('residentDialog').showModal();
    S.setBusy(S.busy);
  };

  S.renderResidentCard = function renderResidentCard(p) {
    return (
      `<article class="panel"><h3>${h(name(p))} <small>${h(p.age)} 岁</small></h3>${S.context(p)}<button class="` +
      `btn" data-resident="${p.id}">查看处置页</button></article>`
    );
  };

  S.restoreResidentForms = function restoreResidentForms(drafts) {
    for (const formId of ['responsibilityForm', 'staffContactForm']) {
      const form = el(formId),
        status = document.createElement('p'),
        reload = document.createElement('button');
      status.className = 'care-error';
      status.setAttribute('role', 'alert');
      status.hidden = true;
      status.textContent = '记录已更新。放弃本表草稿后可载入最新值。';
      reload.className = 'btn';
      reload.type = 'button';
      reload.dataset.formReload = '';
      reload.hidden = true;
      reload.textContent = '放弃本表草稿并载入最新值';
      form.append(status, reload);
    }
    el('residentBody')
      .querySelectorAll('form[data-pid]')
      .forEach((form) => (form.dataset.dirty = 'false'));
    for (const draft of drafts) {
      const target = el(draft.id);
      if (!target) continue;
      target.dataset.revision = draft.revision;
      target.dataset.dirty = 'true';
      for (const field of draft.fields) {
        const input = target.querySelector(`[name="${CSS.escape(field.name)}"]`);
        if (input) {
          if (input.type === 'checkbox') input.checked = field.value;
          else input.value = field.value;
        }
      }
    }
  };

  S.captureResidentDrafts = function captureResidentDrafts(id, cleanFormId) {
    const drafts =
      S.activeResidentId === id
        ? [...el('residentBody').querySelectorAll('form[data-pid]')]
            .filter((form) => form.id !== cleanFormId && form.dataset.dirty === 'true')
            .map((form) => ({
              id: form.id,
              revision: form.dataset.revision,
              fields: [...form.querySelectorAll('input,textarea,select')].map((field) => ({
                name: field.name,
                value: field.type === 'checkbox' ? field.checked : field.value,
              })),
            }))
        : [];
    return drafts;
  };

  S.renderResidentBody = function renderResidentBody(p, events, maintenanceOpen) {
    return (
      `<header class="care-profile-header"><h2 id="residentTitle">${h(name(p))} · 居民处置</h2><p>${h(p.age)} 岁 · ` +
      `${h(p.place || '居住信息待补充')}</p><p>当前未完成问题：${events.length} 项</p><p>责任人员：${h(staff(p.responsible))}</p>` +
      `${CareUI.contacts(p, S.masked)}</header><section class="care-context-grid"><h3>当前问题与健康情况</h3>` +
      `${S.context(p)}` +
      `${events.length ? events.map((e) => CareUI.eventCard(e, S.state, S.masked)).join('') : empty('目前没有未完成事件')}</section><butt` +
      `on class="btn" data-resident="${p.id}">刷新详情</button><details class="resident-maintenance" ` +
      `${maintenanceOpen ? 'open' : ''}><summary>维护资料与用药任务</summary>` +
      `<form id="responsibilityForm" data-pid="${p.id}" data-revision="${p.revision}"><label>居民责任人员<select` +
      ` class="input" name="responsible">${options(p.responsible)}</select></label>` +
      '<button class="btn" type="submit">保存责任人员</button></form><form id="staffContactForm" data-pid="' +
      `${p.id}" data-revision="${p.revision}"><h3>登记首位联系人</h3>` +
      `<label>姓名<input class="input" name="name" value="${h(S.masked ? '' : p.contacts[0]?.name || '')}" required maxl` +
      'ength="30"></label><label>电话<input class="input" name="phone" type="tel" value="' +
      `${h(p.contacts[0]?.phone || '')}" required></label><button class="btn" type="submit">保存联系人</button><p class="data-` +
      `note">只更新首位联系人，其余联系人保留。</p></form><form id="doseForm" data-pid="${p.id}" data-revision="` +
      `${p.revision}"><h3>登记已核对的用药任务</h3><label>用药名称<input class="input" name="name" maxlength="100"` +
      ' required></label><label>计划时间<input class="input" name="dueAt" type="datetime-local" required>' +
      `</label><label><input type="checkbox" name="verified" required> 已核对用药计划${remote ? '' : '（演示）'}</label><button` +
      ' class="btn" type="submit">登记任务</button></form></details><p id="residentError" class="care-error"' +
      ` role="alert" hidden></p><details><summary>病史与健康档案</summary>${CareUI.record(p)}</details><details>` +
      `<summary>指标趋势</summary>${CareUI.trend(p)}</details><details><summary>历史事件</summary>` +
      `${
        S.state.events
          .filter((e) => e.residentId === p.id && !CareAPI.isOpen(e))
          .map((e) => CareUI.eventCard(e, S.state, S.masked))
          .join('') || empty('暂无已完成事件')
      }</details>`
    );
  };

  S.submitResident = function submitResident(event) {
    const form = event.target;
    if (!form.dataset.pid) return;
    event.preventDefault();
    const id = Number(form.dataset.pid),
      data = new FormData(form);
    const expectedRevision = Number(form.dataset.revision);
    S.perform(
      event.submitter,
      () => S.saveResidentForm(form, id, data, expectedRevision),
      () => S.openResident(id, form.id),
      (e) => {
        if (e.code !== 'CONFLICT' || !['responsibilityForm', 'staffContactForm'].includes(form.id)) return false;
        const status = form.querySelector('[role="alert"]'),
          reload = form.querySelector('[data-form-reload]');
        if (status) status.hidden = false;
        if (reload) reload.hidden = false;
        return true;
      },
    );
  };

  S.wireResidentDrafts = function wireResidentDrafts() {
    el('residentDialog').addEventListener('close', () => {
      S.activeResidentId = null;
    });
    for (const type of ['input', 'change'])
      el('residentDialog').addEventListener(type, (event) => {
        const form = event.target.closest('form[data-pid]');
        if (form) form.dataset.dirty = 'true';
      });
  };

  S.saveResidentForm = async function saveResidentForm(form, id, data, expectedRevision) {
    if (form.id === 'responsibilityForm')
      return CareAPI.saveResident(
        id,
        {
          responsible: data.get('responsible'),
        },
        S.actor,
        expectedRevision,
      );
    if (form.id === 'staffContactForm')
      return CareAPI.saveResident(
        id,
        {
          contactUpdates: [
            {
              index: 0,
              value: {
                name: data.get('name'),
                phone: data.get('phone'),
              },
            },
          ],
        },
        S.actor,
        expectedRevision,
      );
    if (form.id === 'doseForm') {
      const name = data.get('name'),
        dueAt = new Date(data.get('dueAt')).toISOString(),
        intent = JSON.stringify([id, name, dueAt]);
      if (!S.doseIntent || S.doseIntent.intent !== intent)
        S.doseIntent = {
          intent,
          key: CareTransport.newIdempotencyKey(),
        };
      const submitted = S.doseIntent,
        result = await CareAPI.addDose(id, name, dueAt, S.actor, submitted.key);
      if (S.doseIntent === submitted) S.doseIntent = null;
      return result;
    }
  };

  S.reloadResidentForm = function reloadResidentForm(b) {
    const form = b.closest('form[data-pid]');
    if (form) {
      b.disabled = true;
      CareAPI.snapshot()
        .then((next) => {
          S.state = next;
          S.revision = next.revision;
          S.openResident(Number(form.dataset.pid), form.id);
        })
        .catch(error)
        .finally(() => {
          if (b.isConnected) b.disabled = false;
        });
    }
    return;
  };

})();
