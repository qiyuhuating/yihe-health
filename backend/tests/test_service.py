import io
import json
import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from backend.service import CareApplication, CareStore, stamp


class CareTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.now = 1800000000
        self.store = CareStore(Path(self.tmp.name)/'care.sqlite3', clock=lambda: self.now)
        self.store.add_user('staff-account', 'staff', 'staff-1', '工作人员一', 'test-password-123')
        self.store.add_user('second-staff', 'staff', 'staff-2', '工作人员二', 'test-password-456')
        for rid in [1, 2]: self.store.add_resident({'id': rid, 'name': '合成居民', 'age': 70, 'responsible': 'staff-1'})
        self.store.add_user('resident-account', 'resident', 1, '居民账户', 'test-password-789')
        self.app = CareApplication(self.store, 'http://localhost:8000', secure=False, device_key='test-device-key')
        self.staff = self.login('staff-account', 'staff', 'test-password-123')
        self.resident = self.login('resident-account', 'resident', 'test-password-789')

    def call(self, method, path, body=None, client=None, extra=None):
        payload = json.dumps(body or {}).encode(); result = {}
        env = {'REQUEST_METHOD': method, 'PATH_INFO': path, 'CONTENT_LENGTH': str(len(payload)), 'wsgi.input': io.BytesIO(payload), 'REMOTE_ADDR': '127.0.0.1', 'HTTP_ORIGIN': 'http://localhost:8000'}
        if client: env.update(HTTP_COOKIE=client.get('cookie', ''), HTTP_X_CSRF_TOKEN=client.get('csrf', ''))
        env.update(extra or {})
        def start(status, headers): result.update(status=int(status.split()[0]), headers=dict(headers))
        data = b''.join(self.app(env, start)); result['data'] = json.loads(data) if data else None
        return result

    def login(self, username, role, password):
        anon = self.call('GET', '/api/v1/session')
        client = {'cookie': anon['headers']['Set-Cookie'].split(';')[0], 'csrf': anon['data']['csrfToken']}
        res = self.call('POST', '/api/v1/session', {'username': username, 'role': role, 'password': password}, client)
        self.assertEqual(res['status'], 200)
        return {'cookie': res['headers']['Set-Cookie'].split(';')[0], 'csrf': res['data']['csrfToken']}

    def snapshot(self, client=None): return self.call('GET', '/api/v1/care/snapshot', client=client or self.staff)['data']

    def device(self, active=True, severity='danger', key='device-1'):
        return self.call('POST', '/api/v1/device/events', {'residentId': 1, 'type': 'health', 'active': active, 'severity': severity, 'detail': '合成危险信号', 'metrics': {'bloodOxygen': 80}, 'metricStates': {'bloodOxygen': 'danger'}}, extra={'HTTP_AUTHORIZATION': 'Bearer test-device-key', 'HTTP_IDEMPOTENCY_KEY': key})

    def event(self): return next(e for e in self.snapshot()['events'] if e['residentId'] == 1 and e['type'] == 'health' and e['state'] not in {'resolved', 'false_alarm'})

    def transition(self, e, action, client=None, note='已核实合成信号'):
        return self.call('POST', '/api/v1/events/'+e['id']+'/transitions', {'action': action, 'note': note, 'revision': e['revision']}, client or self.staff)

    def test_danger_closed_while_active_creates_linked_incident_and_trail(self):
        self.assertEqual(self.device()['status'], 204)
        for action in ['claim', 'processing', 'false_alarm']: self.assertEqual(self.transition(self.event(), action)['status'], 204)
        s = self.snapshot(); events = [e for e in s['events'] if e['type'] == 'health']
        self.assertEqual(len(events), 2); current = self.event()
        old = next(e for e in events if e['state'] == 'false_alarm')
        self.assertEqual(current['previousIncidentId'], old['id']); self.assertEqual(current['notification']['cycle'], 1)
        self.assertEqual(len(old['history']), 4); self.assertTrue(s['logs'])
        with self.store.connect() as db: self.assertEqual(db.execute("SELECT COUNT(*) FROM notification_outbox WHERE channel='sms' AND status='disabled'").fetchone()[0], 2)

    def test_recovered_open_signal_recurrence_raises_cycle(self):
        self.device(); e = self.event(); self.transition(e, 'claim')
        self.device(False, key='recovery'); self.device(key='recurrence')
        e = self.event(); self.assertEqual(e['notification']['cycle'], 2); self.assertEqual(e['notification']['acknowledgedCycle'], 1)

    def test_stale_presented_batch_rolls_back(self):
        self.device(severity='warning'); old = self.event(); self.device(key='worsen')
        r = self.call('POST', '/api/v1/events/presented', {'events': [{'id': old['id'], 'cycle': 1}]}, self.staff)
        self.assertEqual(r['status'], 409); self.assertEqual(self.event()['notification']['presentedCycle'], 0)

    def test_concurrent_claim_has_one_winner(self):
        self.device(); e = self.event(); other = self.login('second-staff', 'staff', 'test-password-456')
        with ThreadPoolExecutor(2) as pool: results = list(pool.map(lambda c: self.transition(e, 'claim', c)['status'], [self.staff, other]))
        self.assertEqual(sorted(results), [204, 409])

    def test_concurrent_resident_updates_have_one_winner(self):
        p = self.snapshot()['residents']['1']
        with ThreadPoolExecutor(2) as pool:
            result = list(pool.map(lambda age: self.call('PATCH', '/api/v1/residents/1', {'age': age, 'expectedRevision': p['revision']}, self.staff)['status'], [71, 72]))
        self.assertEqual(sorted(result), [204, 409])

    def test_contact_patch_preserves_other_contact(self):
        p = self.snapshot()['residents']['1']; contacts = [{'name': '甲', 'phone': '12345'}, {'name': '乙', 'phone': '54321'}]
        self.call('PATCH', '/api/v1/residents/1', {'contacts': contacts, 'expectedRevision': p['revision']}, self.staff)
        p = self.snapshot()['residents']['1']
        self.assertEqual(self.call('PATCH', '/api/v1/residents/1', {'contactUpdates': [{'index': 0, 'value': {'name': '丙', 'phone': '67890'}}], 'expectedRevision': p['revision']}, self.staff)['status'], 204)
        self.assertEqual(self.snapshot()['residents']['1']['contacts'][1]['name'], '乙')

    def test_rename_does_not_change_login_identity(self):
        p = self.snapshot()['residents']['1']; self.call('PATCH', '/api/v1/residents/1', {'name': '新名字', 'expectedRevision': p['revision']}, self.resident)
        self.assertTrue(self.login('resident-account', 'resident', 'test-password-789'))

    def test_resident_cannot_access_other_resident_or_staff_mutations(self):
        self.assertEqual(set(self.snapshot(self.resident)['residents']), {'1'})
        self.assertEqual(self.call('PATCH', '/api/v1/residents/2', {'age': 71, 'expectedRevision': 0}, self.resident)['status'], 403)
        self.device(); self.assertEqual(self.transition(self.event(), 'claim', self.resident)['status'], 403)
        self.assertEqual(self.call('GET', '/api/v1/audit', client=self.resident)['status'], 403)

    def test_duplicate_doses_replay_and_changed_payload_conflicts(self):
        body = {'name': '合成用药任务', 'dueAt': stamp(self.now + 3600)}; extra = {'HTTP_IDEMPOTENCY_KEY': 'dose-key'}
        for _ in range(2): self.assertEqual(self.call('POST', '/api/v1/residents/1/doses', body, self.staff, extra)['status'], 204)
        self.assertEqual(len(self.snapshot()['residents']['1']['doses']), 1)
        self.assertEqual(self.call('POST', '/api/v1/residents/1/doses', {**body, 'name': '不同内容'}, self.staff, extra)['status'], 409)

    def test_confirm_window_and_server_overdue_detection(self):
        body = {'name': '合成用药任务', 'dueAt': stamp(self.now + 3600)}
        self.call('POST', '/api/v1/residents/1/doses', body, self.staff, {'HTTP_IDEMPOTENCY_KEY': 'dose-key'})
        dose = self.snapshot()['residents']['1']['doses'][0]; path = '/api/v1/residents/1/doses/'+dose['id']+'/confirm'
        self.assertEqual(self.call('POST', path, {}, self.resident)['status'], 422)
        self.now += 5401; self.staff=self.login('staff-account', 'staff', 'test-password-123'); self.assertTrue(any(e['type'] == 'medication' for e in self.snapshot()['events']))
        self.assertEqual(self.call('POST', path, {}, self.resident)['status'], 204)
        self.assertEqual(self.call('POST', path, {}, self.resident)['status'], 204)

    def test_session_expiry_logout_csrf_and_origin(self):
        self.assertEqual(self.call('PATCH', '/api/v1/residents/1', {}, self.staff, {'HTTP_X_CSRF_TOKEN': 'forged'})['status'], 403)
        self.assertEqual(self.call('PATCH', '/api/v1/residents/1', {}, self.staff, {'HTTP_ORIGIN': 'https://evil.example'})['status'], 403)
        self.assertEqual(self.call('DELETE', '/api/v1/session', client=self.staff)['status'], 204)
        self.assertEqual(self.call('GET', '/api/v1/care/snapshot', client=self.staff)['status'], 401)
        self.now += 86401; self.assertEqual(self.call('GET', '/api/v1/care/snapshot', client=self.resident)['status'], 401)

    def test_device_requires_secret_and_idempotency(self):
        self.assertEqual(self.call('POST', '/api/v1/device/events', {})['status'], 401)
        self.assertEqual(self.device()['status'], 204); self.assertEqual(self.device()['status'], 204)
        self.assertEqual(len(self.snapshot()['events']), 1)

    def test_validation_rolls_back_without_log(self):
        p = self.snapshot()['residents']['1']; logs = len(self.snapshot()['logs'])
        r = self.call('PATCH', '/api/v1/residents/1', {'name': '改名', 'age': 200, 'expectedRevision': p['revision']}, self.staff)
        self.assertEqual(r['status'], 422); self.assertEqual(self.snapshot()['residents']['1']['name'], p['name']); self.assertEqual(len(self.snapshot()['logs']), logs)

    def test_login_rate_limit_and_wrong_role(self):
        anon = self.call('GET', '/api/v1/session'); c = {'cookie': anon['headers']['Set-Cookie'].split(';')[0], 'csrf': anon['data']['csrfToken']}
        for _ in range(5): self.assertEqual(self.call('POST', '/api/v1/session', {'username': 'staff-account', 'password': 'bad', 'role': 'resident'}, c)['status'], 401)
        self.assertEqual(self.call('POST', '/api/v1/session', {'username': 'staff-account', 'password': 'test-password-123', 'role': 'staff'}, c)['status'], 429)


if __name__ == '__main__': unittest.main()
