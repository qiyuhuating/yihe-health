/* Explicit local-only domain boundaries. No notification or device service. */
window.DemoDomain = (() => {
  const read = (key, fallback) => loadJSON(key, fallback);
  const write = (key, value) => {
    try { localStorage.setItem(key, JSON.stringify(value)); return true; }
    catch (_) { showToast('本机保存失败，请检查浏览器存储空间'); return false; }
  };
  const key = (kind, uid) => `yihe-v2-${kind}-${uid}`;
  const id = () => crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
  function medications(patient) {
    if (Array.isArray(patient.medicationPlan)) return patient.medicationPlan;
    const text = patient.medicalHistory?.medications;
    return !text || text === '无' ? [] : text.split(/[，；]/).filter(Boolean).map(name => ({
      id: name.trim(), name: name.trim(), times: [], source: '演示档案，服用时间未核对'
    }));
  }
  function reminders(uid) { return read(key('reminders', uid), []); }
  function saveReminder(uid, task) {
    if (!task.text?.trim() || !Number.isFinite(Date.parse(task.at))) throw new Error('请填写内容和有效时间');
    const list = reminders(uid), item = { ...task, id: task.id || id(), text: task.text.trim(), status: 'pending' };
    const i = list.findIndex(x => x.id === item.id);
    if (i < 0) list.push(item); else list[i] = item;
    return write(key('reminders', uid), list);
  }
  function removeReminder(uid, taskId) { return write(key('reminders', uid), reminders(uid).filter(x => x.id !== taskId)); }
  function dueReminders(uid, now = Date.now()) {
    const list = reminders(uid), due = list.filter(x => x.status === 'pending' && Date.parse(x.at) <= now);
    if (!due.length) return [];
    due.forEach(x => { x.status = 'completed'; x.completedAt = new Date(now).toISOString(); });
    return write(key('reminders', uid), list) ? due : [];
  }
  function requestHelp(uid) {
    const list = read('yihe-help-events', []);
    const event = { id: id(), uid, at: new Date().toISOString(), status: 'simulated', delivered: false };
    list.push(event);
    if (!write('yihe-help-events', list.slice(-100))) throw new Error('演示求助未保存');
    return event;
  }
  return { read, write, key, medications, reminders, saveReminder, removeReminder, dueReminders, requestHelp };
})();
