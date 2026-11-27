# Yihe-Next Rust gateway experiment

This standalone crate uses Axum, Tokio, reqwest and the existing yihe-core
as a normal Rust dependency. It is not linked to the production deployment,
desktop package or device simulator. It is a migration scaffold, not a replacement for the Go backend. An isolated native step-report writer now exercises the first complete Rust business transaction.

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

## Native step-report writer (test only)

The --native-step-test mode serves only the existing report path on loopback
and connects only to a disposable loopback yihe_test_<8 hex digits> schema.
SQLx uses a bounded connection pool, READ COMMITTED and UTC SQL sessions.
The strict signed-only subset requires deviceNo, secret, bindingVersion,
beTime (UTC wall-clock format), and integer step; other fields, including
requestId, vitals, location, alarm and sleep, are rejected. This deliberately
does not implement legacy coercion, the full report parser, Go's local wall
clock, unsigned firmware or numbered report receipt semantics.

Rust verifies HMAC over the original bytes and the timestamp/nonce contract,
compares the existing domain-separated device credential hash in constant
time, reads the binding, then takes resident/device locks in the existing
order and rechecks the live credential, status and epoch. The existing
device_request_nonces unique key is shared with Go. Step upsert preserves
newest observations, archives previous binding data, and updates the
heartbeat in the same transaction as the nonce. An error rolls back all
writes; a successful response is emitted only after COMMIT. Unknown SQL
errors are not exposed to the caller. This mode is isolated from relay mode.

The native differential test compares real Go and Rust responses for the
supported subset, verifies persisted rows across rebindings, exercises
Go-to-Rust and Rust-to-Go nonce replay on one schema, concurrent same-nonce
requests, injected write failure and retry, and observes a MySQL row-lock
wait before revoking authorization to prove locked revalidation. MySQL
password authentication uses the SQLx MySQL RSA feature on the disposable
loopback connection; TLS and restricted production credentials are future
deployment requirements.

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
go test -tags yihe_next_gateway -run '^TestNextGateway(NativeOracle|HTTPDifferential|NativeStep)$' -count=1 -v -timeout 240s ./internal/handlers
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

Next: extend native wire authentication and transactional business handling beyond the step-only subset; typed binary protocol and simulator integration, safe handoff to the durable event lanes, actual performance comparison and end-to-end fault testing.
Do not place this service on a production ingress before those gates.
