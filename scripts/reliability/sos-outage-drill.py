#!/usr/bin/env python3
"""Exercise signed SOS and notification recovery on owned MySQL and Go processes."""
import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
import configparser
import hashlib
import hmac
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
from threading import Barrier
import time
from urllib.error import HTTPError, URLError
from urllib.request import HTTPHandler, ProxyHandler, Request, build_opener
import uuid


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def loopback_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def await_state(check, message, seconds=45):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(.25)
    raise RuntimeError(message)


class Fixture:
    def __init__(self, tools, repo, lab):
        self.tools, self.repo, self.lab = tools, repo, lab
        self.mysql = self.server = None
        self.server_id = ""
        self.mysql_port, self.app_port = loopback_port(), loopback_port()
        require(self.mysql_port != self.app_port, "Loopback ports collided")
        self.schema = "yihe_drill_" + uuid.uuid4().hex[:16]
        self.dsn = f"root@tcp(127.0.0.1:{self.mysql_port})/{self.schema}?parseTime=true"
        self.secret = secrets.token_urlsafe(32)
        self.client = build_opener(ProxyHandler({}), HTTPHandler())
        self.suffix = ".exe" if os.name == "nt" else ""
        self.flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
        self.env = {key: value for key, value in os.environ.items()
                    if key.upper() in {"PATH", "SYSTEMROOT", "WINDIR", "LANG", "LC_ALL", "LD_LIBRARY_PATH"}}
        self.env.update(
            MYSQL_DSN=self.dsn, ADDR=f"127.0.0.1:{self.app_port}", BLOB_BACKEND="mysql",
            DATA_ENCRYPTION_KEY=base64.b64encode(secrets.token_bytes(32)).decode("ascii"),
            YIHE_REQUIRE_SIGNED_DEVICE="1", LAB_DEVICE_SECRET=self.secret,
            LAB_STATE_FILE=str(lab / "fixture-sessions.secret"), TEMP=str(lab), TMP=str(lab), TMPDIR=str(lab),
            YIHE_RISK_AUDIT_SPOOL_PATH=str(lab / "risk-audit.jsonl"),
        )
        self.go_env = {key: value for key, value in os.environ.items()
                       if not key.upper().startswith(("TEST_", "MYSQL", "CARE_", "NOTIFY_", "SMS_", "YIHE_", "PG_"))}
        self.go_env.update(TEST_MYSQL_DSN=self.dsn, GOWORK="off")

    def tool(self, name):
        path = self.tools / (name + self.suffix)
        require(path.is_file(), f"MySQL installation is missing {name}")
        return str(path)

    def command(self, name, args, cwd=None, env=None, timeout=120):
        with (self.lab / (name + ".log")).open("wb") as output:
            result = subprocess.run(args, cwd=cwd or self.lab, env=env or self.env, stdout=output,
                                    stderr=subprocess.STDOUT, timeout=timeout, creationflags=self.flags, check=False)
        require(result.returncode == 0, f"{name} failed; see its local log")

    def launch(self, label, args):
        with (self.lab / (label + ".log")).open("ab") as log:
            return subprocess.Popen(args, cwd=self.lab, env=self.env, stdout=log,
                                    stderr=subprocess.STDOUT, creationflags=self.flags)

    def mysql_args(self, tool):
        return [self.tool(tool), "--no-defaults", "--protocol=TCP", "--host=127.0.0.1",
                f"--port={self.mysql_port}", "--user=root", "--connect-timeout=2"]

    def sql(self, statement, schema=True):
        args = self.mysql_args("mysql") + ["--batch", "--skip-column-names"]
        if schema:
            args += [self.schema]
        result = subprocess.run(args + ["-e", statement], cwd=self.lab, env=self.env, text=True,
                                capture_output=True, timeout=10, creationflags=self.flags, check=False)
        require(result.returncode == 0, "Owned MySQL query failed")
        return result.stdout.strip()

    def matches_mysql(self):
        if not self.server_id or self.mysql is None or self.mysql.poll() is not None:
            return False
        try:
            return self.sql("SELECT @@server_uuid", schema=False) == self.server_id
        except (RuntimeError, subprocess.TimeoutExpired):
            return False

    def start_mysql(self):
        directory = self.lab / "mysql"
        self.mysql = self.launch("mysql-server", [
            self.tool("mysqld"), "--no-defaults", f"--basedir={self.tools.parent}", f"--datadir={directory}",
            "--bind-address=127.0.0.1", f"--port={self.mysql_port}", "--mysqlx=OFF",
            "--skip-log-bin", "--innodb-buffer-pool-size=64M",
            *(["--no-monitor", "--console"] if os.name == "nt" else []),
        ])

        def ready():
            require(self.mysql.poll() is None, "Owned MySQL exited before readiness")
            # The native server creates auto.cnf on its first startup.
            if not self.server_id and (directory / "auto.cnf").is_file():
                config = configparser.ConfigParser()
                try:
                    config.read(directory / "auto.cnf", encoding="ascii")
                    self.server_id = config.get("auto", "server-uuid", fallback="")
                except (OSError, configparser.Error):
                    return False
            return self.matches_mysql()

        await_state(ready, "Owned MySQL did not report its expected server UUID")

    def stop_mysql(self):
        if self.mysql is None or self.mysql.poll() is not None:
            return
        require(self.matches_mysql(), "Refusing shutdown of a MySQL server with unverified identity")
        self.command("mysql-shutdown", self.mysql_args("mysqladmin") + ["shutdown"], timeout=30)
        self.mysql.wait(timeout=30)

    def initialize(self):
        self.command("mysql-initialize", [
            self.tool("mysqld"), "--no-defaults", f"--basedir={self.tools.parent}",
            "--initialize-insecure", f"--datadir={self.lab / 'mysql'}",
        ])
        self.start_mysql()
        self.sql(f"CREATE DATABASE `{self.schema}` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin", schema=False)
        for target in ["server", "reliability"]:
            self.command("build-" + target, [
                "go", "build", "-o", str(self.lab / (target + self.suffix)), "./cmd/" + target,
            ], cwd=self.repo / "backend", env=self.go_env, timeout=180)
        self.command("seed", [str(self.lab / ("reliability" + self.suffix)), "--action", "fixture"], timeout=180)
        self.start_backend()

    def request(self, path, body=None, signed_headers=None):
        headers = signed_headers or {}
        request = Request(f"http://127.0.0.1:{self.app_port}" + path, data=body, headers=headers)
        try:
            response = self.client.open(request, timeout=15)
        except HTTPError as error:
            response = error
        with response:
            return response.status, json.load(response)

    def ready(self, expected):
        def check():
            require(self.server.poll() is None, "Owned Go server exited during readiness check")
            try:
                code, result = self.request("/api/v1/service/status")
            except (OSError, URLError):
                return False
            return code == 200 and result.get("databaseReady") is expected

        await_state(check, f"Readiness did not become {expected}", seconds=90)

    def start_backend(self):
        self.server = self.launch("go-backend", [str(self.lab / ("server" + self.suffix))])
        self.ready(True)

    def stop_backend(self):
        if self.server is not None and self.server.poll() is None:
            self.server.kill()
            self.server.wait(timeout=20)

    def sos(self, request_id, expected=200):
        body = json.dumps({"deviceNo": "LAB-WATCH", "secret": self.secret, "requestId": request_id,
                           "bindingVersion": 1}, separators=(",", ":")).encode()
        stamp, nonce = str(int(time.time())), secrets.token_hex(16)
        signature = hmac.new(self.secret.encode(),
                             f"LAB-WATCH:{stamp}:{nonce}:{hashlib.sha256(body).hexdigest()}".encode(),
                             hashlib.sha256).hexdigest()
        status, response = self.request("/api/v1/device/sos", body, {
            "Content-Type": "application/json", "X-Device-Timestamp": stamp,
            "X-Device-Nonce": nonce, "X-Device-Signature": signature,
        })
        ok, incident = response.get("returnCode") == "1111", response.get("incidentId")
        require(status == expected, f"SOS expected HTTP {expected}, received {status}")
        if expected == 200:
            require(ok and isinstance(incident, int) and incident > 0, "Successful SOS omitted its durable incident")
        else:
            require(not ok and incident is None, "Unavailable backend returned a success marker")
        return incident, response.get("duplicate") is True

    def inventory(self, count):
        result = self.sql("""SELECT (SELECT COUNT(*) FROM care_incident),
 (SELECT COUNT(*) FROM care_incident_event WHERE action='received'),
 (SELECT COUNT(*) FROM care_notification_outbox WHERE channel='sms'),
 (SELECT COUNT(*) FROM care_incident i JOIN device d ON d.device_id=i.device_id
   JOIN oldperson o ON o.old_id=i.old_id AND d.old_id=o.old_id WHERE d.device_no='LAB-WATCH' AND o.family_id=1),
 (SELECT COUNT(*) FROM care_notification_outbox o LEFT JOIN care_incident i ON i.id=o.incident_id WHERE i.id IS NULL)""")
        keys = ["incidents", "receivedEvents", "smsOutbox", "correctBindings", "orphanOutbox"]
        actual = dict(zip(keys, map(int, result.split()), strict=True))
        require(list(actual.values()) == [count, count, count, count, 0], f"Unexpected aggregate inventory: {actual}")
        return actual

    def database_and_crash_round(self, index):
        previous, fresh = f"retained-{index}", f"interrupted-{index}"
        original, _ = self.sos(previous)
        self.stop_mysql()
        self.ready(False)
        self.sos(previous, 503)
        self.sos(fresh, 503)
        self.start_mysql()
        self.ready(True)
        self.inventory(2 * index - 1)
        require(self.sos(previous) == (original, True), "Committed SOS changed on reconnect")
        retried, duplicate = self.sos(fresh)
        require(retried != original and not duplicate, "Unavailable request was committed or attributed to the wrong incident")
        after_db = self.inventory(2 * index)
        old_pid = self.server.pid
        self.stop_backend()
        self.start_backend()
        require(self.sos(previous) == (original, True) and self.sos(fresh) == (retried, True),
                "Backend crash and retry failed to recover stable incidents")
        after_crash = self.inventory(2 * index)
        return {"round": index, "outageHTTP": [503, 503], "crashedPID": old_pid, "replacementPID": self.server.pid,
                "afterDatabaseReconnect": after_db, "afterProcessCrash": after_crash}

    def submit_simultaneously(self, requests):
        barrier = Barrier(len(requests), timeout=20)

        def submit(request_id):
            barrier.wait()
            return self.sos(request_id)

        with ThreadPoolExecutor(max_workers=100) as workers:
            return list(workers.map(submit, requests))

    def concurrent(self, offset):
        requests = [f"concurrent-{index}" for index in range(100)]
        creations = self.submit_simultaneously(requests)
        incidents = {}
        for request, (incident, duplicate) in zip(requests, creations, strict=True):
            require(not duplicate, "A first SOS submission was marked duplicate")
            incidents[request] = incident
        require(len(set(incidents.values())) == len(requests), "Separate SOS identities shared an incident")
        retries = requests[:10] * 10
        for request, outcome in zip(retries, self.submit_simultaneously(retries), strict=True):
            require(outcome == (incidents[request], True), "Concurrent retry changed its original incident")
        return {"initialRequests": len(creations), "retryRequests": len(retries), "logicalSOS": len(incidents),
                "synchronized": True, "inventory": self.inventory(offset + len(incidents))}

    def gateway(self, rounds):
        name = "TestCareFullHTTPChainAfterRealGatewayTimeout"
        minutes = max(15, rounds * 5)
        self.command("gateway-timeout-events", [
            "go", "test", "-json", "-race", f"-count={rounds}", f"-timeout={minutes}m", "-run", f"^{name}$",
            "./internal/handlers",
        ], cwd=self.repo / "backend", env=self.go_env, timeout=minutes * 60 + 30)
        events = (json.loads(line) for line in (self.lab / "gateway-timeout-events.log").read_text(encoding="utf-8").splitlines()
                  if line.startswith("{"))
        actions = [event["Action"] for event in events if event.get("Test") == name and event["Action"] in {"pass", "fail", "skip"}]
        require(actions == ["pass"] * rounds, "Gateway or duplicate-receipt check failed, skipped, or missing")
        return {"rounds": rounds, "test": name, "verifiedRepeatedReceipt": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mysql-bin", required=True, type=Path, help="directory containing native MySQL executables")
    parser.add_argument("--report-dir", required=True, type=Path, help="private base directory for isolated fixtures and logs")
    parser.add_argument("--rounds", type=int, default=10, help="MySQL outage and Go crash rounds")
    parser.add_argument("--gateway-rounds", type=int, default=10, help="TLS gateway timeout and repeat-receipt rounds")
    args = parser.parse_args()
    if not 1 <= args.rounds <= 50 or not 1 <= args.gateway_rounds <= 20:
        parser.error("rounds must be 1–50 and gateway rounds 1–20")
    repo = Path(__file__).resolve().parents[2]
    source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip()
    subprocess.check_output(["git", "cat-file", "-e", f"{source}:scripts/reliability/sos-outage-drill.py"], cwd=repo)
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=no"], cwd=repo))
    require(not dirty, "Commit the source before collecting formal outage evidence")
    lab = args.report_dir.resolve() / ("sos-outage-" + uuid.uuid4().hex)
    lab.mkdir(parents=True)
    report = {"source": source, "status": "failed", "rounds": [], "cleanupErrors": [],
              "scope": "Real single-MySQL and Go process failures, signed synthetic SOS and a loopback TLS test gateway"}
    print(str(lab), flush=True)
    fixture = Fixture(args.mysql_bin.resolve(), repo, lab)
    try:
        fixture.initialize()
        report["mysqlVersion"] = fixture.sql("SELECT VERSION()", schema=False)
        for index in range(1, args.rounds + 1):
            report["rounds"].append(fixture.database_and_crash_round(index))
            print(f"Database outage and Go crash {index}/{args.rounds} passed", flush=True)
        report["concurrent"] = fixture.concurrent(2 * args.rounds)
        print("100 concurrent signed SOS submissions and 100 concurrent retries passed", flush=True)
        report["gateway"] = fixture.gateway(args.gateway_rounds)
        current = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip()
        require(current == source and not subprocess.check_output(
            ["git", "status", "--porcelain", "--untracked-files=no"], cwd=repo), "Source changed during the drill")
        report["status"] = "passed"
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        for cleanup in (fixture.stop_backend, fixture.stop_mysql):
            try:
                cleanup()
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                report["cleanupErrors"].append(str(error))
                report["status"] = "failed"
        if fixture.mysql is not None and fixture.mysql.poll() is None:
            # The child handle belongs to this run even if the port has been taken by another server.
            try:
                fixture.mysql.kill()
                fixture.mysql.wait(timeout=20)
            except (OSError, subprocess.SubprocessError) as error:
                report["cleanupErrors"].append(str(error))
        (lab / "summary.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    require(not report["cleanupErrors"], "A fixture process did not shut down cleanly")


if __name__ == "__main__":
    main()
