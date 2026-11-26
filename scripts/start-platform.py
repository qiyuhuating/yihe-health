"""Generate local deployment secrets once, then build/start the complete stack.

Never replaces existing keys or passwords. --validate only checks Compose config.
"""
import base64
from pathlib import Path
import secrets
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
folder = root / "deploy/secrets"
folder.mkdir(parents=True, exist_ok=True)
folder.chmod(0o700)
for name, value in {"data-key": base64.b64encode(secrets.token_bytes(32)).decode(), "metrics-token": secrets.token_hex(32), "bootstrap-token": secrets.token_hex(32)}.items():
    target = folder / name
    if not target.exists():
        target.write_bytes((value + "\n").encode())
    # The private host directory blocks traversal by other host users. Read-only
    # secret mounts must also be readable by the app's unprivileged container UID.
    target.chmod(0o444)
env = root / "deploy/platform.env"
if not env.exists():
    values = {"MYSQL_ROOT_PASSWORD": secrets.token_hex(24), "MYSQL_PASSWORD": secrets.token_hex(24), "CARE_RECEIPT_SECRET": secrets.token_hex(32), "APP_PORT": "8080", "PROMETHEUS_PORT": "9090"}
    env.write_bytes(("\n".join(key + "=" + value for key, value in values.items()) + "\n").encode())
    env.chmod(0o600)
command = ["docker", "compose", "--env-file", str(env), "-f", str(root / "deploy/platform.compose.yml")]
subprocess.run(command + ["config", "--quiet"], check=True)
if "--validate" not in sys.argv:
    # First bring up the authoritative MySQL business plane and monitoring.
    # All persistent business services now require only MySQL.
    subprocess.run(command + ["up", "--build", "-d", "--wait", "mysql", "app", "gateway", "prometheus"], check=True)
    print("颐和：http://127.0.0.1:8080 ；Prometheus：http://127.0.0.1:9090")
    print("首次管理员：python scripts/bootstrap-admin.py；密钥与数据卷必须独立备份。")
