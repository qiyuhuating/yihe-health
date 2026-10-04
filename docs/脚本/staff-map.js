/* 工作人员控制台 · 地图：社区安全示意图与待处理安全事件。 */
(() => {
  'use strict';
  const S = window.StaffConsole;
  const { h, mount, remote, name, openEvents } = S;

  S.renderMap = function renderMap() {
    const safety = S.pendingSafety();
    CareUI.patch(
      'safetyMap',
      '<div class="safety-fence"><span>' +
        (remote ? '社区安全围栏 · 位置示意' : '社区安全围栏 · 模拟') +
        '</span></div>' +
        Object.values(S.state.residents)
          .map((p) => S.renderMapResident(p, safety))
          .join(''),
    );
    S.eventList('safetyEvents', safety);
  };

  S.pendingSafety = function pendingSafety() {
    return openEvents().filter((e) => e.type === 'fence' || e.type === 'offline');
  };

  S.renderMapResident = function renderMapResident(p, safety) {
    const active = safety.some((e) => e.residentId === p.id),
      lost = CareAPI.now() - Date.parse(p.locationAt) > 600000;
    return (
      `<button class="map-resident ${active ? 'map-alert' : p.risk ? 'map-risk' : 'map-normal'}" style="left:` +
      `${Math.max(4, Math.min(96, p.pos.x / 8))}%;top:${Math.max(10, Math.min(92, p.pos.y / 6))}%" data-residen` +
      `t="${p.id}" aria-label="${h(name(p))}，${active ? '安全事件待处理' : p.risk ? '高风险居民' : '暂无安全事件'}` +
      `${lost ? '，最后定位已过期' : ''}">${h(name(p))}</button>`
    );
  };

})();
