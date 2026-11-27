# WSL COMMIT tail investigation

Candidate base: PR #25 commit 6cacfce0683c1eca352c3a7a466a2365ff11066c. WSL2 Ubuntu 26.04, Linux 6.18.58.1, 20 logical CPUs, 15,545 MiB guest RAM, Go 1.27.4 (GOMAXPROCS=8), extracted native MySQL 8.4.8 Ubuntu package. Source and disposable MySQL data on the WSL Linux filesystem, not /mnt/d. MySQL O_DIRECT, 256 MiB buffer pool/redo capacity, 64 connection server maximum, business pool 12/SOS pool 2. Both durability settings remain 1; log writers ON, group-commit delay 0. Host and MySQL version differ from the VPS; absolute times must not be compared as an algorithmic speedup.

The opt-in mixed fixture described in linux-mixed-commit-comparison.md uses four health writers, two SOS workers, two profile readers and real login/logout. Enable MIXED_TRACE_PHASES=1 to capture request-local fixed-name monotonic phase durations (no secrets or SQL strings). Ordinary requests without a trace do not read the clock for these probes. Health priority mode separates connection acquisition from BEGIN; SOS/login currently report the combined pool+BEGIN stage. Health owner-first mode includes its late lock in business_sql; early bound_device_lock stage represents the pre-business section. Stage p99 values are not additive. Successful commits are counted by lane; server digest histograms use bucket upper bounds. Digest and histogram counts can differ at sampling boundaries; digest loss was zero.

## Mixed workload: pre-transaction health concurrency

ABBA at original binlog ordering. Set MIXED_HEALTH_LIMIT for this fixture only. Production limits stay unchanged.

| Trial | Health limit | Samples/s | Health rejects | Server COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | SOS admission p99 ms | SOS HTTP p99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | 8 | 4693.91 | 0 | 45.709 | 46.058 | 42.990 | 105.031 | 156.924 |
| B1 | 2 | 3698.13 | 6 | 38.019 | 34.346 | 33.926 | 43.755 | 108.465 |
| B2 | 2 | 5498.08 | 1 | 21.878 | 23.122 | 16.466 | 30.578 | 54.743 |
| A2 | 8 | 7276.53 | 0 | 22.909 | 23.399 | 22.817 | 51.209 | 80.267 |

Two-trial midpoint of server COMMIT p99 bounds: 34.309 to 29.949 ms (-12.7%), samples/s 5985.22 to 4598.11 (-23.2%). The lower budget rejects uploads and moves delay into admission; not selected. A1 health business SQL p99 111.682 ms and SOS business SQL p99 22.866 ms. A1 health connection acquisition p99 0.348 ms, BEGIN p99 1.623 ms; no database pool waits. An end-to-end SOS tail is not just a COMMIT tail.

## Mixed workload: binlog commit ordering

ABBA, same original health limit, change only binlog_order_commits. No production configuration change.

| Trial | Order | Samples/s | Server COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | SOS HTTP p99 ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| A1 | ON | 5031.63 | 43.652 | 43.371 | 36.239 | 97.762 |
| B1 | OFF | 5135.92 | 45.709 | 43.698 | 41.953 | 136.617 |
| B2 | OFF | 4966.94 | 43.652 | 43.543 | 41.728 | 147.471 |
| A2 | ON | 6658.91 | 39.811 | 43.976 | 19.756 | 79.757 |

Server p99 midpoint 41.731 to 44.680 ms (+7.1%). No gain justifies changing ordering semantics. Across both experiments all 1600 SOS committed and acknowledged telemetry matched persisted counts; no redo-capacity or buffer-pool free-page waits. Seven health rejections occurred only with limit 2, none in the ordering experiment. No auth rejects. Shared host variation is substantial.

## Filesystem synchronization probe

After stopping the mixed fixture, write 4 KiB into preallocated 4 MiB temporary files and call fdatasync/fsync after each write; 128 operations per writer, separate files, one/four/four/one writers. No filesystem or host tuning. Single-writer fdatasync p50 2.175-2.200 ms, p99 2.857-3.598 ms; four-writer p50 4.349-4.395 ms, p99 5.917-6.128 ms. Single-writer fsync p50 2.076-2.344 ms, p99 3.172-3.535 ms; four-writer p50 4.250-4.303 ms, p99 5.797-6.707 ms. The probe measures guest-observed sync completion, not physical media power-loss behavior. Real DB commit includes multiple persistence and coordination stages.

## Shared durable transaction prototype

Opt-in `YIHE_HEALTH_GROUP_AB=1` `TestHealthDurableCommitGrouping` on an exclusive disposable binlog-enabled database. Run from backend with the same GOMAXPROCS and SQL settings above. Four devices/independent residents, 64 protected samples per upload, 128 rounds/trial. Prepare all four requests before starting each round; baseline four parallel single-member transactions; candidate two parallel transactions with two independent members each. Real device row lock, credential/binding/epoch check, existing SaveProtected, heartbeat update, commit before logical acknowledgment. This is not an HTTP implementation: no group-formation queue, signed nonce path, SOS, login or notification workload. It does not establish a production lock-order contract for arbitrary cross-owner business operations.

Inject failure after writing the first group member but before the second; verify both health_sample and health_change remain empty. Count actual Binlog Xid events and persisted sample/history counts, not inferred commit counts. ABBA uses 512 logical uploads per trial with increasing versions.

| Trial | Group size | Xid | Binlog bytes | Samples/s | Logical ack p50 ms | Logical ack p99 ms | Physical commit p50 ms | Physical commit p99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | 1 | 512 | 29045259 | 10105.22 | 20.1653 | 88.0717 | 9.8410 | 23.9483 |
| B1 | 2 | 256 | 29075569 | 9195.10 | 24.9012 | 44.8986 | 8.9394 | 21.1503 |
| B2 | 2 | 256 | 29077376 | 9891.13 | 23.5642 | 35.7449 | 8.5430 | 17.1422 |
| A2 | 1 | 512 | 29130368 | 11548.03 | 20.3848 | 35.9170 | 10.0907 | 25.2406 |

Both grouped trials have lower physical commit p99 than either baseline trial. Two-trial midpoint physical p99 24.594 to 19.146 ms (-22.2%), logical ack p99 61.994 to 40.322 ms (-35.0%, overlapping trial ranges), logical ack p50 20.275 to 24.233 ms (+19.5%). Throughput 10826.63 to 9543.12 samples/s (-11.9%). Xid count halves; byte volume is approximately unchanged. Retain as an isolated prototype, not production batching. Actual production feasibility requires bounded formation wait, resource-domain lock ordering including existing writers, per-member rejection/nonce/retry semantics, rollback behavior, and emergency bypass under sustained mixed load. Avoid declaring the overall COMMIT problem solved. No production durability/configuration change.
