# Yihe-Next JetStream transport experiment

Related design: [Issue #18](https://github.com/qiyuhuating/yihe-health/issues/18).

This is a standalone Go module for synthetic, disposable tests. It is not linked
to the current server, desktop installer, device API or production Compose.
Do not connect it to production data or credentials.

## Transport contract

- Provision the SOS and telemetry lanes on independent broker deployments.
  The test uses separate broker instances, ports, disk directories and
  connections on one machine; this is not separate hardware or availability zones.
- Streams use file storage, explicit capacity limits, discard-new overflow,
  work-queue retention and explicit consumer acknowledgements. There is no
  age-based deletion of unacknowledged events.
- Publish waits for the broker ACK. This confirms queue acceptance, not MySQL
  persistence or delivery to a caregiver. Single-replica ACKs are not quorum
  durability. Replica count is explicit.
- Byte-identical retries use a SHA-256 message ID for broker deduplication
  within two minutes. The caller retains the original encoded event on
  uncertain results. This window is not a business idempotency guarantee.
- The commit callback must succeed before the consumer sends a confirmed ACK.
  A failed commit causes redelivery. A lost acknowledgement can also cause
  redelivery after a successful commit.
- Before production integration, the authenticated producer must bind event
  identity, type, device, epoch, request ID and ciphertext to the request.
  The durable business sink must transactionally reject a conflicting reuse of
  an identity and apply a matching event only once. Stale binding epochs and
  revoked authorization must never be applied.
- The work queue deletes acknowledged messages; it does not provide an audit
  history. Auditable receipts and replayable projections require a separate
  durable history contract.

## Verification

From this directory using Go 1.27.4:

```powershell
go test -count=1 -v -timeout 90s ./...
go vet ./...
```

The tests start real NATS servers bound to loopback using disposable directories
with synchronous file-store writes enabled. They verify queue saturation,
independent SOS admission, duplicate publish acknowledgement, graceful restart
recovery and redelivery after a failed callback. A three-node local cluster test
shuts down its actual stream leader, consumes the acknowledged event after
reelection, confirms a new publish, then removes the quorum and checks that a
minority node cannot acknowledge another publish.
Broker-only tests need no MySQL, credentials or notification provider.

## Transactional MySQL sink

The module uses a local Go workspace containing both the experiment and the
existing backend, so Go verifies downloaded dependencies without treating the
local backend as a downloaded archive. It reuses resident-first/device-second row locking, the production daily
sample repository, heartbeat persistence and authenticated encryption. A trusted
producer seals device, resident, binding epoch, authorization generation,
request ID and low-value telemetry together. Device authentication itself
is not implemented in this experiment.

The disposable integration fixture installs separate receipt and rejection
tables so existing HTTP admission receipts are not mistaken for business
commit receipts. The primary key is (device ID, binding epoch, request ID).
The receipt stores a keyed digest of canonical event content using the
existing securestore lookup key (which must stay stable across key rotations).
The transaction checks binding, enabled status and authorization generation
before reading receipts or updating data. The test-only generation ledger and
device triggers increment on binding, status, credential and lifecycle changes,
so disable/re-enable and rotation cannot revive an old event.

Matching receipts are no-ops; conflicting content and stale authorization
are durably rejected before broker ACK. Failed commits and unauthenticated
ciphertext do not receive an ACK. No default production migration or HTTP
route is changed. The generation ledger would require backfill, deployment
and writer-cutover review before use with real existing devices.

To run the integration test, set TEST_MYSQL_DSN to a disposable MySQL 8.4
server with permissions to create/drop temporary schemas, then run the same
test command. Never point the fixture at production. The test applies the
actual backend migrations, injects receipt failure, verifies rollback,
re-delivery, keyed idempotency, conflict and authorization invalidation.

The [isolated CI workflow](../../.github/workflows/next-jetstream.yml) runs
these tests with the race detector. The NATS server and client are Apache-2.0
licensed dependencies; this experiment does not vendor or ship their binaries.

Not covered: production producer authentication, vital signs or SOS business
consumption, shared production receipt namespace/cutover, encryption-key
migration, abrupt process or power loss, separate availability zones,
throughput/P99, or real notification delivery.
