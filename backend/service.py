"""Single-community care API. SQLite owns sessions, signals, incidents and audit.
No clinical thresholds: authenticated device inputs carry reviewed severity.
"""

import argparse
import getpass
import hmac
import json
import os
import re
import sqlite3
import threading
from http.cookies import SimpleCookie
from pathlib import Path
from urllib.parse import unquote
from wsgiref.simple_server import make_server, WSGIRequestHandler, WSGIServer
from socketserver import ThreadingMixIn

try:
    from .common import TYPES, Problem, digest, encode, epoch, integer, require, stamp, string
    from .care import CareStore, SCHEMA
    from .notifications import FakeNotificationService
    from .auth import AuthService
except ImportError:
    from common import TYPES, Problem, digest, encode, epoch, integer, require, stamp, string
    from care import CareStore, SCHEMA
    from notifications import FakeNotificationService
    from auth import AuthService


class CareApplication:
    def __init__(
        self,
        store,
        origin,
        static_root=None,
        secure=True,
        device_key="",
        staff_session_seconds=43200,
        trusted_proxy=False,
    ):
        require(origin.startswith("https://") or not secure)
        self.store, self.origin, self.secure, self.device_key = store, origin.rstrip("/"), secure, device_key
        self.auth = AuthService(store, staff_session_seconds=staff_session_seconds, trusted_proxy=trusted_proxy)
        self.static_root = Path(static_root).resolve() if static_root else None

    def _confirm_dose(self, db, user, body, match):
        rid, doseid = int(match[1]), match[2]
        self.store.authorize_resident(user, rid)
        require(not body)
        row = db.execute("SELECT data FROM medication_tasks WHERE id=? AND resident_id=?", (doseid, rid)).fetchone()
        require(row, 404, "NOT_FOUND")
        dose = json.loads(row["data"])
        if not dose.get("confirmedAt"):
            require(self.store.clock() >= epoch(dose["dueAt"]) - 1800)
            dose["confirmedAt"] = stamp(self.store.clock())
            self.store.save(db, "medication_tasks", doseid, dose, rid)
            self.store.revision(db)
            self.store.audit(db, rid, user["username"], "确认服药")
            self.store.signal(db, rid, "medication", doseid, False, "warning", "")
        return 204, None, None

    def _audit_page(self, db, user, env):
        require(user["role"] == "staff", 403, "FORBIDDEN")
        from urllib.parse import parse_qs

        query = parse_qs(env.get("QUERY_STRING", ""))
        try:
            after = int(query.get("after", ["0"])[0])
            limit = int(query.get("limit", ["100"])[0])
        except ValueError:
            raise Problem(422, "VALIDATION_FAILED")
        require(after >= 0 and 1 <= limit <= 200)
        rows = db.execute("SELECT id,data FROM audit_logs WHERE id>? ORDER BY id LIMIT ?", (after, limit)).fetchall()
        return (
            200,
            {
                "items": [{"id": r["id"], **json.loads(r["data"])} for r in rows],
                "next": rows[-1]["id"] if rows else after,
            },
            None,
        )

    def _inquiry(self, db, body, sid, env):
        require(set(body) == {"name", "phone", "type", "message"})
        for key, maximum in [("name", 80), ("phone", 30), ("type", 80), ("message", 2000)]:
            string(body[key], maximum)
        if not self.store.idempotent(db, "inquiry:" + digest(sid), env.get("HTTP_IDEMPOTENCY_KEY"), body):
            db.execute("INSERT INTO inquiries(data) VALUES(?)", (encode({"at": stamp(self.store.clock()), **body}),))
        return 204, None, None

    def _update_device_metrics(self, body, p, now):
        metrics = body.get("metrics", {})
        states = body.get("metricStates", {})
        keys = {"heartRate", "bloodOxygen", "temperature", "systolic", "diastolic", "bloodSugar"}
        require(
            isinstance(metrics, dict)
            and set(metrics) <= keys
            and all(type(v) in {int, float} and abs(v) < 1e6 for v in metrics.values())
        )
        require(
            isinstance(states, dict)
            and set(states) <= keys
            and all(v in {"normal", "warning", "danger", "unknown"} for v in states.values())
        )
        p.update(metrics)
        p["metricStates"].update(states)
        p["lastSeen"] = now
        p["updatedAt"] = now
        p["deviceState"] = "online"
        p["trend"] = (p["trend"] + [{"at": now, **metrics}])[-72:]

    def _device_event(self, db, body, env):
        require(bool(self.device_key), 503, "DEVICE_DISABLED")
        require(
            hmac.compare_digest(env.get("HTTP_AUTHORIZATION", ""), "Bearer " + self.device_key),
            401,
            "UNAUTHENTICATED",
        )
        require(
            set(body) <= {"residentId", "type", "source", "active", "severity", "detail", "metrics", "metricStates"}
        )
        rid = body.get("residentId")
        require(integer(rid) and rid > 0)
        kind = body.get("type")
        require(kind in TYPES | {"heartbeat"})
        source = string(body.get("source", "device"), 80)
        active = body.get("active", True)
        require(type(active) is bool)
        severity = body.get("severity", "warning")
        require(severity in {"warning", "danger"})
        detail = string(body.get("detail", "设备信号待人工核实"), 500)
        if self.store.idempotent(db, "device", env.get("HTTP_IDEMPOTENCY_KEY"), body):
            return 204, None, None
        p = self.store.resident(db, rid)
        now = stamp(self.store.clock())
        self._update_device_metrics(body, p, now)
        self.store.save(db, "residents", rid, p)
        self.store.revision(db)
        if kind != "heartbeat":
            self.store.signal(db, rid, kind, source, active, severity, detail)
        self.store.signal(db, rid, "offline", "heartbeat", False, "warning", "设备恢复心跳")
        self.store.audit(db, rid, "device", "设备事件接入", source=source)
        return 204, None, None

    def route(self, env, body, cookies, db):
        method, path = env["REQUEST_METHOD"], unquote(env["PATH_INFO"])
        sid = cookies["yihe_session"].value if "yihe_session" in cookies else ""
        session = self.auth.session(db, sid)
        if path == "/api/v1/device/events" and method == "POST":
            return self._device_event(db, body, env)
        if method not in {"GET", "HEAD"}:
            require(env.get("HTTP_ORIGIN") == self.origin, 403, "ORIGIN_REJECTED")
            require(session is not None, 401, "UNAUTHENTICATED")
            require(hmac.compare_digest(env.get("HTTP_X_CSRF_TOKEN", ""), session["csrf"]), 403, "CSRF_REJECTED")
        if path == "/api/v1/session":
            return self.auth.session_route(db, body, sid, session, method, env)
        if path == "/api/v1/inquiries" and method == "POST":
            return self._inquiry(db, body, sid, env)
        user = self.auth.user(db, session)
        if path == "/api/v1/care/snapshot" and method == "GET":
            return 200, self.store.snapshot(db, user), None
        if path == "/api/v1/audit" and method == "GET":
            return self._audit_page(db, user, env)
        if path == "/api/v1/events/presented" and method == "POST":
            self.store.presented(db, body, user)
            return 204, None, None
        match = re.fullmatch(r"/api/v1/events/([^/]+)/transitions", path)
        if match and method == "POST":
            self.store.transition(db, match[1], body, user)
            return 204, None, None
        match = re.fullmatch(r"/api/v1/residents/(\d+)", path)
        if match and method == "PATCH":
            self.store.patch_resident(db, int(match[1]), body, user)
            return 204, None, None
        match = re.fullmatch(r"/api/v1/residents/(\d+)/doses", path)
        if match and method == "POST":
            self.store.doses(db, int(match[1]), body, user, env.get("HTTP_IDEMPOTENCY_KEY"))
            return 204, None, None
        match = re.fullmatch(r"/api/v1/residents/(\d+)/doses/([^/]+)/confirm", path)
        if match and method == "POST":
            return self._confirm_dose(db, user, body, match)
        raise Problem(404, "NOT_FOUND")

    def _static_response(self, env, headers):
        require(self.static_root is not None and env["REQUEST_METHOD"] in {"GET", "HEAD"}, 404, "NOT_FOUND")
        path = (self.static_root / unquote(env["PATH_INFO"]).lstrip("/")).resolve()
        if env["PATH_INFO"] == "/":
            path = self.static_root / "home.html"
        require(path.is_relative_to(self.static_root) and path.is_file() and not path.is_symlink(), 404, "NOT_FOUND")
        import mimetypes

        data = path.read_bytes()
        headers.append(("Content-Type", mimetypes.guess_type(str(path))[0] or "application/octet-stream"))
        status = 200
        return status, data

    def _api_response(self, env, headers):
        length = int(env.get("CONTENT_LENGTH") or 0)
        require(0 <= length <= 65536, 413, "PAYLOAD_TOO_LARGE")
        raw = env["wsgi.input"].read(length) if length else b"{}"
        try:
            body = json.loads(raw, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
        except (ValueError, UnicodeError):
            raise Problem(400, "INVALID_JSON")
        require(isinstance(body, dict), 400, "INVALID_JSON")
        cookie = SimpleCookie()
        cookie.load(env.get("HTTP_COOKIE", ""))
        path = unquote(env["PATH_INFO"])
        pure_read = env["REQUEST_METHOD"] in {"GET", "HEAD"} and path in {
            "/api/v1/care/snapshot",
            "/api/v1/audit",
        }
        transaction = self.store.read_transaction if pure_read else self.store.write_transaction
        with transaction() as db:
            status, value, token = self.route(env, body, cookie, db)
        data = encode(value).encode() if value is not None else b""
        headers.append(("Content-Type", "application/json; charset=utf-8"))
        if token is not None:
            headers.append(
                (
                    "Set-Cookie",
                    "yihe_session="
                    + token
                    + "; Path=/; HttpOnly; SameSite=Lax"
                    + ("; Secure" if self.secure else "")
                    + ("; Max-Age=0" if token == "" else ""),
                )
            )
        return status, data

    def __call__(self, env, start_response):
        from http import HTTPStatus

        # WSGI transports URL path bytes through latin-1; recover UTF-8 file names.
        try:
            env = {**env, "PATH_INFO": env["PATH_INFO"].encode("latin-1").decode("utf-8")}
        except (UnicodeError, KeyError):
            pass
        headers = [
            ("Cache-Control", "no-store"),
            ("X-Content-Type-Options", "nosniff"),
            ("Referrer-Policy", "no-referrer"),
        ]
        try:
            if not env["PATH_INFO"].startswith("/api/"):
                status, data = self._static_response(env, headers)
            else:
                status, data = self._api_response(env, headers)
        except Problem as e:
            status, data = e.status, encode({"error": {"code": e.code}}).encode()
            headers.append(("Content-Type", "application/json; charset=utf-8"))
        except (ValueError, TypeError, KeyError):
            status, data = 422, encode({"error": {"code": "VALIDATION_FAILED"}}).encode()
            headers.append(("Content-Type", "application/json; charset=utf-8"))
        except Exception:
            status, data = 500, b'{"error":{"code":"SERVICE_UNAVAILABLE"}}'
            headers.append(("Content-Type", "application/json; charset=utf-8"))
        headers.append(("Content-Length", str(len(data))))
        start_response(f"{status} {HTTPStatus(status).phrase}", headers)
        return [b"" if env["REQUEST_METHOD"] == "HEAD" else data]


class ThreadedServer(ThreadingMixIn, WSGIServer):
    daemon_threads = True


class QuietHandler(WSGIRequestHandler):
    def log_message(self, *_):
        pass  # No identifiers, tokens or health data in access logs.


def serve(args, store):
    require(
        not args.insecure_local or args.origin in {f"http://127.0.0.1:{args.port}", f"http://localhost:{args.port}"}
    )
    app = CareApplication(
        store,
        args.origin,
        args.static,
        secure=not args.insecure_local,
        device_key=os.environ.get("YIHE_DEVICE_KEY", ""),
        staff_session_seconds=int(args.staff_session_hours * 3600),
        trusted_proxy=args.trusted_proxy,
    )
    stopped = threading.Event()

    def worker():
        # Run immediately on startup, then every five seconds. Reads never trigger
        # time-driven domain mutations; this worker is the sole production clock.
        while not stopped.is_set():
            try:
                store.tick()
            except sqlite3.Error:
                pass  # Retry next tick; no partially committed state.
            if stopped.wait(5):
                break

    thread = threading.Thread(target=worker, daemon=True)
    thread.start()
    server = make_server("127.0.0.1", args.port, app, server_class=ThreadedServer, handler_class=QuietHandler)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        stopped.set()
        server.server_close()
        thread.join(timeout=6)


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", default="care.sqlite3")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("init")
    user = sub.add_parser("add-user")
    user.add_argument("username")
    user.add_argument("role", choices=["staff", "resident"])
    user.add_argument("identity")
    user.add_argument("name")
    resident = sub.add_parser("add-resident")
    resident.add_argument("json_file")
    serve = sub.add_parser("serve")
    serve.add_argument("--port", type=int, default=8000)
    serve.add_argument("--origin", required=True)
    serve.add_argument("--insecure-local", action="store_true")
    serve.add_argument("--static", default="docs/dist")
    serve.add_argument("--staff-session-hours", type=float, default=12)
    serve.add_argument("--trusted-proxy", action="store_true")
    args = parser.parse_args()
    return args


def main():
    args = parse_args()
    store = CareStore(args.db)
    if args.command == "add-user":
        store.add_user(
            args.username, args.role, args.identity, args.name, getpass.getpass("Password (12+ characters): ")
        )
        return
    if args.command == "add-resident":
        store.add_resident(json.loads(Path(args.json_file).read_text()))
        return
    if args.command == "init":
        return
    serve(args, store)


if __name__ == "__main__":
    main()
