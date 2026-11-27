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

## Native JetStream producer (test only)

The --native-outbox-test mode requires the signed step-report subset plus
a valid requestId. The native admission transaction reuses the locked
credential/binding validation and nonce INSERT, then reads the existing
experimental authorization generation under lock. It commits a keyed
semantic fingerprint and a Go-compatible AES-256-GCM sne1: envelope in
rust_step_outbox. A 202 response means durable SQL admission only, not
broker delivery or business commit. The fixture installs the schema and
uses an unversioned 32-byte test key shared with Go; keyring migration
and production key management are not implemented.

A retry with a fresh nonce and identical semantic identity/payload reuses
the original admission, including timestamps and exact ciphertext; a
changed payload or authorization generation with the same device/epoch/
requestId is rejected. Exact nonce replay remains a conflict. Each device
may have at most 64 pending records; indexed pending queries avoid scanning
published history. The published request fingerprint stays in SQL for
admission idempotency. Global quotas and safe retention/pruning remain
production prerequisites.

The --dispatch-one-test LOOPBACK:PORT mode selects one pending row and
publishes via async-nats to the existing telemetry subject, with the
SHA-256 of the stored bytes as Nats-Msg-Id and the expected stream header.
Only a confirmed PubAck permits an exact-byte conditional SQL update to
clear the ciphertext and record publication. No SQL lock or transaction
is held during broker I/O. --dispatch-crash-test exits with code 77
immediately after a real PubAck and before the SQL update, exclusively
for testing. Re-running dispatch republishes the same bytes; the Go
consumer checks the live authorization generation and atomically records
its durable commit/rejection before ACK. Broker dedup is finite, so the
consumer receipt remains the final business fence. Delivery is at-least-
once, not a claim of exactly-once distributed transport.

The integration test in experiments/jetstream uses a real sync-always
NATS server and the production MySQL migrations plus disposable experiment
tables. It verifies admission rollback, payload authentication, replay,
conflicting request IDs, unavailable broker, process exit after PubAck,
redelivery and revoked generation. Set YIHE_NEXT_GATEWAY_BINARY to the
built Rust binary, YIHE_NEXT_WATCH_BINARY to the built opt-in simulator,
and TEST_MYSQL_DSN to a disposable database.

The --dispatch-worker-test LOOPBACK:PORT mode reuses a SQL connection
pool and NATS connection while publishing pending rows. It immediately
continues when there is work, backs off on idle (100 ms to 2 s) and failure
(250 ms to 5 s), and reconnects on failures without clearing pending rows.
Ctrl-C stops between attempts. The integration test observes the actual
NATS server connection count: three publications, including one after an
idle period, use one new connection. Automatic recovery after an actual
broker restart and graceful shutdown latency are not separately verified.
Global quotas, poison quarantine, production TLS/ACLs, key rotation and
operational cutover remain future work.

## Protobuf / Ed25519 device experiment

The same --native-outbox-test listener exposes POST /api/v2/device/step
with application/x-protobuf. The shared experiments/device-wire crate
defines step.proto and the canonical signed encoding. The version-1
packet is limited to 1024 bytes and contains only a step observation,
device number, binding epoch, request ID, nonce, and signed/observed
Unix seconds. Ed25519 authenticates the domain-separated original
payload; unknown/duplicate fields and noncanonical encodings are
rejected. This intentionally requires canonical encoding rather than
accepting all valid Protobuf encodings.

An enrolled public key in the disposable rust_device_key table is bound
to the current broker authorization generation. Rust verifies before
locking, then verifies the current key again under the resident/device
and generation locks. The same nonce namespace and encrypted Outbox
admission transaction are used by both HMAC JSON and Ed25519 requests.
A generation change invalidates the enrolled key, even after re-enable.
Enrollment is fixture-only; no real device provisioning, hardware
keystore, production key lifecycle, QUIC/MQTT or firmware migration
is implemented.

Build the opt-in simulator from watch-sim with
cargo build --locked --features next-wire --bin watch-sim-next.
Its --public-key / --send commands use a 32-byte hex test seed provided
through WATCH_NEXT_SEED_HEX, and only permit loopback endpoints.
The existing default simulator binary and production routes are unchanged.
The integration test sends a real simulator packet and independently
encodes and signs Protobuf in Go, exercises tampering and cross-protocol
replay, then consumes the actual broker events. See step.proto for tags.

