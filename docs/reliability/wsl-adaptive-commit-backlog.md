# Backlog-driven health transaction grouping

Base: PR #25, 996599668fad1b1bf27791297aef92e307cac91d. Same WSL2 Ubuntu 26.04 native Linux filesystem, Go 1.27.4 (GOMAXPROCS=8), MySQL 8.4.8, 256 MiB buffer pool/redo, O_DIRECT and durability 1/1. Business pool 12, SOS pool 2. Each ABBA uses a separate disposable instance.

## Test design

`TestHealthBacklogCommitGrouping` with `YIHE_HEALTH_BACKLOG_AB=1`, additionally `YIHE_HEALTH_BACKLOG_GREEDY=1` for the second experiment. Eight devices and eight distinct owners, 64 protected samples per upload, 64 rounds per trial. Four workers in both variants, each transaction verifies the device binding, calls the actual SaveProtected and heartbeat paths, and acknowledges only after COMMIT. Queue capacity eight; every request in each round is already prepared, and all are released together. Neither candidate deliberately waits for future arrivals.

In the first experiment the initial four jobs start as singletons; the remaining four are queued. A free worker takes one job and may immediately take a second queued job. In the second experiment all eight jobs are queued before dispatch. Both candidates take at most two distinct-device/owner jobs per transaction. The baseline always takes one. Measure confirmation latency from round admission, including queue wait. Count actual Binlog Xid events and check persisted sample/history counts; the existing injected second-member rollback is retained. This is a closed-round storage fixture, not a continuous production scheduler: owner/device conflict isolation is a fixture property, and the channels do not implement production cancellation, per-member nonce/rejection or SOS arbitration. No simultaneous SOS load in these trials.

## Experiment 1: tail-only backlog grouping

| Trial | Max width | Xid | Samples/s | Logical ack P50 ms | Logical ack P99 ms | COMMIT P99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | 1 | 512 | 9082.17 | 36.3568 | 185.9646 | 26.1146 |
| B1 | 2 | 384 | 10378.48 | 38.6150 | 65.6547 | 23.4467 |
| B2 | 2 | 386 | 10042.60 | 40.0797 | 63.6923 | 33.5621 |
| A2 | 1 | 512 | 12141.11 | 31.3710 | 67.5878 | 27.4234 |

Two-trial midpoints: Xid 512 to 385 (-24.8%); throughput 10611.64 to 10210.54 samples/s (-3.8%); COMMIT P99 26.769 to 28.504 ms (+6.5%); logical confirmation P99 126.776 to 64.674 ms (-49.0%). The first baseline has a much longer confirmation tail than the second. No production selection.

## Experiment 2: already queued burst grouping

| Trial | Max width | Xid | Samples/s | Logical ack P50 ms | Logical ack P99 ms | COMMIT P99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | 1 | 512 | 5774.07 | 52.6375 | 354.4140 | 51.9616 |
| B1 | 2 | 256 | 11531.48 | 39.0135 | 93.4487 | 71.0061 |
| B2 | 2 | 256 | 14356.25 | 31.8687 | 91.2365 | 70.3034 |
| A2 | 1 | 512 | 11513.42 | 33.8777 | 66.7745 | 31.5309 |

Two-trial midpoints: Xid 512 to 256 (-50%); throughput 8643.745 to 12943.865 samples/s (+49.7%); COMMIT P99 41.746 to 70.655 ms (+69.2%); logical confirmation P99 210.594 to 92.343 ms (-56.2%). Baselines vary substantially; the candidate has worse physical commit P99 in both trials. No production selection. These are midpoints of per-trial percentiles, not pooled percentiles. All eight trial history counts matched, and no durability change was made.

## Independent SOS improvement

After a successful health COMMIT, release the local physical-device transaction marker immediately, before returning the explicit connection and publishing the statistics projection. Keep the device/owner admission reservation until projection publication has completed. A verified SOS may proceed on its own SQL pool after the durable commit; another ordinary upload cannot overtake the retained owner reservation. No preemption of an in-flight SQL transaction and no change to persistent authorization, nonce or transaction semantics. The optional post_commit_projection trace measures the remaining work before owner-slot release.

Existing real database regression: authentication congestion SOS durable commit 19.57256 ms (single controlled observation), existing SOS reserved-pool test and bounded/cancelable health admission passed. The new targeted post-commit regression pauses the health response at the post-commit trace while retaining the admission reservation, checks the committed sample, invokes the actual SOS handler and checks the persisted alarm. It is a controlled bypass test, not an end-to-end SOS p99 benchmark.

Production transaction merging remains unimplemented. Next candidate needs a bounded cancellation-aware scheduler with explicit domain ownership and per-member failure semantics, measuring under continuous mixed load and retaining the original single-member path whenever grouping cannot meet both throughput and COMMIT tail objectives.
