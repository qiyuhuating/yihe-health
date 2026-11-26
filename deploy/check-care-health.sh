#!/bin/sh
# Read-only local smoke probe for the independently deployed care API.
# Never treat HTTP reachability alone as proof of SOS delivery or rescue.
set -eu
BASE_URL="${YIHE_HEALTH_URL:-http://127.0.0.1:8080}"
if command -v systemctl >/dev/null 2>&1; then
  systemctl is-active --quiet yihe-care || { echo "care systemd unit not running" >&2; exit 2; }
fi
curl --fail --silent --show-error --max-time 5 "$BASE_URL/healthz" >/dev/null
curl --fail --silent --show-error --max-time 5 "$BASE_URL/api/v1/service/status" \
  | python3 -c 'import json,sys; obj=json.load(sys.stdin); sys.exit(0 if obj.get("databaseReady") is True else 1)'
echo "HTTP ready and MySQL primary connected; verify notification workers, outbox and staffing separately"