## Native JetStream consumer (test only)

--consume-one-test LOOPBACK:PORT fetches one event from the existing
YIHE_telemetry / commit durable consumer. --consume-worker-test reuses
the MySQL pool, event key and NATS connection, pulls one event at a time,
and retries errors with a delay. --consume-crash-test exits with code 78
after SQL COMMIT, before the broker acknowledgement.

The supported wire subset is step-only with UTC timestamps, the existing
sne1 AES-GCM test envelope and Go's canonical JSON / HMAC receipt digest.
Unsupported metrics or invalid events stay unacknowledged (delayed NAK). One-shot test consumption waits up to 35 seconds for a redelivery when a previous process lost its asynchronous NAK; transaction errors still exit immediately.
The consumer obtains resident, device and authorization generation locks
in that order, checks live binding/status, consults the existing rejection
and receipt tables, and commits the day metric, heartbeat and receipt in
one transaction. Only a confirmed business commit, matching receipt or
durable terminal rejection permits DoubleAck. The real fault test injects
receipt failure, exits the Rust process after COMMIT, waits for actual
broker redelivery, then consumes via Go; resealed Go events also replay
safely through Rust. The simulator integration test exercises the native
admission/publish/consume chain and replays the same event through Go.

This is not a production consumer deployment. It does not handle sleep,
vital samples, SOS, keyring rotation, poison quarantine, or notification
delivery. The shared lane cannot be cut over to this subset while it
contains unsupported events. The consumer does not manufacture heart-rate
observations from step events.

## Independent vital event experiment

The dedicated YIHE_vitals stream (subject yihe.next.vitals, durable
commit) is provisioned by the disposable integration fixture, separately
from YIHE_telemetry. --consume-vital-one-test and
--consume-vital-worker-test reuse the existing bounded pull/ack framework.
Version 1 encrypts the entire JSON event with AAD
yihe-next-vitals-event-v1. Its fields are version, device_id,
resident_id, binding_epoch, auth_generation, request_id, received_at,
observed_at, hr and bp. Timestamps are UTC, observed_at is in integral
seconds; hr (20..250) and bp (40..300) are nullable integers with at least
one present. BP is the existing scalar measurement, not a systolic/
diastolic pair. POST /api/v2/device/vitals accepts the independent Vital
Protobuf message signed with the domain yihe-next-vital-wire-v1 + NUL.
It uses the same enrolled key, generation and nonce transaction as
the step endpoint, but writes rust_vital_outbox with a separate keyed
fingerprint and encrypted event. The --dispatch-vital-one-test and
--dispatch-vital-worker-test modes target YIHE_vitals only. Each device
has 64 pending vital events. A 202 response is SQL admission only.
Public production routing, provisioning and firmware remain pending.

Under resident/device/generation locks the native consumer validates
binding time, current epoch and generation, and checks a separate keyed
request receipt/rejection. It uses the real watch_info thresholds and
ratepre table; the existing vital-measurements:<id> encrypted format
binds owner and device, leaving legacy hr/bp columns NULL. The original
alert snapshot's OR semantics preserve unresolved flags. Measurement,
snapshot, heartbeat and receipt with a per-binding revision all commit
together. Only then may the broker be acknowledged. A Go repository
differential verifies the ciphertext via the real Go reader.

--vital-stats-test DEVICE_ID is a loopback/disposable-schema local
projection replay, not an authenticated production analytics endpoint.
It locks the current resident/device, loads up to 256 latest receipts
for the current epoch, authenticates each referenced committed ratepre
row, checks revision continuity, and incrementally replays ordered heart
rate events into RollingHeartRate. Historical insertions or BP-only
revision advances rebuild the bounded projection; after rebind old
receipts are not included. The receipt revision is specific to this
experiment, not the production health_sample clock. The window is the
selected receipt set rather than the complete historical time series.
--vital-stats-worker-test DEVICE_ID maintains this bounded ring for one
device, using an indexed revision probe and idle backoff (100ms to 2s).
Only changed revisions acquire locks and load new committed receipts.
Ordered samples append and expire the oldest sample at capacity;
historical order, equal-time eviction and BP-only events may rebuild.
Identity changes reset the cache; errors invalidate it. The worker
emits diagnostic JSON, not clinical decisions or a production read
API. Its receipt history is append-only; production corrections and
health_sample integration are not supported.

