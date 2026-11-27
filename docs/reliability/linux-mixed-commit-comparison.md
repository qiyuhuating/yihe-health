# Linux mixed-load and commit investigation

Candidate: Yihe PR #25, base d7429ba8bcae376b60bf5ebc15b13887ed72d390. Linux x86-64, 2 vCPU, 908 MiB RAM, 1 GiB swap, Go 1.27.4 (GOMAXPROCS=2), MySQL 8.4.13, O_DIRECT, 64 MiB buffer pool, 128 MiB redo capacity, maximum 32 server connections. Durable settings innodb_flush_log_at_trx_commit=1 and sync_binlog=1, binary log enabled. Business pool 8, SOS pool 2, SOS priority enabled. Same host runs other services; this is not a dedicated production-size node.

Opt-in harness: `TestMixedSOSLatencyAB`, set `RUN_MIXED_SOS_AB=1 MIXED_SOS_AUTH=1 MIXED_SOS_WORKERS=4 MYSQL_MAX_OPEN_CONNS=8 MYSQL_MAX_IDLE_CONNS=8 MYSQL_SOS_MAX_CONNS=2 YIHE_SOS_PRIORITY=1` and `TEST_MYSQL_DSN` to an otherwise idle disposable MySQL server. Run `go test ./internal/handlers -run '^TestMixedSOSLatencyAB$' -count=1 -v -timeout 120s` from backend. The test migrates an independent schema; do not run against production.

Four continuous device writers (64 samples per batch, increasing versions), two emergency writers (100 distinct requests each), two authenticated profile readers, one successful password login/session rotation/logout worker (150 ms pacing). Actual loopback HTTP routes, CSRF, auth, duty, device-signature and admission middleware, persisted encrypted sessions and audit. Synthetic identities and secrets, no real devices. This is a closed-loop benchmark, not an arrival-rate-controlled load generator, and does not include every production middleware, persistent risk-manager initialization, background workers or external notification delivery.

Sample schema-specific Performance Schema COMMIT statement digest and histogram before/after the workload; use bucket upper bounds, not exact per-call percentiles. Keep statement digest consumer enabled; optionally enable timing for redo/binlog file I/O and relevant synchronization waits. Reset the digest summary only on the exclusive disposable instance before each trial, or allocate sufficient digest capacity; the test rejects missing COMMIT samples. Global status/wait counters include all operations on this isolated server (including InnoDB background work); explicit COMMIT digest excludes implicit/autocommit statements. The histogram and digest counters can differ slightly at a snapshot boundary.

## Binlog group-commit delay (before atomic login rotation)

ABBA: 0, 200, 200, 0 microseconds. Preserve both durability settings. No delay was adopted.

| Trial | Delay (us) | Samples/s | SOS p99 ms | Health p99 ms | Profile p99 ms | Successful logins | COMMIT digest count | COMMIT p99 upper ms | COMMIT mean ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | 0 | 3655.69 | 117.33 | 140.62 | 21.33 | 11 | 703 | 5.248 | 1.470 |
| B1 | 200 | 3677.97 | 186.24 | 193.05 | 23.40 | 11 | 708 | 8.318 | 3.020 |
| B2 | 200 | 3718.23 | 108.91 | 120.68 | 20.23 | 11 | 702 | 9.120 | 3.209 |
| A2 | 0 | 3258.35 | 188.49 | 192.26 | 27.11 | 10 | 685 | 7.244 | 1.553 |

All 800 SOS commits and all acknowledged health samples matched persisted counts. No rejected writes or auth requests, business connection waits, redo-capacity waits, or buffer-pool-free-page waits. Trial B2 had 704 histogram samples vs 702 digest-count delta. The two-trial midpoint COMMIT p99 bound worsened 39.6% with 200 us, and mean more than doubled. SOS and throughput results vary substantially. Do not infer individual SOS phase times from pooled COMMIT timings. Redo fsync counts ranged 6619-6897 across trials, including background log I/O; they are not per-transaction fsync counts.

## Atomic successful-login rotation

