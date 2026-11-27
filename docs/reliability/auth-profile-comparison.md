# Authentication profile comparison

Opt-in harness: `backend/internal/handlers/auth_profile_test.go`, `TestProfileComparison`. Set `YIHE_PROFILE_AB=1` and use an otherwise idle disposable `TEST_MYSQL_DSN`; the test creates its own schema. It records global Com_select deltas and elapsed time. Do not run it against production or alongside other database clients.

Comparison: Suining preview f03b13665ca34a09f0421c62d7f49b84c86ef9c7 vs Yihe PR #25 based on main 6569f0c, including request-local negative lookup and profile authorization reuse. The identical test harness was compiled in a temporary detached Suining checkout, changing only module imports and omitting the Yihe-only AuthenticationAdmission wrapper. Each trial uses the same synthetic supervisor, encrypted MySQL session, 32 warmup calls and 512 measured calls per case. ABBA order: Suining, Yihe, Yihe, Suining. In-process HTTP middleware/router requests, not a live network or full service. MySQL 8.4.13 on the same Windows host, 128 MiB buffer pool, innodb_flush_log_at_trx_commit=1 and sync_binlog=1. Distinct disposable schema per trial.

| Case | Suining SELECTs / 512 | Yihe SELECTs / 512 | Suining RPS (two trials) | Yihe RPS (two trials) |
| --- | ---: | ---: | --- | --- |
| Valid supervisor profile | 2048 | 1024 | 4038.60, 4686.06 | 3862.29, 7576.67 |
| Unknown cookie with bootstrap slots held | 1024 | 512 | 9660.32, 9101.70 | 8567.27, 17110.30 |
| No cookie with bootstrap slots held | 0 | 0 | 188152.29, 188166.12 | 245351.73, 227747.88 |

The deterministic result is half the SELECT count for both profile and missing-cookie cases. The two-trial midpoint throughput changes are +31.1%, +36.9%, and +25.7%, respectively. Observed trial variation is substantial and ranges overlap for the first two cases. Windows timing returned zero for the individual request median; the observed P99 samples cannot establish a stable latency advantage. No claims about concurrent saturation, CPU/RSS, disk I/O, or full-service speed follow from this benchmark.

The authorization regression covers direct profile access, the real CSRF/auth/duty gate, demotion and suspension between requests, and existing transaction-time duty lock tests. The profile-only snapshot is not a substitute for transaction-time authorization. No cross-request authorization cache is introduced.

`TestAuthOverloadLeavesReservedSOSConnection` verifies a separate deterministic congestion case on real MySQL: with a one-connection business pool held and `MYSQL_SOS_MAX_CONNS=2`, eight session reads and eight login requests waited in the actual SQL pool; additional requests received 429 with no extra SQL wait. The real device SOS handler committed one alarm in 9.2755 ms. This is one controlled observation with the optional SOS pool enabled, not a CPU-saturation test, production default configuration, or SOS P99 measurement.
