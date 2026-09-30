"""Single-community care API. SQLite owns sessions, signals, incidents and audit.
No clinical thresholds: authenticated device inputs carry reviewed severity.
"""
import argparse
import contextlib
import datetime as dt
import getpass
import hashlib
import hmac
import json
import os
import re
import secrets
import sqlite3
import threading
import time
import uuid
from http.cookies import SimpleCookie
from pathlib import Path
from urllib.parse import unquote
from wsgiref.simple_server import make_server, WSGIRequestHandler, WSGIServer
from socketserver import ThreadingMixIn

TYPES = {'health', 'offline', 'fence', 'medication'}
CLOSED = {'resolved', 'false_alarm'}


def stamp(epoch):
    return dt.datetime.fromtimestamp(epoch, dt.timezone.utc).isoformat().replace('+00:00', 'Z')


def epoch(value):
    try:
        if not isinstance(value, str) or not re.search(r'(Z|[+-]\d{2}:\d{2})$', value): raise ValueError()
        return dt.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()
    except (ValueError, TypeError, OverflowError):
        raise Problem(422, 'INVALID_TIME')


class Problem(Exception):
    def __init__(self, status, code):
        self.status, self.code = status, code


def require(ok, status=422, code='VALIDATION_FAILED'):
    if not ok: raise Problem(status, code)


def string(value, limit):
    require(isinstance(value, str) and 0 < len(value.strip()) <= limit)
    return value.strip()


