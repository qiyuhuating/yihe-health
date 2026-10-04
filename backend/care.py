"""Care domain persistence and state transitions. No HTTP or session handling."""

import contextlib
import hashlib
import json
import re
import secrets
import sqlite3
import time
import uuid

try:
    from .common import CLOSED, encode, epoch, integer, require, stamp, string
    from .serializers import resident_snapshot
    from .incidents import apply_transition
    from .notifications import FakeNotificationService
except ImportError:
    from common import CLOSED, encode, epoch, integer, require, stamp, string
    from serializers import resident_snapshot
    from incidents import apply_transition
    from notifications import FakeNotificationService

SCHEMA = """
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
CREATE INDEX IF NOT EXISTS history_event ON event_history(event_id);
CREATE INDEX IF NOT EXISTS meds_resident ON medication_tasks(resident_id);
"""


class CareStore:
    def __init__(self, filename, clock=time.time, notifications=None):
        self.filename, self.clock = str(filename), clock
        self.notifications = notifications or FakeNotificationService()
        # Connection.__exit__ commits/rolls back; it does not close SQLite.
        with contextlib.closing(self.connect()) as db:
            db.executescript(SCHEMA)

    def connect(self):
        db = sqlite3.connect(self.filename, timeout=10, isolation_level=None)
        db.row_factory = sqlite3.Row
        return db

    @contextlib.contextmanager
    def read_transaction(self):
        """Open a consistent, SQLite-enforced read-only transaction."""
        db = self.connect()
        try:
            db.execute("PRAGMA query_only=ON")
            db.execute("BEGIN")
            yield db
            db.rollback()
        except BaseException:
            db.rollback()
            raise
        finally:
            db.close()

    @contextlib.contextmanager
    def write_transaction(self):
        """Serialize writes early so business conflicts fail inside one atomic unit."""
        db = self.connect()
        try:
            db.execute("BEGIN IMMEDIATE")
            yield db
            db.commit()
        except BaseException:
            db.rollback()
            raise
        finally:
            db.close()

    # Backwards-compatible alias for callers/tests written before v0.3.2.
    transaction = write_transaction

    def revision(self, db):
        db.execute("UPDATE settings SET value=value+1 WHERE key='revision'")

    def resident(self, db, rid):
        row = db.execute("SELECT data FROM residents WHERE id=?", (rid,)).fetchone()
        require(row is not None, 404, "NOT_FOUND")
        return json.loads(row["data"])

    def save(self, db, table, identity, value, rid=None):
        if table == "residents":
            db.execute(
                "INSERT INTO residents VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",
                (identity, encode(value)),
            )
        else:
            require(table in {"care_events", "signal_conditions", "medication_tasks"})
            db.execute(
                f"INSERT INTO {table} VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",
                (identity, rid, encode(value)),
            )

    def audit(self, db, rid, actor, action, **extra):
        value = {"at": stamp(self.clock()), "residentId": rid, "actor": actor, "action": action, **extra}
        db.execute("INSERT INTO audit_logs(resident_id,data) VALUES(?,?)", (rid, encode(value)))

    def history(self, db, event, actor, action, note=""):
        value = {
            "at": stamp(self.clock()),
            "actor": actor,
            "action": action,
            "note": note,
            "state": event["state"],
            "owner": event["owner"],
        }
        db.execute("INSERT INTO event_history(event_id,data) VALUES(?,?)", (event["id"], encode(value)))

    def add_user(self, username, role, identity, name, password):
        require(role in {"staff", "resident"} and isinstance(password, str) and len(password) >= 12)
        salt = secrets.token_hex(16)
        hashed = hashlib.scrypt(password.encode(), salt=bytes.fromhex(salt), n=16384, r=8, p=1).hex()
        with self.transaction() as db:
            if role == "resident":
                self.resident(db, int(identity))
            db.execute(
                "INSERT INTO users VALUES(?,?,?,?,?,?)",
                (string(username, 80), role, str(identity), string(name, 80), salt, hashed),
            )

    def add_resident(self, value):
        require(integer(value.get("id")) and value["id"] > 0)
        with self.transaction() as db:
            require(
                db.execute("SELECT 1 FROM residents WHERE id=?", (value["id"],)).fetchone() is None, 409, "CONFLICT"
            )
            require(
                db.execute("SELECT 1 FROM users WHERE role='staff' AND identity=?", (value["responsible"],)).fetchone()
                is not None
            )
            now = stamp(self.clock())
            p = {
                "contacts": [],
                "trend": [],
                "pos": {"x": 150, "y": 125},
                "risk": False,
                "revision": 0,
                "deviceState": "unknown",
                "lastSeen": now,
                "updatedAt": now,
                "locationAt": now,
                "metricStates": {},
                **value,
            }
            p.pop("doses", None)
            require(type(p["age"]) is int and 1 <= p["age"] <= 120)
            p["name"] = string(p["name"], 20)
            self.save(db, "residents", p["id"], p)
            self.revision(db)

    def idempotent(self, db, subject, key, payload):
        require(isinstance(key, str) and 1 <= len(key) <= 128, 422, "IDEMPOTENCY_REQUIRED")
        request = encode(payload)
        row = db.execute("SELECT request FROM idempotency WHERE subject=? AND key=?", (subject, key)).fetchone()
        if row:
            require(row["request"] == request, 409, "CONFLICT")
            return True
        db.execute("INSERT INTO idempotency VALUES(?,?,?)", (subject, key, request))
        return False

    def _new_signal_event(self, db, rid, key, kind, source, severity, detail, prior, now):
        event = {
            "id": str(uuid.uuid4()),
            "residentId": rid,
            "signalId": key,
            "type": kind,
            "source": source,
            "severity": severity,
            "detail": detail,
            "state": "new",
            "owner": None,
            "revision": 1,
            "createdAt": now,
            "dueAt": stamp(self.clock() + (900 if severity == "danger" else 3600)),
            "previousIncidentId": prior["eventId"] if prior else None,
            "notification": {"cycle": 1, "deliveredCycle": 1, "presentedCycle": 0, "acknowledgedCycle": 0},
        }
        self.history(db, event, "system", "发现危险信号")
        return event

    def _update_signal_event(self, db, event, severity, detail, recurrent, worsening):
        if recurrent or worsening:
            event.pop("sourceRecoveredAt", None)
            event["notification"]["cycle"] += 1
            event["notification"]["deliveredCycle"] = event["notification"]["cycle"]
            if severity == "danger":
                event["dueAt"] = stamp(min(epoch(event["dueAt"]), self.clock() + 900))
            self.history(db, event, "system", "信号再次异常或恶化，重新提醒")
        if event["detail"] != detail or recurrent or worsening:
            event["revision"] += 1
        event["detail"] = detail
        if severity == "danger":
            event["severity"] = severity

    def _recover_signal(self, db, rid, key, prior, event, now):
        if not prior or not prior["active"]:
            return
        prior["active"] = False
        prior["lastDetectedAt"] = now
        self.save(db, "signal_conditions", key, prior, rid)
        if event and event["state"] not in CLOSED:
            event["sourceRecoveredAt"] = now
            event["revision"] += 1
            self.history(db, event, "system", "信号恢复，待人工复核")
            self.save(db, "care_events", event["id"], event, rid)
        self.revision(db)
        return

    def signal(self, db, rid, kind, source, active, severity, detail):
        self.resident(db, rid)
        key = f"{rid}:{kind}:{source}"
        row = db.execute("SELECT data FROM signal_conditions WHERE id=?", (key,)).fetchone()
        prior = json.loads(row["data"]) if row else None
        now = stamp(self.clock())
        event = None
        if prior:
            row = db.execute("SELECT data FROM care_events WHERE id=?", (prior["eventId"],)).fetchone()
            if row:
                event = json.loads(row["data"])
        if not active:
            self._recover_signal(db, rid, key, prior, event, now)
            return
        fresh = not event or event["state"] in CLOSED
        recurrent = prior is not None and not prior["active"]
        worsening = event and severity == "danger" and event["severity"] != "danger"
        if fresh:
            event = self._new_signal_event(db, rid, key, kind, source, severity, detail, prior, now)
        else:
            self._update_signal_event(db, event, severity, detail, recurrent, worsening)
        condition = {
            "id": key,
            "residentId": rid,
            "type": kind,
            "active": True,
            "severity": severity,
            "detail": detail,
            "firstDetectedAt": prior["firstDetectedAt"] if prior and prior["active"] else now,
            "lastDetectedAt": now,
            "generation": (prior["generation"] if prior else 0) + int(fresh or recurrent),
            "eventId": event["id"],
        }
        changed = (
            fresh or recurrent or worsening or not prior or prior["detail"] != detail or prior["severity"] != severity
        )
        self.save(db, "care_events", event["id"], event, rid)
        self.save(db, "signal_conditions", key, condition, rid)
        self.notifications.enqueue(db, event)
        if changed:
            self.revision(db)

    def tick(self, db=None):
        if db is None:
            with self.transaction() as conn:
                self.tick(conn)
            return
        for row in db.execute("SELECT id,data FROM residents").fetchall():
            p = json.loads(row["data"])
            offline = self.clock() - epoch(p["lastSeen"]) > 300
            state = "offline" if offline else p["deviceState"]
            if p["deviceState"] != state:
                p["deviceState"] = state
                self.save(db, "residents", row["id"], p)
                self.revision(db)
            self.signal(db, row["id"], "offline", "heartbeat", offline, "warning", "超过 5 分钟未收到设备心跳")
        for row in db.execute(
            "SELECT id,resident_id,data FROM medication_tasks WHERE json_extract(data,'$.confirmedAt') IS NULL"
        ).fetchall():
            dose = json.loads(row["data"])
            self.signal(
                db,
                row["resident_id"],
                "medication",
                row["id"],
                not dose.get("confirmedAt") and self.clock() > epoch(dose["dueAt"]) + 1800,
                "warning",
                dose["name"] + " · 超过计划时间 30 分钟未确认",
            )

    def _snapshot_events(self, db, user, residents):
        scope, args = (" AND resident_id=?", (user["residentId"],)) if user["role"] == "resident" else ("", ())
        rows = db.execute(
            "SELECT rowid,data FROM care_events WHERE json_extract(data,'$.state') NOT IN ('resolved','false_alarm')"
            + scope,
            args,
        ).fetchall()
        rows += db.execute(
            "SELECT rowid,data FROM care_events WHERE json_extract(data,'$.state') IN ('resolved','false_alarm')"
            + scope
            + " ORDER BY rowid DESC LIMIT 200",
            args,
        ).fetchall()
        events = []
        for row in sorted(rows, key=lambda row: -row["rowid"]):
            e = json.loads(row["data"])
            if str(e["residentId"]) not in residents:
                continue
            e["history"] = [
                json.loads(h["data"])
                for h in db.execute(
                    "SELECT data FROM event_history WHERE event_id=? ORDER BY id DESC LIMIT 100", (e["id"],)
                )
            ][::-1]
            events.append(e)
        return events

    def _snapshot_residents(self, db, user):
        rows = db.execute(
            "SELECT id,data FROM residents" + (" WHERE id=?" if user["role"] == "resident" else ""),
            (user["residentId"],) if user["role"] == "resident" else (),
        ).fetchall()
        residents = {str(row["id"]): json.loads(row["data"]) for row in rows}
        for p in residents.values():
            # SQL-side limits keep the snapshot cost bounded as history grows.
            rows = db.execute(
                "SELECT rowid,data FROM medication_tasks WHERE resident_id=? AND json_extract(data,'$.confirmedAt') IS NULL",
                (p["id"],),
            ).fetchall()
            rows += db.execute(
                "SELECT rowid,data FROM medication_tasks WHERE resident_id=? AND json_extract(data,'$.confirmedAt') IS NOT NULL ORDER BY rowid DESC LIMIT 200",
                (p["id"],),
            ).fetchall()
            p["doses"] = [json.loads(row["data"]) for row in sorted(rows, key=lambda row: row["rowid"])]
        return residents

    def snapshot(self, db, user):
        staff = (
            []
            if user["role"] == "resident"
            else [
                {"id": row["identity"], "name": row["name"]}
                for row in db.execute("SELECT identity,name FROM users WHERE role='staff'")
            ]
        )
        residents = self._snapshot_residents(db, user)
        events = self._snapshot_events(db, user, residents)
        logs = (
            []
            if user["role"] == "resident"
            else [
                json.loads(row["data"]) for row in db.execute("SELECT data FROM audit_logs ORDER BY id DESC LIMIT 500")
            ][::-1]
        )
        value = {
            "revision": db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()[0],
            "serverTime": stamp(self.clock()),
            "staff": staff,
            "residents": residents,
            "events": events,
            "logs": logs,
        }
        return resident_snapshot(value) if user["role"] == "resident" else value

    def authorize_resident(self, user, rid):
        require(user["role"] == "staff" or user.get("residentId") == rid, 403, "FORBIDDEN")

    def patch_resident(self, db, rid, body, user):
        self.authorize_resident(user, rid)
        p = self.resident(db, rid)
        require(set(body) <= {"expectedRevision", "name", "age", "contacts", "contactUpdates", "responsible"})
        require(integer(body.get("expectedRevision")))
        require(body["expectedRevision"] == p["revision"], 409, "CONFLICT")
        require(not ("contacts" in body and "contactUpdates" in body))
        if "responsible" in body:
            require(user["role"] == "staff", 403, "FORBIDDEN")
            require(
                db.execute("SELECT 1 FROM users WHERE role='staff' AND identity=?", (body["responsible"],)).fetchone()
                is not None
            )
            p["responsible"] = body["responsible"]
        if "name" in body:
            p["name"] = string(body["name"], 20)
        if "age" in body:
            require(type(body["age"]) is int and 1 <= body["age"] <= 120)
            p["age"] = body["age"]
        contacts = body.get("contacts", p["contacts"])
        if "contactUpdates" in body:
            require(isinstance(body["contactUpdates"], list) and 1 <= len(body["contactUpdates"]) <= 3)
            contacts = [dict(c) for c in contacts]
            for item in body["contactUpdates"]:
                require(
                    isinstance(item, dict)
                    and set(item) == {"index", "value"}
                    and type(item["index"]) is int
                    and 0 <= item["index"] <= len(contacts)
                    and item["index"] < 3
                )
                if item["index"] == len(contacts):
                    contacts.append(item["value"])
                else:
                    contacts[item["index"]] = item["value"]
        require(isinstance(contacts, list) and len(contacts) <= 3)
        for c in contacts:
            require(isinstance(c, dict) and set(c) == {"name", "phone"})
            c["name"] = string(c["name"], 30)
            c["phone"] = string(c["phone"], 22)
            require(re.fullmatch(r"\+?[\d -]{5,22}", c["phone"]) is not None)
        p["contacts"] = contacts
        p["revision"] += 1
        p["updatedAt"] = stamp(self.clock())
        self.save(db, "residents", rid, p)
        self.revision(db)
        self.audit(db, rid, user["username"], "更新居民资料")

    def transition(self, db, eid, body, user):
        require(user["role"] == "staff", 403, "FORBIDDEN")
        require(set(body) <= {"action", "note", "revision", "targetStaffId"})
        row = db.execute("SELECT data FROM care_events WHERE id=?", (eid,)).fetchone()
        require(row, 404, "NOT_FOUND")
        e = json.loads(row["data"])
        require(integer(body.get("revision")))
        require(e["revision"] == body["revision"], 409, "CONFLICT")
        require(e["state"] not in CLOSED, 409, "CONFLICT")
        actor = user["staffId"]
        action, note, notification_changed = apply_transition(
            e,
            body,
            actor,
            self.clock,
            lambda target: db.execute(
                "SELECT 1 FROM users WHERE role='staff' AND identity=?", (target,)
            ).fetchone() is not None,
        )
        if notification_changed:
            self.notifications.enqueue(db, e)
        e["revision"] += 1
        e["updatedAt"] = stamp(self.clock())
        self.history(db, e, user["staffId"], action, note)
        self.save(db, "care_events", eid, e, e["residentId"])
        self.revision(db)
        self.audit(db, e["residentId"], user["username"], action, eventId=eid)
        if e["state"] in CLOSED:
            row = db.execute("SELECT data FROM signal_conditions WHERE id=?", (e["signalId"],)).fetchone()
            c = json.loads(row["data"]) if row else None
            if c and c["active"]:
                self.signal(db, e["residentId"], c["type"], e["source"], True, c["severity"], c["detail"])

    def doses(self, db, rid, body, user, key):
        require(user["role"] == "staff", 403, "FORBIDDEN")
        self.resident(db, rid)
        require(set(body) == {"name", "dueAt"})
        name = string(body["name"], 100)
        due = epoch(body["dueAt"])
        if self.idempotent(db, user["username"] + f":doses:{rid}", key, body):
            return
        require(due >= self.clock() - 60)
        count = db.execute(
            "SELECT COUNT(*) FROM medication_tasks WHERE resident_id=? AND json_extract(data,'$.confirmedAt') IS NULL",
            (rid,),
        ).fetchone()[0]
        require(count < 500, 422, "LIMIT_EXCEEDED")
        dose = {
            "id": str(uuid.uuid4()),
            "name": name,
            "dueAt": stamp(due),
            "createdAt": stamp(self.clock()),
            "source": "工作人员登记",
            "actor": user["username"],
        }
        self.save(db, "medication_tasks", dose["id"], dose, rid)
        self.revision(db)
        self.audit(db, rid, user["username"], "登记用药任务")

    def presented(self, db, body, user):
        require(user["role"] == "staff", 403, "FORBIDDEN")
        require(set(body) == {"events"} and isinstance(body["events"], list) and len(body["events"]) <= 500)
        targets = []
        for item in body["events"]:
            require(
                isinstance(item, dict)
                and set(item) == {"id", "cycle"}
                and isinstance(item["id"], str)
                and integer(item["cycle"])
                and item["cycle"] > 0
            )
            row = db.execute("SELECT data FROM care_events WHERE id=?", (item["id"],)).fetchone()
            require(row, 404, "NOT_FOUND")
            e = json.loads(row["data"])
            require(item["cycle"] == e["notification"]["cycle"], 409, "CONFLICT")
            targets.append(e)
        for e in targets:
            if e["notification"]["presentedCycle"] == e["notification"]["cycle"]:
                continue
            e["notification"]["presentedCycle"] = e["notification"]["cycle"]
            self.save(db, "care_events", e["id"], e, e["residentId"])
            self.history(db, e, user["staffId"], "页面提醒已展示")
            self.revision(db)