### Independent SOS consumer experiment

--consume-sos-one-test and --consume-sos-worker-test use the existing
YIHE_sos stream and its independent broker/pool process. The trusted
producer fixture seals version, device/resident identity, binding epoch,
authorization generation, request ID and receive timestamp under
yihe-next-sos-event-v1. This subset accepts no coordinates. The consumer
checks the live binding/generation and keyed receipt/rejection; it also
consults the actual Go care_incident device/request retry identity.
The transaction writes alarm_precess, location alert snapshot, heartbeat,
care_incident, care_incident_event, care_notification_outbox and
message_outbox plus rust_sos_receipt. Family phone authentication and
recipient encryption/index use the original Go domains. A valid
recipient gets a pending SMS row and an available in-app message.
No notification sender is started. A broker ACK is not delivery or
human acceptance; the consumer acknowledges only after SQL commit.
--consume-sos-crash-test exits after COMMIT before broker ACK.
Go retry lookup and protected notification readers validate the actual
native records. A dedicated --native-sos-test listener exposes only
POST /api/v2/device/sos, with its own four-connection SQL pool and four
admission permits, separate from the ordinary eight-permit process.
The canonical Sos Protobuf is signed with Ed25519 under the
yihe-next-sos-wire-v1 + NUL domain. The same enrolled-key, epoch,
generation and nonce fence applies. Its separate rust_sos_outbox uses
the Go device/request incident identity (including historical epochs)
and a per-device pending limit of 64. The --dispatch-sos-one-test,
--dispatch-sos-worker-test and --dispatch-sos-crash-test modes target
the independent YIHE_sos deployment. The real HTTP test exhausts eight
ordinary body-reading permits, observes 503 for another ordinary
request, and confirms SOS admission and durable incident commit.
This proves process/pool/permit isolation, not isolation from shared
MySQL locks, disk or host scheduling. Location reporting, notification
delivery, production schema and routing remain pending. The current
production Go SOS path is unchanged.

### Native rule transaction differential

--rule-commit-test reads trusted local JSON observation fixtures from
stdin in a disposable schema; it is not an HTTP route. It evaluates
the seven rule candidates via Yihe-Core with the default policy,
uses the supplied Go-compatible observation keys/evidence fields,
locks resident/device and reads live binding, then applies the
native event-keyed state transition to the existing rule_state.
Incident, encrypted evidence, event note, both original notification
outboxes and an experimental keyed batch receipt share one SQL commit.
Actual Go persistence is compared on an equivalent device for all
seven rules, including recovery, source change, duplicate and receipt
failure rollback. Existing states for one device/epoch are locked in
primary-key order with one SELECT and changed states use one multi-row
UPSERT. Newly activated incidents keep their individual database-issued
IDs, while event notes and notification outboxes are batched. The
family recipient is authenticated once per batch; each outbox row
uses a fresh encrypted envelope. All writes remain in one transaction.
Trusted fixtures may not substitute for authoritative observations.

### Native authority snapshot

--rule-authority-test reads local JSON lines [device_id, now_ns] only
in a disposable schema. It loads current binding, the actual
health_owner_clock revision (not the global allocation high-water
mark), rule_policy revision/payload or watch_info defaults, and
rule_task eligibility/due time. Latest heart-rate, systolic pressure
and raw steps observations use the three indexed Go selection ranges.
It resolves the real health_sample history reference through
health_change, authenticates the revision/owner/sample/device/epoch
identity, and also supports the inline protected sample format.
All reads share a REPEATABLE READ, READ ONLY transaction; a missing
or inconsistent protected observation makes the snapshot unavailable.
The actual Go differential includes rollback, corrected samples,
custom policy and due-task state, and missing-history rejection.

This is a snapshot reader, not a clinical decision engine or task
claimer. It does not resolve timezones, collect baseline/activity
windows or legacy vital fallbacks, upgrade night incidents, fence a
subsequent mutating rule transaction, or reschedule rule_task.
Production writers can use either device-first or experimental
owner-clock-first locking; do not add a blocking clock lock after
the device lock without an agreed migration strategy.

