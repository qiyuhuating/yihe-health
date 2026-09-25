/* Modal presentation for simulated events. Acknowledgement never means resolved. */
var AlertSystem = {
    _modal: null, _current: null, _queue: [], _busy: false,
    init() {
        this._modal = document.getElementById('dangerModal');
        if (!this._modal) return;
        document.getElementById('btnAcknowledge').addEventListener('click', () => this._dismiss());
        this._modal.addEventListener('dialog:request-close', event => {
            event.preventDefault(); this._dismiss();
        });
    },
    sync(events) {
        const pending = events.filter(event => event.state === 'triggered');
        const ids = new Set(pending.map(event => event.eventId));
        this._queue = this._queue.filter(event => ids.has(event.eventId));
        if (this._current && !ids.has(this._current.eventId) && !this._busy) this._close();
        pending.forEach(event => this.show(event));
        if (!this._current && !this._busy) this._next();
    },
    show(event) {
        if (!this._modal || !event.eventId || event.state === 'acknowledged') return;
        if (this._current?.eventId === event.eventId || this._queue.some(item => item.eventId === event.eventId)) return;
        this._queue.push(event);
        if (!this._current && !this._busy) this._next();
    },
    _next() {
        const event = this._queue.shift();
        if (!event) return;
        this._current = event;
        const text = (id, value) => { document.getElementById(id).textContent = value; };
        text('modalIcon', '!');
        text('modalTitle', event.type === 'fence' ? '模拟越界预警' : '模拟健康预警');
        text('modalPatientName', event.name);
        text('modalDetail', event.alertMsg);
        text('modalPlace', '模拟位置：' + (event.place || '未知'));
        text('modalUrgent', '知晓只记录本机处理状态，不代表异常消失，也不会联系任何人。');
        const queue = document.getElementById('modalQueue');
        queue.hidden = !this._queue.length;
        queue.textContent = `另有 ${this._queue.length} 条模拟预警待知晓`;
        this._modal.hidden = false;
        document.getElementById('btnAcknowledge').focus();
        AudioUtils.startAlarm(); AudioUtils.speak('有新的模拟预警，请查看');
    },
    async _dismiss() {
        if (!this._current || this._busy) return;
        const event = this._current;
        const button = document.getElementById('btnAcknowledge');
        this._busy = true; button.disabled = true;
        try {
            const result = await API.acknowledgeAlert(event.eventId);
            if (!result.success) showToast(result.reason || '预警已经更新');
            else Audit.log('知晓模拟预警', event.name + ' · ' + event.eventId);
            this._close();
        } catch (error) {
            showToast(error.message || '预警确认失败，请重试');
        } finally {
            this._busy = false; button.disabled = false;
        }
        if (!this._current) this._next();
        if (window.AdminApp?.refreshSummary) window.AdminApp.refreshSummary();
    },
    _close() {
        this._current = null; this._modal.hidden = true;
        AudioUtils.stopAlarm(); AudioUtils.stopSpeak();
    }
};