def integer(value):
    return type(value) is int and 0 <= value <= 9007199254740991


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def encode(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'), allow_nan=False)


SCHEMA = '''
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value INTEGER NOT NULL);
INSERT OR IGNORE INTO settings VALUES('revision',0);
CREATE TABLE IF NOT EXISTS users(username TEXT PRIMARY KEY,role TEXT NOT NULL,identity TEXT NOT NULL,name TEXT NOT NULL,salt TEXT NOT NULL,password_hash TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS users_identity ON users(role,identity);
CREATE TABLE IF NOT EXISTS residents(id INTEGER PRIMARY KEY,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS signal_conditions(id TEXT PRIMARY KEY,resident_id INTEGER NOT NULL,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS care_events(id TEXT PRIMARY KEY,resident_id INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_resident ON care_events(resident_id);
CREATE TABLE IF NOT EXISTS event_history(id INTEGER PRIMARY KEY,event_id TEXT NOT NULL,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS medication_tasks(id TEXT PRIMARY KEY,resident_id INTEGER NOT NULL,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audit_logs(id INTEGER PRIMARY KEY,resident_id INTEGER NOT NULL,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,username TEXT,csrf TEXT NOT NULL,expires REAL NOT NULL);
CREATE TABLE IF NOT EXISTS idempotency(subject TEXT NOT NULL,key TEXT NOT NULL,request TEXT NOT NULL,PRIMARY KEY(subject,key));
CREATE TABLE IF NOT EXISTS login_limits(subject TEXT PRIMARY KEY,failures INTEGER NOT NULL,until REAL NOT NULL);
CREATE TABLE IF NOT EXISTS notification_outbox(id INTEGER PRIMARY KEY,event_id TEXT NOT NULL,cycle INTEGER NOT NULL,channel TEXT NOT NULL,status TEXT NOT NULL,UNIQUE(event_id,cycle,channel));
CREATE TABLE IF NOT EXISTS inquiries(id INTEGER PRIMARY KEY,data TEXT NOT NULL);
'''


class FakeNotificationService:
    """Page delivery exists in snapshots; SMS is explicitly disabled, never 'sent'."""
    def enqueue(self, db, event):
        cycle = event['notification']['cycle']
        for channel, status in [('page', 'available'), ('sms', 'disabled')]:
            db.execute('INSERT OR IGNORE INTO notification_outbox(event_id,cycle,channel,status) VALUES(?,?,?,?)', (event['id'], cycle, channel, status))


class CareStore:
    def __init__(self, filename, clock=time.time, notifications=None):
        self.filename, self.clock = str(filename), clock
        self.notifications = notifications or FakeNotificationService()
        with self.connect() as db: db.executescript(SCHEMA)

    def connect(self):
        db = sqlite3.connect(self.filename, timeout=10, isolation_level=None)
        db.row_factory = sqlite3.Row
        return db

    @contextlib.contextmanager
    def transaction(self):
        db = self.connect()
        try:
            db.execute('BEGIN IMMEDIATE')
            yield db
            db.commit()
        except BaseException:
            db.rollback(); raise
        finally: db.close()

    def revision(self, db):
        db.execute("UPDATE settings SET value=value+1 WHERE key='revision'")

    def resident(self, db, rid):
        row = db.execute('SELECT data FROM residents WHERE id=?', (rid,)).fetchone()
        require(row is not None, 404, 'NOT_FOUND')
        return json.loads(row['data'])

    def save(self, db, table, identity, value, rid=None):
        if table == 'residents':
            db.execute('INSERT INTO residents VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data', (identity, encode(value)))
        else:
            require(table in {'care_events', 'signal_conditions', 'medication_tasks'})
            db.execute(f'INSERT INTO {table} VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data', (identity, rid, encode(value)))

    def audit(self, db, rid, actor, action, **extra):
        value = {'at': stamp(self.clock()), 'residentId': rid, 'actor': actor, 'action': action, **extra}
        db.execute('INSERT INTO audit_logs(resident_id,data) VALUES(?,?)', (rid, encode(value)))

    def history(self, db, event, actor, action, note=''):
        value = {'at': stamp(self.clock()), 'actor': actor, 'action': action, 'note': note, 'state': event['state'], 'owner': event['owner']}
        db.execute('INSERT INTO event_history(event_id,data) VALUES(?,?)', (event['id'], encode(value)))

    def add_user(self, username, role, identity, name, password):
        require(role in {'staff', 'resident'} and isinstance(password, str) and len(password) >= 12)
        salt = secrets.token_hex(16)
        hashed = hashlib.scrypt(password.encode(), salt=bytes.fromhex(salt), n=16384, r=8, p=1).hex()
        with self.transaction() as db:
            if role == 'resident': self.resident(db, int(identity))
            db.execute('INSERT INTO users VALUES(?,?,?,?,?,?)', (string(username, 80), role, str(identity), string(name, 80), salt, hashed))

    def add_resident(self, value):
        require(integer(value.get('id')) and value['id'] > 0)
        with self.transaction() as db:
            require(db.execute('SELECT 1 FROM residents WHERE id=?', (value['id'],)).fetchone() is None, 409, 'CONFLICT')
            require(db.execute("SELECT 1 FROM users WHERE role='staff' AND identity=?", (value['responsible'],)).fetchone() is not None)
            now = stamp(self.clock())
            p = {'contacts': [], 'trend': [], 'pos': {'x': 150, 'y': 125}, 'risk': False, 'revision': 0, 'deviceState': 'unknown', 'lastSeen': now, 'updatedAt': now, 'locationAt': now, 'metricStates': {}, **value}
            p.pop('doses', None)
            require(type(p['age']) is int and 1 <= p['age'] <= 120)
            p['name'] = string(p['name'], 20)
            self.save(db, 'residents', p['id'], p); self.revision(db)

    def idempotent(self, db, subject, key, payload):
        require(isinstance(key, str) and 1 <= len(key) <= 128, 422, 'IDEMPOTENCY_REQUIRED')
        request = encode(payload)
        row = db.execute('SELECT request FROM idempotency WHERE subject=? AND key=?', (subject, key)).fetchone()
        if row:
            require(row['request'] == request, 409, 'CONFLICT'); return True
        db.execute('INSERT INTO idempotency VALUES(?,?,?)', (subject, key, request)); return False

    def signal(self, db, rid, kind, source, active, severity, detail):
        self.resident(db, rid)
        key = f'{rid}:{kind}:{source}'
        row = db.execute('SELECT data FROM signal_conditions WHERE id=?', (key,)).fetchone()
        prior = json.loads(row['data']) if row else None
        now = stamp(self.clock())
        event = None
        if prior:
            row = db.execute('SELECT data FROM care_events WHERE id=?', (prior['eventId'],)).fetchone()
            if row: event = json.loads(row['data'])
        if not active:
            if not prior or not prior['active']: return
            prior['active'] = False; prior['lastDetectedAt'] = now
            self.save(db, 'signal_conditions', key, prior, rid)
            if event and event['state'] not in CLOSED:
                event['sourceRecoveredAt'] = now; event['revision'] += 1
                self.history(db, event, 'system', '信号恢复，待人工复核')
                self.save(db, 'care_events', event['id'], event, rid)
            self.revision(db); return
        fresh = not event or event['state'] in CLOSED
        recurrent = prior is not None and not prior['active']
        worsening = event and severity == 'danger' and event['severity'] != 'danger'
        if fresh:
            event = {'id': str(uuid.uuid4()), 'residentId': rid, 'signalId': key, 'type': kind, 'source': source, 'severity': severity, 'detail': detail, 'state': 'new', 'owner': None, 'revision': 1, 'createdAt': now, 'dueAt': stamp(self.clock() + (900 if severity == 'danger' else 3600)), 'previousIncidentId': prior['eventId'] if prior else None, 'notification': {'cycle': 1, 'deliveredCycle': 1, 'presentedCycle': 0, 'acknowledgedCycle': 0}}
            self.history(db, event, 'system', '发现危险信号')
        else:
            if recurrent or worsening:
                event.pop('sourceRecoveredAt', None)
                event['notification']['cycle'] += 1
                event['notification']['deliveredCycle'] = event['notification']['cycle']
                if severity == 'danger': event['dueAt'] = stamp(min(epoch(event['dueAt']), self.clock() + 900))
                self.history(db, event, 'system', '信号再次异常或恶化，重新提醒')
            if event['detail'] != detail or recurrent or worsening: event['revision'] += 1
            event['detail'] = detail
            if severity == 'danger': event['severity'] = severity
        condition = {'id': key, 'residentId': rid, 'type': kind, 'active': True, 'severity': severity, 'detail': detail, 'firstDetectedAt': prior['firstDetectedAt'] if prior and prior['active'] else now, 'lastDetectedAt': now, 'generation': (prior['generation'] if prior else 0) + int(fresh or recurrent), 'eventId': event['id']}
        changed = fresh or recurrent or worsening or not prior or prior['detail'] != detail or prior['severity'] != severity
        self.save(db, 'care_events', event['id'], event, rid)
        self.save(db, 'signal_conditions', key, condition, rid)
        self.notifications.enqueue(db, event)
        if changed: self.revision(db)

    def tick(self, db=None):
        if db is None:
            with self.transaction() as conn: self.tick(conn)
            return
        for row in db.execute('SELECT id,data FROM residents').fetchall():
            p = json.loads(row['data']); offline = self.clock() - epoch(p['lastSeen']) > 300
            state = 'offline' if offline else p['deviceState']
            if p['deviceState'] != state:
                p['deviceState'] = state; self.save(db, 'residents', row['id'], p); self.revision(db)
            self.signal(db, row['id'], 'offline', 'heartbeat', offline, 'warning', '超过 5 分钟未收到设备心跳')
        for row in db.execute('SELECT id,resident_id,data FROM medication_tasks').fetchall():
            dose = json.loads(row['data'])
            self.signal(db, row['resident_id'], 'medication', row['id'], not dose.get('confirmedAt') and self.clock() > epoch(dose['dueAt']) + 1800, 'warning', dose['name'] + ' · 超过计划时间 30 分钟未确认')

    def snapshot(self, db, user):
        self.tick(db)
        staff = [{'id': row['identity'], 'name': row['name']} for row in db.execute("SELECT identity,name FROM users WHERE role='staff'")]
        rows = db.execute('SELECT id,data FROM residents' + (' WHERE id=?' if user['role'] == 'resident' else ''), (user['residentId'],) if user['role'] == 'resident' else ()).fetchall()
        residents = {str(row['id']): json.loads(row['data']) for row in rows}
        for p in residents.values():
            all_doses=[json.loads(row['data']) for row in db.execute('SELECT data FROM medication_tasks WHERE resident_id=? ORDER BY rowid', (p['id'],))]
            done={d['id'] for d in [d for d in all_doses if d.get('confirmedAt')][-200:]}
            p['doses']=[d for d in all_doses if not d.get('confirmedAt') or d['id'] in done]
        events = []
        for row in db.execute('SELECT data FROM care_events ORDER BY rowid DESC'):
            e = json.loads(row['data'])
            if str(e['residentId']) not in residents: continue
            if e['state'] in CLOSED and sum(x['state'] in CLOSED for x in events) >= 200: continue
            e['history'] = [json.loads(h['data']) for h in db.execute('SELECT data FROM event_history WHERE event_id=? ORDER BY id DESC LIMIT 100', (e['id'],))][::-1]
            events.append(e)
        logs = [json.loads(row['data']) for row in db.execute('SELECT data,resident_id FROM audit_logs ORDER BY id DESC LIMIT 500') if str(row['resident_id']) in residents][::-1]
        return {'revision': db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()[0], 'serverTime': stamp(self.clock()), 'staff': staff, 'residents': residents, 'events': events, 'logs': logs}

    def authorize_resident(self, user, rid):
        require(user['role'] == 'staff' or user.get('residentId') == rid, 403, 'FORBIDDEN')

    def patch_resident(self, db, rid, body, user):
        self.authorize_resident(user, rid); p = self.resident(db, rid)
        require(set(body) <= {'expectedRevision', 'name', 'age', 'contacts', 'contactUpdates', 'responsible'})
        require(integer(body.get('expectedRevision')))
        require(body['expectedRevision'] == p['revision'], 409, 'CONFLICT')
        require(not ('contacts' in body and 'contactUpdates' in body))
        if 'responsible' in body:
            require(user['role'] == 'staff', 403, 'FORBIDDEN')
            require(db.execute("SELECT 1 FROM users WHERE role='staff' AND identity=?", (body['responsible'],)).fetchone() is not None)
            p['responsible'] = body['responsible']
        if 'name' in body: p['name'] = string(body['name'], 20)
        if 'age' in body:
            require(type(body['age']) is int and 1 <= body['age'] <= 120); p['age'] = body['age']
        contacts = body.get('contacts', p['contacts'])
        if 'contactUpdates' in body:
            require(isinstance(body['contactUpdates'], list) and 1 <= len(body['contactUpdates']) <= 3)
            contacts = [dict(c) for c in contacts]
            for item in body['contactUpdates']:
                require(isinstance(item, dict) and set(item) == {'index', 'value'} and type(item['index']) is int and 0 <= item['index'] <= len(contacts) and item['index'] < 3)
                if item['index'] == len(contacts): contacts.append(item['value'])
                else: contacts[item['index']] = item['value']
        require(isinstance(contacts, list) and len(contacts) <= 3)
        for c in contacts:
            require(isinstance(c, dict) and set(c) == {'name', 'phone'})
            c['name'] = string(c['name'], 30); c['phone'] = string(c['phone'], 22)
            require(re.fullmatch(r'\+?[\d -]{5,22}', c['phone']) is not None)
        p['contacts'] = contacts; p['revision'] += 1; p['updatedAt'] = stamp(self.clock())
        self.save(db, 'residents', rid, p); self.revision(db); self.audit(db, rid, user['username'], '更新居民资料')

    def transition(self, db, eid, body, user):
        require(user['role'] == 'staff', 403, 'FORBIDDEN')
        require(set(body) <= {'action', 'note', 'revision', 'targetStaffId'})
        row = db.execute('SELECT data FROM care_events WHERE id=?', (eid,)).fetchone(); require(row, 404, 'NOT_FOUND')
        e = json.loads(row['data']); require(integer(body.get('revision')))
        require(e['revision'] == body['revision'], 409, 'CONFLICT'); require(e['state'] not in CLOSED, 409, 'CONFLICT')
        action, actor = body.get('action'), user['staffId']
        note = body.get('note', ''); require(isinstance(note, str) and len(note) <= 2000)
        if action == 'claim':
            require(e['state'] == 'new' and e['owner'] is None, 409, 'CONFLICT')
            e['owner'] = actor; e['state'] = 'claimed'; e['notification']['acknowledgedCycle'] = e['notification']['cycle']
        else:
            require(e['owner'] == actor, 403, 'FORBIDDEN')
            if action == 'processing':
                require(e['state'] in {'claimed', 'escalated'}, 409, 'CONFLICT'); e['state'] = 'processing'
            else:
                require(e['state'] == 'processing' and action in CLOSED | {'escalated'}, 409, 'CONFLICT'); require(isinstance(note,str) and bool(note.strip()),422,'NOTE_REQUIRED')
                if action == 'escalated':
                    target = body.get('targetStaffId'); require(target != actor and db.execute("SELECT 1 FROM users WHERE role='staff' AND identity=?", (target,)).fetchone() is not None)
                    e['owner'] = target
                else: e['closedAt'] = stamp(self.clock())
                e['state'] = action
        e['revision'] += 1; e['updatedAt'] = stamp(self.clock())
        self.history(db, e, user['staffId'], action, note); self.save(db, 'care_events', eid, e, e['residentId']); self.revision(db); self.audit(db, e['residentId'], user['username'], action, eventId=eid)
        if e['state'] in CLOSED:
            row = db.execute('SELECT data FROM signal_conditions WHERE id=?', (e['signalId'],)).fetchone()
            c = json.loads(row['data']) if row else None
            if c and c['active']: self.signal(db, e['residentId'], c['type'], e['source'], True, c['severity'], c['detail'])

    def doses(self, db, rid, body, user, key):
        require(user['role'] == 'staff', 403, 'FORBIDDEN'); self.resident(db, rid)
        require(set(body) == {'name', 'dueAt'}); name = string(body['name'], 100); due = epoch(body['dueAt'])
        if self.idempotent(db, user['username'] + f':doses:{rid}', key, body): return
        require(due >= self.clock() - 60)
        count = db.execute('SELECT COUNT(*) FROM medication_tasks WHERE resident_id=? AND json_extract(data,\'$.confirmedAt\') IS NULL', (rid,)).fetchone()[0]
        require(count < 500, 422, 'LIMIT_EXCEEDED')
        dose = {'id': str(uuid.uuid4()), 'name': name, 'dueAt': stamp(due), 'createdAt': stamp(self.clock()), 'source': '工作人员登记', 'actor': user['username']}
        self.save(db, 'medication_tasks', dose['id'], dose, rid); self.revision(db); self.audit(db, rid, user['username'], '登记用药任务')

    def presented(self, db, body, user):
        require(user['role'] == 'staff', 403, 'FORBIDDEN')
        require(set(body) == {'events'} and isinstance(body['events'], list) and len(body['events']) <= 500)
        targets = []
        for item in body['events']:
            require(isinstance(item, dict) and set(item) == {'id', 'cycle'} and isinstance(item['id'], str) and integer(item['cycle']) and item['cycle'] > 0)
            row = db.execute('SELECT data FROM care_events WHERE id=?', (item['id'],)).fetchone(); require(row, 404, 'NOT_FOUND')
            e = json.loads(row['data']); require(item['cycle'] == e['notification']['cycle'], 409, 'CONFLICT'); targets.append(e)
        for e in targets:
            if e['notification']['presentedCycle'] == e['notification']['cycle']: continue
            e['notification']['presentedCycle'] = e['notification']['cycle']
            self.save(db, 'care_events', e['id'], e, e['residentId']); self.history(db, e, user['staffId'], '页面提醒已展示'); self.revision(db)


class CareApplication:
    def __init__(self, store, origin, static_root=None, secure=True, device_key=''):
        require(origin.startswith('https://') or not secure)
        self.store, self.origin, self.secure, self.device_key = store, origin.rstrip('/'), secure, device_key
        self.static_root = Path(static_root).resolve() if static_root else None

    def session(self, db, cookie):
        row = db.execute('SELECT * FROM sessions WHERE id=? AND expires>?', (digest(cookie), self.store.clock())).fetchone() if cookie else None
        return row

    def new_session(self, db, username=None):
        db.execute('DELETE FROM sessions WHERE expires<=?', (self.store.clock(),))
        token, csrf = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
        role = db.execute('SELECT role FROM users WHERE username=?', (username,)).fetchone() if username else None
        ttl = 900 if role and role['role'] == 'staff' else 86400 if username else 3600
        db.execute('INSERT INTO sessions VALUES(?,?,?,?)', (digest(token), username, csrf, self.store.clock() + ttl))
        return token, csrf

    def user(self, db, session):
        require(session and session['username'], 401, 'UNAUTHENTICATED')
        row = db.execute('SELECT username,role,identity,name FROM users WHERE username=?', (session['username'],)).fetchone()
        require(row, 401, 'UNAUTHENTICATED')
        return {'username': row['username'], 'role': row['role'], 'name': row['name'], **({'staffId': row['identity']} if row['role'] == 'staff' else {'residentId': int(row['identity'])})}

    def route(self, env, body, cookies, db):
        method, path = env['REQUEST_METHOD'], unquote(env['PATH_INFO'])
        sid = cookies['yihe_session'].value if 'yihe_session' in cookies else ''
        session = self.session(db, sid)
        response_cookie = None
        if path == '/api/v1/device/events' and method == 'POST':
            require(bool(self.device_key), 503, 'DEVICE_DISABLED')
            require(hmac.compare_digest(env.get('HTTP_AUTHORIZATION', ''), 'Bearer ' + self.device_key), 401, 'UNAUTHENTICATED')
            require(set(body) <= {'residentId', 'type', 'source', 'active', 'severity', 'detail', 'metrics', 'metricStates'})
            rid = body.get('residentId'); require(integer(rid) and rid > 0)
            kind = body.get('type'); require(kind in TYPES | {'heartbeat'})
            source = string(body.get('source', 'device'), 80)
            active = body.get('active', True); require(type(active) is bool)
            severity = body.get('severity', 'warning'); require(severity in {'warning', 'danger'})
            detail = string(body.get('detail', '设备信号待人工核实'), 500)
            if self.store.idempotent(db, 'device', env.get('HTTP_IDEMPOTENCY_KEY'), body): return 204, None, None
            p = self.store.resident(db, rid); now = stamp(self.store.clock())
            metrics = body.get('metrics', {}); states = body.get('metricStates', {})
            keys = {'heartRate', 'bloodOxygen', 'temperature', 'systolic', 'diastolic', 'bloodSugar'}
            require(isinstance(metrics, dict) and set(metrics) <= keys and all(type(v) in {int, float} and abs(v) < 1e6 for v in metrics.values()))
            require(isinstance(states, dict) and set(states) <= keys and all(v in {'normal', 'warning', 'danger', 'unknown'} for v in states.values()))
            p.update(metrics); p['metricStates'].update(states); p['lastSeen'] = now; p['updatedAt'] = now; p['deviceState'] = 'online'
            p['trend'] = (p['trend'] + [{'at': now, **metrics}])[-72:]
            self.store.save(db, 'residents', rid, p); self.store.revision(db)
            if kind != 'heartbeat': self.store.signal(db, rid, kind, source, active, severity, detail)
            self.store.signal(db, rid, 'offline', 'heartbeat', False, 'warning', '设备恢复心跳')
            self.store.audit(db, rid, 'device', '设备事件接入', source=source); return 204, None, None
        if method not in {'GET', 'HEAD'}:
            require(env.get('HTTP_ORIGIN') == self.origin, 403, 'ORIGIN_REJECTED')
            require(session is not None, 401, 'UNAUTHENTICATED')
            require(hmac.compare_digest(env.get('HTTP_X_CSRF_TOKEN', ''), session['csrf']), 403, 'CSRF_REJECTED')
        if path == '/api/v1/session':
            if method == 'GET':
                if not session:
                    token, csrf = self.new_session(db); response_cookie = token; user = None
                else:
                    csrf = session['csrf']; user = self.user(db, session) if session['username'] else None
                return 200, {'user': user, 'csrfToken': csrf}, response_cookie
            if method == 'POST':
                require(set(body) == {'role', 'username', 'password'} and body['role'] in {'staff', 'resident'})
                username = string(body['username'], 80); require(isinstance(body['password'], str) and len(body['password']) <= 256)
                subject = digest(env.get('REMOTE_ADDR', '') + ':' + username)
                limit = db.execute('SELECT * FROM login_limits WHERE subject=?', (subject,)).fetchone()
                require(not limit or limit['until'] <= self.store.clock(), 429, 'RATE_LIMITED')
                row = db.execute('SELECT * FROM users WHERE username=?', (username,)).fetchone()
                # Equal work for nonexistent users; role and account details are not disclosed.
                salt = bytes.fromhex(row['salt']) if row else bytes(16)
                hashed = hashlib.scrypt(body['password'].encode(), salt=salt, n=16384, r=8, p=1).hex()
                if not row or row['role'] != body['role'] or not hmac.compare_digest(hashed, row['password_hash']):
                    failures = (limit['failures'] if limit and limit['until'] > self.store.clock() - 60 else 0) + 1
                    db.execute('INSERT INTO login_limits VALUES(?,?,?) ON CONFLICT(subject) DO UPDATE SET failures=excluded.failures,until=excluded.until', (subject, failures, self.store.clock() + 60 if failures >= 5 else self.store.clock()))
                    db.commit(); raise Problem(401, 'UNAUTHENTICATED')
                db.execute('DELETE FROM login_limits WHERE subject=?', (subject,))
                db.execute('DELETE FROM sessions WHERE id=?', (digest(sid),))
                token, csrf = self.new_session(db, username)
                current = self.session(db, token); return 200, {'user': self.user(db, current), 'csrfToken': csrf}, token
            if method == 'DELETE':
                db.execute('DELETE FROM sessions WHERE id=?', (digest(sid),)); return 204, None, ''
            raise Problem(405, 'METHOD_NOT_ALLOWED')
        if path == '/api/v1/inquiries' and method == 'POST':
            require(set(body) == {'name', 'phone', 'type', 'message'})
            for key, maximum in [('name', 80), ('phone', 30), ('type', 80), ('message', 2000)]: string(body[key], maximum)
            if not self.store.idempotent(db, 'inquiry:' + digest(sid), env.get('HTTP_IDEMPOTENCY_KEY'), body):
                db.execute('INSERT INTO inquiries(data) VALUES(?)', (encode({'at': stamp(self.store.clock()), **body}),))
            return 204, None, None
        user = self.user(db, session)
        if path == '/api/v1/care/snapshot' and method == 'GET': return 200, self.store.snapshot(db, user), None
        if path == '/api/v1/audit' and method == 'GET':
            require(user['role'] == 'staff', 403, 'FORBIDDEN')
            from urllib.parse import parse_qs
            query = parse_qs(env.get('QUERY_STRING', ''))
            try: after = int(query.get('after', ['0'])[0]); limit = int(query.get('limit', ['100'])[0])
            except ValueError: raise Problem(422, 'VALIDATION_FAILED')
            require(after >= 0 and 1 <= limit <= 200)
            rows = db.execute('SELECT id,data FROM audit_logs WHERE id>? ORDER BY id LIMIT ?', (after, limit)).fetchall()
            return 200, {'items': [{'id': r['id'], **json.loads(r['data'])} for r in rows], 'next': rows[-1]['id'] if rows else after}, None
        if path == '/api/v1/events/presented' and method == 'POST':
            self.store.presented(db, body, user); return 204, None, None
        match = re.fullmatch(r'/api/v1/events/([^/]+)/transitions', path)
        if match and method == 'POST': self.store.transition(db, match[1], body, user); return 204, None, None
        match = re.fullmatch(r'/api/v1/residents/(\d+)', path)
        if match and method == 'PATCH': self.store.patch_resident(db, int(match[1]), body, user); return 204, None, None
        match = re.fullmatch(r'/api/v1/residents/(\d+)/doses', path)
        if match and method == 'POST': self.store.doses(db, int(match[1]), body, user, env.get('HTTP_IDEMPOTENCY_KEY')); return 204, None, None
        match = re.fullmatch(r'/api/v1/residents/(\d+)/doses/([^/]+)/confirm', path)
        if match and method == 'POST':
            rid, doseid = int(match[1]), match[2]; self.store.authorize_resident(user, rid); require(not body)
            row = db.execute('SELECT data FROM medication_tasks WHERE id=? AND resident_id=?', (doseid, rid)).fetchone(); require(row, 404, 'NOT_FOUND')
            dose = json.loads(row['data'])
            if not dose.get('confirmedAt'):
                require(self.store.clock() >= epoch(dose['dueAt']) - 1800)
                dose['confirmedAt'] = stamp(self.store.clock()); self.store.save(db, 'medication_tasks', doseid, dose, rid)
                self.store.revision(db); self.store.audit(db, rid, user['username'], '确认服药'); self.store.tick(db)
            return 204, None, None
        raise Problem(404, 'NOT_FOUND')

    def __call__(self, env, start_response):
        from http import HTTPStatus
        # WSGI transports URL path bytes through latin-1; recover UTF-8 file names.
        try: env={**env,'PATH_INFO':env['PATH_INFO'].encode('latin-1').decode('utf-8')}
        except (UnicodeError,KeyError): pass
        headers = [('Cache-Control', 'no-store'), ('X-Content-Type-Options', 'nosniff'), ('Referrer-Policy', 'no-referrer')]
        try:
            if not env['PATH_INFO'].startswith('/api/'):
                require(self.static_root is not None and env['REQUEST_METHOD'] in {'GET', 'HEAD'}, 404, 'NOT_FOUND')
                path = (self.static_root / unquote(env['PATH_INFO']).lstrip('/')).resolve()
                if env['PATH_INFO'] == '/': path = self.static_root / 'home.html'
                require(path.is_relative_to(self.static_root) and path.is_file() and not path.is_symlink(), 404, 'NOT_FOUND')
                import mimetypes
                data = path.read_bytes(); headers.append(('Content-Type', mimetypes.guess_type(str(path))[0] or 'application/octet-stream')); status = 200
            else:
                length = int(env.get('CONTENT_LENGTH') or 0); require(0 <= length <= 65536, 413, 'PAYLOAD_TOO_LARGE')
                raw = env['wsgi.input'].read(length) if length else b'{}'
                try: body = json.loads(raw, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                except (ValueError, UnicodeError): raise Problem(400, 'INVALID_JSON')
                require(isinstance(body, dict), 400, 'INVALID_JSON')
                cookie = SimpleCookie(); cookie.load(env.get('HTTP_COOKIE', ''))
                with self.store.transaction() as db: status, value, token = self.route(env, body, cookie, db)
                data = encode(value).encode() if value is not None else b''
                headers.append(('Content-Type', 'application/json; charset=utf-8'))
                if token is not None:
                    headers.append(('Set-Cookie', 'yihe_session=' + token + '; Path=/; HttpOnly; SameSite=Lax' + ('; Secure' if self.secure else '') + ('; Max-Age=0' if token == '' else '')))
        except Problem as e:
            status, data = e.status, encode({'error': {'code': e.code}}).encode(); headers.append(('Content-Type', 'application/json; charset=utf-8'))
        except (ValueError, TypeError, KeyError):
            status, data = 422, encode({'error': {'code': 'VALIDATION_FAILED'}}).encode(); headers.append(('Content-Type', 'application/json; charset=utf-8'))
        except Exception:
            status, data = 500, b'{"error":{"code":"SERVICE_UNAVAILABLE"}}'; headers.append(('Content-Type', 'application/json; charset=utf-8'))
        headers.append(('Content-Length', str(len(data))))
        start_response(f'{status} {HTTPStatus(status).phrase}', headers)
        return [b'' if env['REQUEST_METHOD'] == 'HEAD' else data]


class ThreadedServer(ThreadingMixIn, WSGIServer):
    daemon_threads = True


class QuietHandler(WSGIRequestHandler):
    def log_message(self, *_): pass  # No identifiers, tokens or health data in access logs.


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--db', default='care.sqlite3')
    sub = parser.add_subparsers(dest='command', required=True)
    sub.add_parser('init')
    user = sub.add_parser('add-user'); user.add_argument('username'); user.add_argument('role', choices=['staff', 'resident']); user.add_argument('identity'); user.add_argument('name')
    resident = sub.add_parser('add-resident'); resident.add_argument('json_file')
    serve = sub.add_parser('serve'); serve.add_argument('--port', type=int, default=8000); serve.add_argument('--origin', required=True); serve.add_argument('--insecure-local', action='store_true'); serve.add_argument('--static', default='docs/dist')
    args = parser.parse_args(); store = CareStore(args.db)
    if args.command == 'add-user': store.add_user(args.username, args.role, args.identity, args.name, getpass.getpass('Password (12+ characters): ')); return
    if args.command == 'add-resident': store.add_resident(json.loads(Path(args.json_file).read_text())); return
    if args.command == 'init': return
    require(not args.insecure_local or args.origin in {f'http://127.0.0.1:{args.port}', f'http://localhost:{args.port}'})
    app = CareApplication(store, args.origin, args.static, secure=not args.insecure_local, device_key=os.environ.get('YIHE_DEVICE_KEY', ''))
    stopped = threading.Event()
    def worker():
        while not stopped.wait(5):
            try: store.tick()
            except sqlite3.Error: pass  # Retry next tick; no partially committed state.
    thread = threading.Thread(target=worker, daemon=True); thread.start()
    server = make_server('127.0.0.1', args.port, app, server_class=ThreadedServer, handler_class=QuietHandler)
    try: server.serve_forever()
    except KeyboardInterrupt: pass
    finally: stopped.set(); server.server_close(); thread.join(timeout=6)


if __name__ == '__main__': main()
