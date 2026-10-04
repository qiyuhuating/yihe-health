"""Regression tests for the v0.2.0 backend changes (stdlib only)."""
import io, json, os, sys, tempfile, time, unittest, uuid
import contextlib
from http.cookies import SimpleCookie
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))
import service


class Clock:
    def __init__(self): self.t = 1_790_000_000.0
    def __call__(self): return self.t
    def advance(self, seconds): self.t += seconds


class Client:
    def __init__(self, app, addr='127.0.0.1', forwarded=None):
        self.app, self.addr, self.forwarded, self.cookie, self.csrf = app, addr, forwarded, '', ''
    def call(self, method, path, body=None, headers=None):
        raw = json.dumps(body).encode() if body is not None else b''
        env = {'REQUEST_METHOD': method, 'PATH_INFO': path, 'QUERY_STRING': '', 'CONTENT_LENGTH': str(len(raw)),
               'wsgi.input': io.BytesIO(raw), 'REMOTE_ADDR': self.addr, 'HTTP_ORIGIN': 'http://127.0.0.1:8000'}
        if self.forwarded: env['HTTP_X_FORWARDED_FOR'] = self.forwarded
        if self.cookie: env['HTTP_COOKIE'] = 'yihe_session=' + self.cookie
        if self.csrf: env['HTTP_X_CSRF_TOKEN'] = self.csrf
        for key, value in (headers or {}).items(): env['HTTP_' + key.upper().replace('-', '_')] = value
        result = {}
        def start(status, response_headers): result['status'], result['headers'] = int(status.split()[0]), response_headers
        data = b''.join(self.app(env, start))
        for key, value in result['headers']:
            if key == 'Set-Cookie':
                cookie = SimpleCookie(); cookie.load(value)
                if 'yihe_session' in cookie: self.cookie = cookie['yihe_session'].value
        parsed = json.loads(data) if data else None
        if isinstance(parsed, dict) and 'csrfToken' in parsed: self.csrf = parsed['csrfToken']
        return result['status'], parsed
    def login(self, role, username, password):
        self.call('GET', '/api/v1/session')
        return self.call('POST', '/api/v1/session', {'role': role, 'username': username, 'password': password})
    def snapshot(self): return self.call('GET', '/api/v1/care/snapshot')[1]


class BackendCase(unittest.TestCase):
    def setUp(self):
        self.clock = Clock()
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.store = service.CareStore(os.path.join(directory.name, 'c.sqlite3'), clock=self.clock)
        self.store.add_user('nurse1', 'staff', 'staff-1', '护士甲', 'correct-horse-1')
        self.store.add_user('nurse2', 'staff', 'staff-2', '护士乙', 'correct-horse-2')
        self.store.add_resident({'id': 1, 'name': '合成居民', 'age': 70, 'responsible': 'staff-1'})
        self.store.add_user('res1', 'resident', '1', '居民一', 'correct-horse-3')
        self.app = self.make_app()
    def make_app(self, **options):
        return service.CareApplication(self.store, 'http://127.0.0.1:8000', None, secure=False, device_key='devkey', **options)
    def danger(self, key='k1'):
        client = Client(self.app)
        body = {'residentId': 1, 'type': 'health', 'source': 'sim', 'active': True, 'severity': 'danger', 'detail': '合成危险信号', 'metrics': {'bloodOxygen': 80}, 'metricStates': {'bloodOxygen': 'danger'}}
        return client.call('POST', '/api/v1/device/events', body, {'Authorization': 'Bearer devkey', 'Idempotency-Key': key})[0]
    def transition(self, client, action, note='', target=None):
        event = client.snapshot()['events'][0]
        body = {'action': action, 'note': note, 'revision': event['revision']}
        if target: body['targetStaffId'] = target
        return client.call('POST', f"/api/v1/events/{event['id']}/transitions", body)[0]


class StaffSession(BackendCase):
    def test_default_staff_session_outlives_fifteen_minutes(self):
        client = Client(self.app); self.assertEqual(client.login('staff', 'nurse1', 'correct-horse-1')[0], 200)
        self.clock.advance(3 * 3600); self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 200)
        self.clock.advance(8 * 3600); self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 200)
        self.clock.advance(3600 + 60); self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 401)

    def test_ttl_is_configurable_and_bounded(self):
        app = self.make_app(staff_session_seconds=900); client = Client(app); client.login('staff', 'nurse1', 'correct-horse-1')
        self.clock.advance(901); self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 401)
        with self.assertRaises(service.Problem): self.make_app(staff_session_seconds=60)


class Escalation(BackendCase):
    def test_new_owner_is_alerted_and_old_owner_is_not_required_to_act(self):
        self.danger(); a = Client(self.app); a.login('staff', 'nurse1', 'correct-horse-1')
        self.assertEqual([self.transition(a, 'claim'), self.transition(a, 'processing'), self.transition(a, 'escalated', '需要医生复核', 'staff-2')], [204, 204, 204])
        b = Client(self.app); b.login('staff', 'nurse2', 'correct-horse-2'); note = b.snapshot()['events'][0]['notification']
        self.assertGreater(note['cycle'], note['acknowledgedCycle'], 'the recipient of an escalation must see a new notification cycle')
        self.assertEqual(self.transition(b, 'processing'), 204)
        note = b.snapshot()['events'][0]['notification']
        self.assertEqual(note['acknowledgedCycle'], note['cycle'], 'starting work acknowledges the cycle so it does not pop up again after a reload')