### Fenced hybrid rule scheduler

--rule-scheduler-test runs one due-index tick per local cutoff JSON line.
--rule-scheduler-worker-test runs a persistent indexed-head worker, with
100 ms to 2 s idle/failure backoff and a bounded two-second wakeup probe
when idle. Neither mode scans the device inventory or claims tasks in
memory. A fixed cutoff and (due_at, device_id) keyset pagination prevent
failed early tasks from starving later candidates within a tick.

For each task, Rust first reads an authenticated production-format
snapshot in a short read-only REPEATABLE READ transaction. It computes
an ephemeral input fingerprint from the shared native_rule_fence.sql.
The read transaction and pool connection are released before Rust sends
the challenge-bound request over the trusted local pipe. The build-tagged
Go CollectNativeRuleInput adapter independently checks that fingerprint
inside its own read-only REPEATABLE READ snapshot, then uses the actual
Go policy, baseline, legacy vital, activity-window and IANA-timezone
algorithms. A mismatched snapshot returns a retryable stale response.
The ten-second IPC deadline holds no Rust database connection or write
locks. Rust validates the response and computes candidates with the
actual policy and authenticated latest HR/BP and battery values before
opening the write transaction.

The final short transaction acquires the resident, current device,
policy/watch thresholds, exact eligible due task and existing owner clock
with FOR UPDATE NOWAIT. The owner-first writer's clock never blocks while
the scheduler owns the device. It checks binding, owner revision and the
same fingerprint again before writing. In addition to health/policy
revisions, the fingerprint includes heartbeat, battery, legacy latest
measurement identity/cipher digest, baseline replay keys, active SOS/fence
presence and task state; this covers changes even when an already-overdue
due_at stays unchanged. The protocol relies on existing ordinary writer
locks and revision contracts, not arbitrary out-of-band maintenance.
A missing clock, mismatched snapshot or busy row leaves the task durable.
The native decisions are cross-checked with Go evidence. Rule state,
incident, protected evidence,
notification outboxes, test receipt and exact due-task advancement commit
together; disabled policies disarm their due row. No notification sender
runs. The task remains unchanged on rejection or rollback. Night
SOS/fence upgrades and missing clocks are reported as fallback, not
silently consumed; Go retains responsibility for those cases.

--rule-scheduler-crash-test exits with 79 after the SQL commit but before
reporting the tick. The tagged integration fixture exercises this mode and
the worker protocol; it is not a production scheduler supervisor. All
modes are restricted by the existing disposable-schema/loopback gate.
Production deployment still requires a trusted adapter supervisor,
explicit fallback handoff, full native window/timezone and night incident
migration, bounded load and failure drills, and an agreed lock/writer
ownership plan. The integration test withholds the collector reply for
500 ms while executing the actual authenticated Go SOS business workflow
(with synthetic nonce/persistence hooks) and verifies that all relevant
write fences remain available, SOS commits its incident/outbox, and the
now-stale rule snapshot cannot commit. In one local MySQL 8.4.13 durable
test the SOS transaction took 6.86 ms. This is a controlled single
observation, not a full HTTP or high-concurrency P99 benchmark.
Sustained conflicting updates may defer optimistic attempts; mixed-load
starvation, lock hold durations, CPU/RSS and retry budgets still need
measurement.

### Rule transaction workload

The optional YIHE_NEXT_RULE_BENCH=1 tagged Go integration test
TestNextCoreRulePersistence runs three alternating trials of 128
batches (seven rule candidates each). Use an optimized release Rust
binary via YIHE_NEXT_GATEWAY_BINARY and an isolated MySQL instance
with durable commit settings. Both paths lock resident/device,
evaluate the same inputs, update the actual rule tables/notifications
and commit a test receipt. Go is in-process and Rust includes the local
JSON pipe, so the comparison is not a pure language or full-backend
benchmark. The test reports committed batches/s, P50 and P99.

Historical baseline (86f13ed), measured on Ryzen AI 9 H 365, Go 1.27.4, release Rust 1.100.0,
MySQL 8.4.13 with innodb_flush_log_at_trx_commit=1 and sync_binlog=1.
One serial request at a time; 384 committed batches per runtime,
seven candidates per batch. Median of the three per-trial results:

