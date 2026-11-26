#!/usr/bin/env python3
"""Isolated real-process watchdog drill; never targets existing services."""
import argparse
import hashlib
import hmac
import json
import os
from pathlib import Path
import secrets
import socket
import sqlite3
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import urllib.error
import urllib.request
import uuid


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prometheus", required=True, type=Path)
    parser.add_argument("--alertmanager", required=True, type=Path)
    parser.add_argument("--report-dir", required=True, type=Path)
    parser.add_argument("--allow-dirty", action="store_true")
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=no"], cwd=repo))
    dirty = dirty or bool(subprocess.check_output([
        "git", "status", "--porcelain", "--", "scripts/reliability/watchdog-process-drill.py"], cwd=repo))
    require(not dirty or args.allow_dirty, "Formal drill requires clean committed source")
    for binary in (args.prometheus, args.alertmanager):
        require(binary.is_file(), "Both real monitoring binaries must exist")
    lab = args.report_dir.resolve() / ("watchdog-process-" + uuid.uuid4().hex)
    lab.mkdir(parents=True)
    state = lab / "watchdog.sqlite"
    flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
    report = {"source": source, "dirty": dirty, "status": "failed", "phases": [], "tools": {},
              "scope": "Real Prometheus, Alertmanager and production watchdog in independent owned processes; loopback controlled receiver; no external notification or host power-loss claim"}
    started = time.monotonic()
    processes, handles = [], []
    lock = threading.Lock()
    receiver_state = {"broken": True, "rejected": 0, "accepted": [], "errors": []}
    token = secrets.token_hex(32)

    class Receiver(BaseHTTPRequestHandler):
        def log_message(self, *unused):
            pass

        def do_POST(self):
            try:
                require(self.path == "/alerts", "Unexpected receiver path")
                require(hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + token), "Receiver authentication failed")
                length = int(self.headers.get("Content-Length", "0"))
                require(0 < length <= 65536, "Invalid notification size")
                body = json.loads(self.rfile.read(length))
                require(body["receiver"] == "independent-watchdog", "Wrong event producer")
                event = {"id": self.headers.get("Idempotency-Key", ""), "status": body["status"],
                         "component": body["alerts"][0]["labels"]["alertname"]}
                require(event["id"].startswith("watchdog-"), "Missing stable event identity")
                require(event["status"] in ("firing", "resolved"), "Unknown event state")
                with lock:
                    broken = receiver_state["broken"]
                    if broken:
                        receiver_state["rejected"] += 1
                    else:
                        receiver_state["accepted"].append(event)
                self.send_response(503 if broken else 200)
                self.end_headers()
            except (ValueError, KeyError, RuntimeError) as error:
                with lock:
                    receiver_state["errors"].append(str(error))
                self.send_response(400)
                self.end_headers()

    receiver = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
    receiver.daemon_threads = True
    thread = threading.Thread(target=receiver.serve_forever, daemon=True)
    thread.start()
    prom_port, am_port = port(), port()
    require(prom_port != am_port, "Ephemeral monitor ports collided; rerun safely")
    (lab / "prometheus.yml").write_text("global:\n  scrape_interval: 1s\nscrape_configs: []\n", encoding="utf-8")
    (lab / "alertmanager.yml").write_text("route:\n  receiver: discard\nreceivers:\n  - name: discard\n", encoding="utf-8")

    def launch(name, command, env=None):
        log = (lab / (name + ".log")).open("ab")
        handles.append(log)
        process = subprocess.Popen([str(part) for part in command], cwd=lab, env=env,
                                   stdout=log, stderr=subprocess.STDOUT, creationflags=flags)
        processes.append(process)
        return process

    def stop(process):
        if process.poll() is None:
            process.kill()
            process.wait(timeout=15)

    def alive(*items):
        return all(item.poll() is None for item in items)

    def wait_for(predicate, message, live=(), timeout=35):
        deadline = time.monotonic() + timeout
        while True:
            require(alive(*live), "Owned process exited unexpectedly: " + message)
            with lock:
                require(not receiver_state["errors"], "Receiver rejected malformed event")
            if predicate():
                return
            require(time.monotonic() < deadline, "Timed out: " + message)
            time.sleep(0.15)

    def ready(number):
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{number}/-/ready", timeout=2) as response:
                return response.status == 200
        except (OSError, urllib.error.URLError):
            return False

    def rows():
        if not state.exists():
            return []
        with sqlite3.connect(state.as_uri() + "?mode=ro", uri=True, timeout=2) as database:
            if not database.execute("SELECT 1 FROM sqlite_master WHERE name='notification'").fetchone():
                return []
            return database.execute("SELECT id,event_status,status,attempts,group_key FROM notification ORDER BY rowid").fetchall()

    def snapshot():
        with lock:
            return list(receiver_state["accepted"])

    def phase(name):
        report["phases"].append({"name": name, "elapsedSeconds": round(time.monotonic() - started, 3)})

    def start_prom():
        return launch("prometheus", [args.prometheus.resolve(), "--config.file=" + str(lab / "prometheus.yml"),
                      "--storage.tsdb.path=" + str(lab / "prometheus-data"),
                      "--web.listen-address=127.0.0.1:" + str(prom_port), "--log.level=warn"])

    def start_am():
        return launch("alertmanager", [args.alertmanager.resolve(), "--config.file=" + str(lab / "alertmanager.yml"),
                      "--storage.path=" + str(lab / "alertmanager-data"), "--cluster.listen-address=",
                      "--web.listen-address=127.0.0.1:" + str(am_port), "--log.level=warn"])

    env = {key: value for key, value in os.environ.items() if not key.startswith(("WATCHDOG_", "ONCALL_"))}
    env.update(WATCHDOG_STATE=str(state), WATCHDOG_PROMETHEUS_URL=f"http://127.0.0.1:{prom_port}/-/ready",
               WATCHDOG_ALERTMANAGER_URL=f"http://127.0.0.1:{am_port}/-/ready", WATCHDOG_INTERVAL_SECONDS="1",
               WATCHDOG_FAILURE_THRESHOLD="2", ONCALL_WEBHOOK_URL=f"http://127.0.0.1:{receiver.server_port}/alerts", ONCALL_TOKEN=token)

    def start_watchdog():
        return launch("watchdog", [sys.executable, "-u", repo / "scripts/reliability/watchdog.py"], env)

    try:
        for name, binary in (("prometheus", args.prometheus), ("alertmanager", args.alertmanager)):
            version = subprocess.run([str(binary.resolve()), "--version"], capture_output=True, text=True,
                                     timeout=20, creationflags=flags, check=True)
            with binary.open("rb") as binary_file:
                digest = hashlib.file_digest(binary_file, "sha256").hexdigest()
            report["tools"][name] = {"version": (version.stdout + version.stderr).splitlines()[0], "sha256": digest}
        prom, am = start_prom(), start_am()
        wait_for(lambda: ready(prom_port) and ready(am_port), "real monitors ready", (prom, am))
        watchdog = start_watchdog()
        wait_for(lambda: state.exists(), "watchdog spool created", (prom, am, watchdog))
        time.sleep(2.5)
        require(not rows() and not snapshot(), "Healthy cold start must not send an alert")
        phase("healthy-cold-start")

        stop(prom)
        wait_for(lambda: any(row[1] == "firing" and row[2] == "pending" and row[3] >= 1 for row in rows()),
                 "Prometheus crash persisted despite receiver 503", (am, watchdog))
        require(not snapshot(), "Failed receiver must not count as acceptance")
        pending_id = rows()[0][0]
        phase("prometheus-crash-receiver-503")

        stop(watchdog)
        watchdog = start_watchdog()
        wait_for(lambda: any(row[0] == pending_id for row in rows()), "same spool survives watchdog crash", (am, watchdog))
        prom = start_prom()
        wait_for(lambda: ready(prom_port), "Prometheus restart", (prom, am, watchdog))
        wait_for(lambda: len(rows()) == 2 and rows()[1][1] == "resolved", "resolved persists behind pending firing", (prom, am, watchdog))
        require(all(row[2] == "pending" for row in rows()) and not snapshot(), "Resolved must not overtake undelivered firing")
        phase("watchdog-and-prometheus-restart-preserve-order")
        with lock:
            receiver_state["broken"] = False
        wait_for(lambda: len(snapshot()) == 2 and all(row[2] == "receiver_accepted" for row in rows()),
                 "receiver recovery drains durable spool", (prom, am, watchdog))
        phase("receiver-recovery")

        stop(am)
        wait_for(lambda: len(snapshot()) == 3, "Alertmanager crash detected independently", (prom, watchdog))
        am = start_am()
        wait_for(lambda: ready(am_port), "Alertmanager restart", (prom, am, watchdog))
        wait_for(lambda: len(snapshot()) == 4 and all(row[2] == "receiver_accepted" for row in rows()),
                 "Alertmanager recovery delivered", (prom, am, watchdog))
        time.sleep(2.5)
        events = snapshot()
        require([(event["component"], event["status"]) for event in events] == [
            ("PrometheusUnavailable", "firing"), ("PrometheusUnavailable", "resolved"),
            ("AlertmanagerUnavailable", "firing"), ("AlertmanagerUnavailable", "resolved")], "Unexpected delivery order or duplicate business event")
        require(len({event["id"] for event in events}) == 4 and len(rows()) == 4, "Restart must not duplicate durable events")
        phase("alertmanager-crash-and-recovery")
        report.update(status="passed", events=events, rejectedAttempts=receiver_state["rejected"],
                      spoolPending=0, humanAcknowledged=False)
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        cleanup_errors = []
        for process in reversed(processes):
            try:
                stop(process)
            except (OSError, subprocess.TimeoutExpired) as error:
                cleanup_errors.append(type(error).__name__)
        receiver.shutdown()
        receiver.server_close()
        thread.join(timeout=5)
        for handle in handles:
            handle.close()
        report["allOwnedProcessesStopped"] = all(process.poll() is not None for process in processes)
        if cleanup_errors or not report["allOwnedProcessesStopped"]:
            report["status"] = "failed"
            report["cleanupErrors"] = cleanup_errors or ["Owned process still running"]
        report["elapsedSeconds"] = round(time.monotonic() - started, 3)
        (lab / "summary.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        print(json.dumps({"status": report["status"], "source": source, "dirty": dirty,
                          "summary": str(lab / "summary.json"), "allOwnedProcessesStopped": report["allOwnedProcessesStopped"]}), flush=True)
        require(not cleanup_errors and report["allOwnedProcessesStopped"], "Owned process cleanup failed; inspect private summary")


if __name__ == "__main__":
    main()