class ResidentPrivacy(BackendCase):
    def test_resident_snapshot_hides_notes_actors_and_audit_log(self):
        self.danger(); a = Client(self.app); a.login('staff', 'nurse1', 'correct-horse-1')
        self.transition(a, 'claim'); self.transition(a, 'processing'); self.transition(a, 'escalated', '内部备注：需要医生复核', 'staff-2')
        staff_view = a.snapshot(); self.assertTrue(any(h['note'] for h in staff_view['events'][0]['history'])); self.assertTrue(staff_view['logs'])
        resident = Client(self.app); resident.login('resident', 'res1', 'correct-horse-3'); view = resident.snapshot()
        history = view['events'][0]['history']
        self.assertTrue(history and all(h['note'] == '' for h in history)); self.assertNotIn('nurse1', json.dumps(view)); self.assertEqual(view['logs'], [])


class LoginLimiter(BackendCase):
    def fail_five(self, forwarded):
        client = Client(self.app, forwarded=forwarded)
        for i in range(5): client.login('staff', 'nurse1', f'wrong-{i}')
    def test_behind_a_proxy_one_client_cannot_lock_out_everyone(self):
        app = self.make_app(trusted_proxy=True); self.app = app
        self.fail_five('203.0.113.9')
        legit = Client(app, forwarded='198.51.100.7'); self.assertEqual(legit.login('staff', 'nurse1', 'correct-horse-1')[0], 200)
        attacker = Client(app, forwarded='203.0.113.9'); self.assertEqual(attacker.login('staff', 'nurse1', 'correct-horse-1')[0], 429)
    def test_client_supplied_forwarded_for_is_not_trusted_without_the_flag(self):
        self.fail_five('203.0.113.9'); self.assertEqual(Client(self.app, forwarded='198.51.100.7').login('staff', 'nurse1', 'correct-horse-1')[0], 429)
    def test_spoofed_leading_hop_cannot_evade_the_limit(self):
        app = self.make_app(trusted_proxy=True); self.app = app
        for i in range(5): Client(app, forwarded=f'1.1.1.{i}, 203.0.113.9').login('staff', 'nurse1', f'wrong-{i}')
        self.assertEqual(Client(app, forwarded='9.9.9.9, 203.0.113.9').login('staff', 'nurse1', 'correct-horse-1')[0], 429)


class Scale(BackendCase):
    def test_snapshot_cost_stays_bounded_with_a_year_of_history(self):
        with self.store.transaction() as db:
            for i in range(4000):
                eid = str(uuid.uuid4())
                event = {'id': eid, 'residentId': 1, 'signalId': '1:health:x', 'type': 'health', 'source': 'x', 'severity': 'warning', 'detail': 'd', 'state': 'resolved', 'owner': 'staff-1', 'revision': 3, 'createdAt': '2026-01-01T00:00:00Z', 'dueAt': '2026-01-01T01:00:00Z', 'notification': {'cycle': 1, 'deliveredCycle': 1, 'presentedCycle': 1, 'acknowledgedCycle': 1}}
                db.execute('INSERT INTO care_events VALUES(?,?,?)', (eid, 1, json.dumps(event)))
                for _ in range(5): db.execute('INSERT INTO event_history(event_id,data) VALUES(?,?)', (eid, json.dumps({'at': '2026-01-01T00:00:00Z', 'actor': 'x', 'action': 'a', 'note': 'n', 'state': 'resolved', 'owner': 'staff-1'})))
            for i in range(3000): db.execute('INSERT INTO medication_tasks VALUES(?,?,?)', (str(uuid.uuid4()), 1, json.dumps({'id': str(i), 'name': '药', 'dueAt': '2026-01-01T00:00:00Z', 'confirmedAt': '2026-01-01T00:01:00Z'})))
        client = Client(self.app); client.login('staff', 'nurse1', 'correct-horse-1')
        started = time.perf_counter(); view = client.snapshot(); elapsed = time.perf_counter() - started
        self.assertEqual(len(view['events']), 200); self.assertLessEqual(len(view['residents']['1']['doses']), 200)
        self.assertLess(elapsed, 0.25, f'snapshot took {elapsed:.3f}s')

    def test_confirming_a_dose_still_recovers_its_overdue_signal(self):
        client = Client(self.app); client.login('staff', 'nurse1', 'correct-horse-1')
        due = service.stamp(self.clock() + 120)
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', {'name': '降压药', 'dueAt': due}, {'Idempotency-Key': 'dose-1'})[0], 204)
        self.clock.advance(120 + 1801); self.store.tick(); view = client.snapshot(); self.assertTrue(any(e['type'] == 'medication' for e in view['events']))
        dose = view['residents']['1']['doses'][0]
        resident = Client(self.app); resident.login('resident', 'res1', 'correct-horse-3')
        self.assertEqual(resident.call('POST', f"/api/v1/residents/1/doses/{dose['id']}/confirm", {})[0], 204)
        with contextlib.closing(self.store.connect()) as db:
            conditions = [json.loads(r['data']) for r in db.execute('SELECT data FROM signal_conditions')]
        self.assertFalse(any(c['type'] == 'medication' and c['active'] for c in conditions))


if __name__ == '__main__':
    unittest.main()