With a valid previous cookie, the old implementation deleted it, created a new encrypted session, and wrote success audit in three separate autocommit statements. The new implementation prepares the encrypted envelope before the transaction, updates the existing session primary key to a fresh random ID (requiring one unexpired matching row), writes the success audit on the same transaction, and returns the cookie only after COMMIT. A failed audit rolls back both writes, leaving the old session intact. Two independent stores racing the same previous session produce exactly one successful rotation. No schema change; memory backend is development-only and has no database audit transaction.

Opt-in `YIHE_ROTATION_AB=1`, `TestSessionRotationCommitComparison`, binlog-enabled exclusive disposable MySQL. 64 pre-created anonymous sessions per trial, excluding bcrypt and fixture creation; same actual database and audit operations. Count real Binlog Xid events within measured positions, not just explicit COMMIT statements.

| Trial | Implementation | Xid | Binlog bytes | Rotations/s | P50 ms | P99 ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| A1 | separate autocommit | 192 | 149072 | 926.34 | 0.919745 | 2.220114 |
| B1 | atomic | 64 | 116114 | 403.85 | 1.650777 | 17.943772 |
| B2 | atomic | 64 | 116135 | 318.51 | 2.995885 | 5.489409 |
| A2 | separate autocommit | 192 | 149088 | 468.98 | 1.972448 | 3.167279 |

Xid count fell 66.7%, binlog bytes approximately 22.1%. However, isolated rotation throughput regressed and tail increased on this host. The atomic implementation is justified by stronger consistency and fewer durable transaction boundaries, not an established per-request latency win. Full login includes password verification; this focused measurement excludes it. One subsequent mixed run completed all 200 SOS (p99 114.96 ms), 486 health batches (3850.81 samples/s), 2827 profiles and 10 logins, but COMMIT digest capacity was exhausted by prior fixtures, so its COMMIT distribution was unavailable and is excluded from commit comparisons. The new harness fails rather than reporting zero when sampling is missing.

Real regressions passed: admin/family login and old-cookie revocation, audit failure rollback, competing replica rotation, cancellation, full session suite (including cross-replica revoke and factor generation), and held-business-pool SOS isolation with 8 session requests and 1 login request waiting (single SOS commit 10.952299 ms, not p99).

## InnoDB log writer threads (after atomic login rotation)

ABBA: ON, OFF, OFF, ON. Binlog group-commit delay remained 0, and both durable settings remained 1. Reset only the disposable instance's statement digest summary before each trial. No production configuration change.

| Trial | Log writer | Samples/s | SOS p99 ms | Health p99 ms | COMMIT count | COMMIT p50 upper ms | COMMIT p99 upper ms | COMMIT mean ms | Redo fsyncs |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A1 | ON | 3237.30 | 215.06 | 211.67 | 724 | 1.259 | 5.754 | 1.573 | 7364 |
| B1 | OFF | 4133.84 | 165.37 | 160.94 | 717 | 0.316 | 100.000 | 2.768 | 801 |
| B2 | OFF | 4036.56 | 159.21 | 152.77 | 724 | 0.437 | 79.433 | 1.848 | 822 |
| A2 | ON | 3536.65 | 123.73 | 139.08 | 710 | 1.380 | 6.310 | 1.627 | 7254 |

All 800 SOS and acknowledged health samples were durable, with zero reported auth or health rejections, pool waits, log capacity waits, buffer-pool free-page waits and digest loss. All four histogram sample counts matched explicit COMMIT digest counts. ON midpoint p99 bound 6.032 ms; OFF 89.716 ms (~14.9x). OFF increased sample throughput ~20.6% and reduced total redo fsyncs ~88.9%, but both OFF trials exhibited severe explicit COMMIT tails. Keep dedicated writers enabled for the tail-latency objective. These are server-global redo fsync counts, not individual requests; low median and low fsync count do not prove low tail latency.

Total eight measured comparison trials: 1600 SOS successful. A separate atomic mixed regression also completed 200 SOS, but its exhausted digest summary was excluded from comparison. The host is small and shared, so repeated trials on an isolated production-class Linux host remain necessary. This investigation does not establish that every request type is faster than Suining or that SOS P99 has reached its target. Next instrument admission, connection acquisition, BEGIN, row locks, business SQL and COMMIT per lane to localize the gap between explicit COMMIT and end-to-end SOS latency. Do not relax durability or add an unvalidated production tuning knob.
