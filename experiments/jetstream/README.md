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
No MySQL, credentials or external notification provider is required.

The [isolated CI workflow](../../.github/workflows/next-jetstream.yml) runs
these tests with the race detector. The NATS server and client are Apache-2.0
licensed dependencies; this experiment does not vendor or ship their binaries.

Not covered: production authentication, encryption, atomic business receipts,
abrupt process or power loss, separate availability zones, throughput/P99,
real notification delivery, or cutover from MySQL.
