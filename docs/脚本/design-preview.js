(() => {
  'use strict';
  const rows = document.getElementById('previewResidents');
  const sample = window.SeedData && SeedData.PATIENTS || [];
  sample.slice(0, 4).forEach(person => {
    const tr = document.createElement('tr');
    [person.name, `${person.age} 岁`, `${Number(person.heartRate).toFixed(1)} bpm`, '演示基线'].forEach(value => {
      const td = document.createElement('td');
      td.textContent = value;
      tr.appendChild(td);
    });
    rows.appendChild(tr);
  });
  const theme = document.getElementById('previewTheme');
  function syncTheme() {
    const dark = YiheTheme.get() === 'dark';
    theme.setAttribute('aria-pressed', String(dark));
    theme.textContent = dark ? '切换浅色' : '切换深色';
  }
  theme.addEventListener('click', () => YiheTheme.set(YiheTheme.get() === 'dark' ? 'light' : 'dark'));
  window.addEventListener('yihe:themechange', syncTheme);
  syncTheme();

  const toast = document.getElementById('previewToastMessage');
  let timer;
  function notify(message) {
    toast.textContent = message;
    toast.hidden = false;
    clearTimeout(timer);
    timer = setTimeout(() => { toast.hidden = true; }, 3500);
  }
  document.getElementById('previewToast').addEventListener('click', () => notify('样例反馈：操作已完成，演示数据未改变。'));
  document.getElementById('previewDanger').addEventListener('click', () => notify('危险样例：请立即核实；此操作不会生成预警。'));

  const overlay = document.getElementById('previewOverlay');
  const cancel = document.getElementById('previewCancel');
  const confirm = document.getElementById('previewConfirm');
  const opener = document.getElementById('previewDialog');
  function close() { overlay.hidden = true; opener.focus(); }
  opener.addEventListener('click', () => { overlay.hidden = false; cancel.focus(); });
  cancel.addEventListener('click', close);
  confirm.addEventListener('click', () => { close(); notify('样例已确认，居民数据未改变。'); });
  overlay.addEventListener('click', event => { if (event.target === overlay) close(); });
  overlay.addEventListener('keydown', event => {
    if (event.key === 'Escape') close();
    if (event.key === 'Tab' && event.shiftKey && document.activeElement === cancel) { event.preventDefault(); confirm.focus(); }
    else if (event.key === 'Tab' && !event.shiftKey && document.activeElement === confirm) { event.preventDefault(); cancel.focus(); }
  });
})();
