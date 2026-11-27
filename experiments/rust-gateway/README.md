# Yihe-Next Rust gateway experiment

This standalone crate uses Axum, Tokio, reqwest and the existing yihe-core
as a normal Rust dependency. It is not linked to the production deployment,
desktop package or device simulator. It is a migration scaffold, not a
replacement for the Go backend.

## Scope

- Loopback-only listener and fixed loopback upstream. The two routes
  POST /api/v1/device/report and POST /api/v1/device/sos forward the exact
  original body and device signature headers to Go. Authentication, nonce
  persistence, authorization and business commits remain in Go/MySQL.
- Separate nonblocking concurrency budgets (64 telemetry, 8 SOS) before
  reading bodies. Each incoming body is limited to 1 MiB + 1 byte and
  ten seconds; upstream requests have a 30-second timeout and response
  bodies are limited to 1 MiB. These are experimental limits, not tuned SLOs.
- No redirects, environment proxy, or automatic retry on an upstream POST.
  A lost/uncertain upstream response returns 502, never a success ACK.
  The gateway does not forward cookies, caller-supplied proxy headers, or
  connection-specific headers. It is not a complete production reverse
  proxy: remote address, middleware, rate limiting and transport policy
  need a separate cutover design. Gateway-specific overload/limit errors
  intentionally differ from the Go handler.
- A native Rust SOS shape-check candidate and a direct yihe-core committed
  heart-rate statistics call are exercised through a local stdin/stdout
  oracle, not exposed as public HTTP endpoints or used to authorize a
  request. The candidate does not implement signatures, position checks,
  replay protection or business logic. Full parser parity is not claimed.

## Verification

From this directory:

```powershell
cargo fmt --check
cargo clippy --locked --all-targets -- -D warnings
cargo test --locked --all-targets
cargo build --locked
```

Then from backend, set YIHE_NEXT_GATEWAY_BINARY to the absolute path of the
compiled binary and TEST_MYSQL_DSN to a disposable MySQL 8.4 instance with
temporary schema creation/deletion permissions. Run:

```powershell
go test -tags yihe_next_gateway -run '^TestNextGateway(NativeOracle|HTTPDifferential)$' -count=1 -v -timeout 180s ./internal/handlers
```

The tagged test calls the actual Go SOS parser and health.Aggregate, rather
than a duplicate reference specification. The HTTP test executes the actual
Go report/SOS handlers in two independently migrated MySQL schemas, one
direct and one through the Rust gateway. It compares response status and
JSON (except wall-clock serverTime), and counts of ratepre, care_incident,
and device_request_nonces. It uses real HMAC signatures over indented JSON
to verify byte preservation, and exercises nonce replay and idempotent SOS
retry. The separate Rust network tests simulate saturated admission and
an abruptly disconnected upstream.

Next: native wire authentication with real credential and nonce transactions,
typed binary protocol and simulator integration, safe handoff to the durable
event lanes, actual performance comparison and end-to-end fault testing.
Do not place this service on a production ingress before those gates.
