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

## S3 encrypted image shadow migration

The experiment uses an actual S3-compatible client with RustFS. The source
catalog and active ciphertext are read together and authenticated against the
existing image-name AAD, plaintext SHA-256 and size. An object key is the SHA-256
of the exact ciphertext, prefixed with images/. PUT is single-part and uses
If-None-Match: *; retries verify existing bytes rather than overwrite. The
download is bounded to the existing 8 MiB encrypted storage limit. No ETag is
treated as a content digest.

Only after GET and authentication succeed does a short MySQL transaction
acquire the existing backup guard, catalog and payload locks, recheck the
source generation and bytes, and commit the shadow reference. Source BLOBs
are retained. A lost response or ledger failure is restartable; an object
without a ledger row is retained for future reconciliation, not automatically
deleted. The test-only ledger is scoped to one dedicated bucket. No online
GC, dual-write or production read path is installed.

CopyReady checks current source business references and paginates the ready
catalog with a positive row limit. This is a restartable bounded scan, not a
consistent whole-inventory cutover: production must quiesce/fence writers and
reconcile changes and references before switching. Verify compares the current
source, ledger, and downloaded authenticated bytes; it does not authorize
serving the shadow object in place of MySQL.

The real integration test requires TEST_MYSQL_DSN plus TEST_S3_ENDPOINT,
TEST_S3_ACCESS_KEY and TEST_S3_SECRET_KEY, all pointing to disposable test
services. The endpoint is host:port and plaintext HTTP is restricted to the
isolated test environment. The fixture creates a random dedicated bucket and
removes it afterward. CI downloads the pinned RustFS 1.0.7 release, checks its
SHA-256, runs it on loopback, and executes the integration test. The test covers
the 5 MiB plaintext limit, failure after upload, retries, key rotation, stale
source rejection, remote corruption, conditional-write collision and missing
business references.

## Valkey session shadow migration

The experiment preserves the existing encrypted session payload and cookie
hash, using a dedicated Valkey key namespace and SET NX PXAT with the original
absolute expiration. A retry verifies the existing ciphertext and expiry.
MirrorActive performs a keyset-paginated backfill without holding SQL cursors
across Valkey operations. No plaintext session identifier or CSRF token is
published outside the original authenticated ciphertext.

The disposable fixture installs a narrow MySQL metadata projection containing
the cookie hash, role, actor ID, expiration and ciphertext digest. Triggers
maintain the projection in the same transaction as the real web_session
insert/update/delete. Backfill is performed on a quiesced fixture; a real
rollout needs fencing, reconciled backfill and migration review.

Lookup fetches ciphertext from Valkey, then checks the live MySQL metadata
and enabled two-factor generation before authenticating the payload.
Authorization is never decided by cache contents or TTL alone. No plaintext
payload column is read from MySQL on this experimental lookup path, but each
lookup still performs a SQL round trip. A stale value restored by a cache
replica cannot bypass a committed SQL revocation. An unavailable cache or
authority returns no session, and there is no cache-only fallback. No claims
are made about production performance, cross-AZ availability, or revocation
of already executing requests.

Set TEST_MYSQL_DSN, TEST_VALKEY_ENDPOINT and TEST_VALKEY_PASSWORD to
disposable test services. The real Valkey 9.0.3 test covers preexisting and
new sessions, bounded backfill, repeat migration, production session field
parity, absence of plaintext tokens, rollback of a revoke transaction,
single-session and actor-wide revocation while stale ciphertext remains,
factor-generation change, corruption, SQL expiration, and SQL connection
closure. CI runs an official digest-pinned Valkey image on loopback.

Not covered: production producer authentication, vital signs or SOS business
consumption, shared production receipt namespace/cutover, encryption-key
migration, abrupt process or power loss, separate availability zones,
throughput/P99, or real notification delivery.
