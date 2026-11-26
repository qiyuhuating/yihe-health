#!/usr/bin/env python3
"""Run outside the application/Prometheus failure domain; persist unsent alerts.

HTTP success means receiver acceptance, never human acknowledgement or rescue.
Use a separate host/supervisor in production. Loopback HTTP is for local drills.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import time
import urllib.error
import urllib.parse
import urllib.request
import ipaddress


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        return None


def secret(name):
    path = os.getenv(name+"_FILE")
    return Path(path).read_text(encoding="utf-8").strip() if path else os.getenv(name, "").strip()


def validate_receiver(url):
    parsed = urllib.parse.urlsplit(url)
    loopback = False
    try:
        loopback = ipaddress.ip_address(parsed.hostname or "").is_loopback
    except ValueError:
        pass
    if parsed.username or parsed.password or not parsed.hostname or (parsed.scheme != "https" and not (parsed.scheme == "http" and loopback)):
        raise ValueError("receiver requires authenticated HTTPS (numeric loopback HTTP allowed for drills)")


class Watchdog:
    def __init__(self, state, receiver, token, clock=time.time):
        validate_receiver(receiver)
        if len(token) < 24:
            raise ValueError("independent receiver token must have at least 24 characters")
        state = Path(state)
        state.parent.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(state)
        self.db.execute("CREATE TABLE IF NOT EXISTS notification(id TEXT PRIMARY KEY,group_key TEXT NOT NULL,event_status TEXT NOT NULL,body TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,retry_at REAL NOT NULL DEFAULT 0,accepted_at REAL)")
        self.db.execute("CREATE TABLE IF NOT EXISTS component(name TEXT PRIMARY KEY,failed_since TEXT,failures INTEGER NOT NULL DEFAULT 0)")
        self.db.commit()
        os.chmod(state, 0o600)
        self.receiver, self.token = receiver, token
        self.clock = clock
        self.http = urllib.request.build_opener(NoRedirect)

    def observe(self, name, endpoint, threshold):
        try:
            with self.http.open(endpoint, timeout=3) as response:
                healthy = response.status == 200
        except (OSError, urllib.error.URLError):
            healthy = False
        row = self.db.execute("SELECT failed_since,failures FROM component WHERE name=?", (name,)).fetchone()
        since, failures = row if row else (None, 0)
        now = datetime.datetime.now(datetime.timezone.utc).isoformat()
        if not healthy:
            since = since or now
            failures += 1
            if failures >= threshold:
                self.enqueue(name, since, "firing", now)
        else:
            if since and failures >= threshold:
                self.enqueue(name, since, "resolved", now)
            since, failures = None, 0
        self.db.execute("INSERT INTO component(name,failed_since,failures) VALUES(?,?,?) ON CONFLICT(name) DO UPDATE SET failed_since=excluded.failed_since,failures=excluded.failures", (name, since, failures))
        self.db.commit()

    def enqueue(self, name, since, status, now):
        event_id = hashlib.sha256((name+since+status).encode()).hexdigest()
        payload = {"version": "4", "status": status, "receiver": "independent-watchdog", "alerts": [{
            "status": status, "labels": {"alertname": name+"Unavailable", "severity": "critical", "source": "independent-watchdog"},
            "annotations": {"summary": name+" readiness check failed", "description": "Check monitoring infrastructure and use the manual care fallback. Receiver acceptance is not a human acknowledgement."},
            "startsAt": since, "endsAt": now if status == "resolved" else "0001-01-01T00:00:00Z",
        }]}
        self.db.execute("INSERT OR IGNORE INTO notification(id,group_key,event_status,body) VALUES(?,?,?,?)", (event_id,name+since,status,json.dumps(payload)))

    def deliver(self):
        now = self.clock()
        rows = self.db.execute("SELECT id,body,attempts,rowid,group_key,event_status FROM notification WHERE status='pending' AND retry_at<=? ORDER BY rowid LIMIT 20", (now,)).fetchall()
        for event_id, body, attempts, rowid, group, event_status in rows:
            if event_status=="resolved" and self.db.execute("SELECT 1 FROM notification WHERE group_key=? AND status='pending' AND rowid<? LIMIT 1",(group,rowid)).fetchone():
                continue
            request = urllib.request.Request(self.receiver, body.encode(), method="POST", headers={"Authorization": "Bearer "+self.token, "Content-Type": "application/json", "Idempotency-Key": "watchdog-"+event_id})
            try:
                with self.http.open(request, timeout=5) as response:
                    if not 200 <= response.status < 300:
                        raise OSError("receiver did not accept event")
                self.db.execute("UPDATE notification SET status='receiver_accepted',accepted_at=? WHERE id=?", (self.clock(), event_id))
                print(json.dumps({"event_id": event_id, "state": "receiver_accepted", "human_acknowledged": False}), flush=True)
            except (OSError, urllib.error.URLError):
                delay = min(300, 5*(2**min(attempts, 6)))
                self.db.execute("UPDATE notification SET attempts=attempts+1,retry_at=? WHERE id=?", (self.clock()+delay, event_id))
                print(json.dumps({"event_id": event_id, "state": "pending_retry", "delay_seconds": delay}), flush=True)
            self.db.commit()
        # Failed/pending events are never removed by retention.
        self.db.execute("DELETE FROM notification WHERE status='receiver_accepted' AND accepted_at<?", (self.clock()-7*86400,))
        self.db.commit()


def main():
    watchdog = Watchdog(os.environ["WATCHDOG_STATE"], secret("ONCALL_WEBHOOK_URL"), secret("ONCALL_TOKEN"))
    endpoints = {"Prometheus": os.environ["WATCHDOG_PROMETHEUS_URL"], "Alertmanager": os.environ["WATCHDOG_ALERTMANAGER_URL"]}
    interval = max(1, int(os.getenv("WATCHDOG_INTERVAL_SECONDS", "15")))
    threshold = max(2, int(os.getenv("WATCHDOG_FAILURE_THRESHOLD", "2")))
    while True:
        for name, endpoint in endpoints.items():
            watchdog.observe(name, endpoint, threshold)
        watchdog.deliver()
        time.sleep(interval)


if __name__ == "__main__":
    main()
