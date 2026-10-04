"""Authentication, session lifecycle and login throttling for the care API."""

import hashlib
import hmac
import ipaddress
import secrets

try:
    from .common import Problem, digest, require, string
except ImportError:
    from common import Problem, digest, require, string


class AuthService:
    def __init__(self, store, staff_session_seconds=43200, trusted_proxy=False):
        require(900 <= staff_session_seconds <= 86400)
        self.store = store
        self.staff_ttl = int(staff_session_seconds)
        self.trusted_proxy = trusted_proxy

    def session(self, db, cookie):
        if not cookie:
            return None
        return db.execute(
            "SELECT * FROM sessions WHERE id=? AND expires>?",
            (digest(cookie), self.store.clock()),
        ).fetchone()

    def new_session(self, db, username=None):
        db.execute("DELETE FROM sessions WHERE expires<=?", (self.store.clock(),))
        token, csrf = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
        role = db.execute("SELECT role FROM users WHERE username=?", (username,)).fetchone() if username else None
        ttl = self.staff_ttl if role and role["role"] == "staff" else 86400 if username else 3600
        db.execute("INSERT INTO sessions VALUES(?,?,?,?)", (digest(token), username, csrf, self.store.clock() + ttl))
        return token, csrf

    def client_address(self, env):
        """Resolve the limiter key without trusting client-controlled proxy hops."""
        remote = env.get("REMOTE_ADDR", "")
        if not self.trusted_proxy:
            return remote
        for part in reversed(env.get("HTTP_X_FORWARDED_FOR", "").split(",")):
            try:
                return str(ipaddress.ip_address(part.strip()))
            except ValueError:
                continue
        return remote

    def user(self, db, session):
        require(session and session["username"], 401, "UNAUTHENTICATED")
        row = db.execute(
            "SELECT username,role,identity,name FROM users WHERE username=?", (session["username"],)
        ).fetchone()
        require(row, 401, "UNAUTHENTICATED")
        return {
            "username": row["username"],
            "role": row["role"],
            "name": row["name"],
            **({"staffId": row["identity"]} if row["role"] == "staff" else {"residentId": int(row["identity"])}),
        }

    def login(self, db, body, sid, env):
        require(set(body) == {"role", "username", "password"} and body["role"] in {"staff", "resident"})
        username = string(body["username"], 80)
        require(isinstance(body["password"], str) and len(body["password"]) <= 256)
        subject = digest(self.client_address(env) + ":" + username)
        limit = db.execute("SELECT * FROM login_limits WHERE subject=?", (subject,)).fetchone()
        require(not limit or limit["until"] <= self.store.clock(), 429, "RATE_LIMITED")
        row = db.execute("SELECT * FROM users WHERE username=?", (username,)).fetchone()
        # Equal work for nonexistent users; role and account details are not disclosed.
        salt = bytes.fromhex(row["salt"]) if row else bytes(16)
        hashed = hashlib.scrypt(body["password"].encode(), salt=salt, n=16384, r=8, p=1).hex()
        if not row or row["role"] != body["role"] or not hmac.compare_digest(hashed, row["password_hash"]):
            failures = (limit["failures"] if limit and limit["until"] > self.store.clock() - 60 else 0) + 1
            db.execute(
                "INSERT INTO login_limits VALUES(?,?,?) ON CONFLICT(subject) DO UPDATE SET failures=excluded.failures,until=excluded.until",
                (subject, failures, self.store.clock() + 60 if failures >= 5 else self.store.clock()),
            )
            # Returning the error allows the transaction owner to commit the limiter update.
            return 401, {"error": {"code": "UNAUTHENTICATED"}}, None
        db.execute("DELETE FROM login_limits WHERE subject=?", (subject,))
        db.execute("DELETE FROM sessions WHERE id=?", (digest(sid),))
        token, csrf = self.new_session(db, username)
        current = self.session(db, token)
        return 200, {"user": self.user(db, current), "csrfToken": csrf}, token

    def session_route(self, db, body, sid, session, method, env):
        if method == "GET":
            if not session:
                token, csrf = self.new_session(db)
                return 200, {"user": None, "csrfToken": csrf}, token
            user = self.user(db, session) if session["username"] else None
            return 200, {"user": user, "csrfToken": session["csrf"]}, None
        if method == "POST":
            return self.login(db, body, sid, env)
        if method == "DELETE":
            db.execute("DELETE FROM sessions WHERE id=?", (digest(sid),))
            return 204, None, ""
        raise Problem(405, "METHOD_NOT_ALLOWED")