| Runtime | Batches/s | P50 (ms) | P99 (ms) |
| --- | ---: | ---: | ---: |
| Go | 128.08 | 7.6023 | 14.9434 |
| Rust | 120.19 | 7.6604 | 20.4964 |

The historical Rust fixture includes pipe/JSON overhead and was not
faster than the Go control.

First ABBA rerun: A was the frozen 86f13ed release, B batched only
rule-state reads/writes. Six trials of 128 batches per variant.
Medians were 121.345 vs 119.585 batches/s and 18.8271 vs 26.94375 ms
P99. The state-only change did not demonstrate a stable speedup.

Final ACCA rerun: A was the same frozen release, C also batched event
and notification writes. Same workload and durable MySQL settings,
fresh migrated schema per group, six trials of 128 batches per
variant. The per-trial medians were:

| Runtime/group | Batches/s | P50 (ms) | P99 (ms) |
| --- | ---: | ---: | ---: |
| Go control (A group) | 100.105 | 9.4922 | 19.8745 |
| Rust A | 97.17 | 9.45365 | 26.10265 |
| Go control (C group) | 100.10 | 9.4894 | 19.4436 |
| Rust C | 118.69 | 8.158 | 14.08855 |

C improved the median throughput 22.15% and P99 46.03% versus A;
relative to the contemporaneous Go control, C throughput was 18.57%
higher and P99 27.54% lower. All groups passed persisted-state,
notification and receipt checks. Individual C P99 trials ranged
from 12.849 to 31.6315 ms; this is a bounded serial transaction
comparison, not proof of stable production or mixed-load tail
latency. The median paired C/Go throughput ratio was 1.1851,
compared with 0.9173 in the A groups. No full-service CPU/RSS data.
Next isolate connection acquisition, lock wait, query execution,
COMMIT and IPC under equivalent concurrent loads.

## Native analytics core

yihe-core::rolling implements a bounded integer heart-rate ring with
separate timestamp/value columns, histogram/bitset extrema, exact sum
and second moment. A fused append returns statistics with identity and
revision validation; unsupported fractional, corrected, out-of-order or
missing-revision data require an authoritative rebuild. Coverage extension
requires the caller's proof of completeness. Existing immutable SIMD
snapshot queries remain available.

yihe-core::rules evaluates seven compact rule candidates and models
observation-keyed transitions. The local oracle compares 10,240 rolling
windows and 1,200 candidate sets to the actual Go implementation; a
disposable MySQL test also compares 16 transitions with persisted Go
rule_state and incident creation. This is not production rule execution:
timezone resolution, evidence, policy loading, incident/outbox writes and
notification dispatch remain with Go. The step-only broker consumer is
not a source of heart-rate data.

Reproducible microbenchmarks: from backend, go test -tags
yihe_next_gateway -run '^$' -bench '^BenchmarkNextGo(Window|RollingControl)$'
-benchmem -cpu 1 -benchtime 6x; from yihe-core, cargo run --locked
--release --example rolling_bench -- 6. On the local Ryzen AI 9 H 365,
three interleaved trials of 4096 observations per iteration gave the
following medians (ns/event, excluding setup and database/network I/O):

| Window | Go Aggregate | Rust snapshot | Rust rolling | Go rolling control |
| --- | ---: | ---: | ---: | ---: |
| 64 | 467.2 | 58.740 | 20.500 | 5.518 |
| 256 | 1720 | 52.112 | 20.866 | 5.086 |
| 1024 | 6256 | 119.828 | 21.004 | 5.168 |
| 4096 | 24877 | 386.845 | 21.061 | 5.192 |

Go 1.27.4 and Rust 1.100.0; the test-only Go rolling control omits
identity/revision/value validation, so this is not a language speed
comparison. No full-service throughput, P99, CPU or RSS claim.

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

Next: connect the native rule transaction to authoritative committed observations and production policy/scheduler revisions, extend device and SOS location contracts, harden dispatch, notification delivery and key provisioning, then compare whole-service performance and fault recovery.
Do not place this service on a production ingress before those gates.
