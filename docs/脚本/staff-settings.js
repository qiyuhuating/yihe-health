/* 工作人员控制台 · 设置：更多页、操作记录与事件导出。 */
(() => {
  'use strict';
  const S = window.StaffConsole;
  const { h, time, mount, empty, staff, el, remote, error, name } = S;

  S.renderMore = function renderMore() {
    if (!remote) {
      const chosen = el('simulationResident').value;
      mount(
        'simulationResident',
        Object.values(S.state.residents)
          .map((p) => `<option value="${p.id}" ${String(p.id) === chosen ? 'selected' : ''}>${h(name(p))}</option>`)
          .join(''),
      );
      el('simToggle').checked = S.state.simulator;
    }
    const records = [
      ...S.state.logs,
      ...S.state.events.flatMap((e) =>
        e.history.map((t) => ({
          ...t,
          residentId: e.residentId,
        })),
      ),
    ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
    CareUI.patch(
      'operationLog',
      records.length ? records.slice(0, 80).map(S.renderOperationRecord).join('') : empty('暂无操作记录'),
    );
    if (remote) return;
    const check = Audit.verify();
    el('auditCheck').textContent =
      '本机记录一致性校验：' +
      (check.ok ? '通过' : '发现异常') +
      (check.retainedOnly ? ' · 仅校验保留记录' : '') +
      '；不证明防篡改。';
    CareUI.patch(
      'legacyAudit',
      Audit.all()
        .slice(-30)
        .reverse()
        .map((r) => `<p>${h(time(r.ts))} · ${h(r.action)} · ${h(r.detail || '')}</p>`)
        .join('') || empty('没有旧记录'),
    );
  };

  S.renderOperationRecord = function renderOperationRecord(r) {
    return (
      `<p>${h(time(r.at))} · ${h(S.state.residents[r.residentId] ? name(S.state.residents[r.residentId]) : '')} · ` +
      `${h(staff(r.actor) === '未分配' ? r.actor : staff(r.actor))} · ${h(r.action)} ${h(r.note || '')}</p>`
    );
  };

  S.exportEvents = async function exportEvents() {
    try {
      const maskAtStart = S.masked,
        s = await CareAPI.snapshot();
      const records = s.events.map((e) => ({
        ...e,
        residentName: displayName(s.residents[e.residentId].name, maskAtStart),
      }));
      const url = URL.createObjectURL(
        new Blob(
          [
            JSON.stringify(
              {
                exportedAt: new Date().toISOString(),
                simulated: !remote,
                events: records,
              },
              null,
              2,
            ),
          ],
          {
            type: 'application/json',
          },
        ),
      );
      const a = document.createElement('a');
      a.href = url;
      a.download = 'yihe-events.json';
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) {
      error(e);
    }
  };

  S.wireSettings = function wireSettings() {
    el('refreshCare').onclick = () => S.perform(el('refreshCare'), () => CareAPI.scan());
    el('settingDarkMode').onchange = () => YiheTheme.set(el('settingDarkMode').checked ? 'dark' : 'light');
    el('settingMask').onchange = () => {
      const previous = S.masked;
      try {
        const settings = loadJSON('hm-settings', {});
        settings.mask = el('settingMask').checked;
        localStorage.setItem('hm-settings', JSON.stringify(settings));
        S.masked = settings.mask;
        S.render();
      } catch (e) {
        el('settingMask').checked = previous;
        error(e);
      }
    };
    if (!remote)
      el('simToggle').onchange = () => S.perform(el('simToggle'), () => CareAPI.setSimulator(el('simToggle').checked));
  };

})();
