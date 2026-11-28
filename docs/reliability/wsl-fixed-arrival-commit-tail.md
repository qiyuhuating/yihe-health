# WSL fixed-arrival COMMIT tail comparison

The production path is unchanged. The comparison used PR #25 candidate 9f5a720; current main a1922d6 has the same measured implementation (only four test-file changes). WSL2 Ubuntu 26.04, 20 logical CPUs, Go 1.27.4 with GOMAXPROCS=8, extracted MySQL 8.4.8, Linux filesystem, 256 MiB buffer pool and redo capacity, business pool 12, reserved SOS pool 2, SOS priority enabled. Both innodb_flush_log_at_trx_commit and sync_binlog are 1. Default log writer threads and ordered commits are retained; no deliberate binlog group-commit delay. These are disposable isolated database experiments on a shared Windows host, not a production deployment or power-loss test.

Run the opt-in TestMixedSOSLatencyAB with RUN_MIXED_SOS_AB=1 MIXED_SOS_AUTH=1 MIXED_FIXED_ARRIVALS=1 MIXED_TRACE_PHASES=1 MIXED_SOS_WORKERS=8, TEST_MYSQL_DSN pointing exclusively at an isolated binlog-enabled MySQL instance. Two waiting slots and one consumer per device; no unbounded arrivals or goroutines. Eight health devices schedule 120 uploads each every 100 ms (64 samples per upload); two SOS streams schedule 100 each every 120 ms, staggered phases. Durations include queue wait measured from the planned timestamp. Auth profiles and password login/logout retain the existing closed-loop workers, so their completed counts vary. Drain accepted work, verify exact persisted health history and incident counts. Track producer queue drops separately from HTTP 503; dropped SOS never reached the server and must not be counted as successful. Fixed offered work is comparable even when service slows. Each trial resets the isolated Performance Schema digest summary before fixture creation, and reports explicit COMMIT bucket upper bounds, not exact quantiles or autocommit statement timing.

Feedback control runs the same one-second telemetry query and stage sampling in both arms. With MIXED_COMMIT_FEEDBACK=1, reduce the pre-BEGIN health admission limit by one (floor 4) when health commit p90 >8 ms, twice commit p90 exceeds SQL p90, and global redo/binlog file wait delta exceeds 100 ms; recover one slot after two consecutive windows below 5 ms. This is an experimental heuristic, not a validated controller; no waiting is introduced inside a transaction. Existing SOS priority and reserved pool are retained. Global file wait time sums parallel events, not wall-clock device busy time or fsync-only waits. The feedback controller and fixed-arrival workload are test-only.

The second experiment varies binlog_row_image FULL/MINIMAL only, with feedback disabled. The third varies startup innodb_flush_method O_DIRECT/fsync, restarting the disposable instance for each trial, with FULL row images and feedback disabled. The fourth varies binlog_transaction_compression OFF/ON with Zstd level 1, retaining FULL row images, on the merged main base. The reported binlog byte delta is measured across the mixed workload and includes all successful operations, not just telemetry. No production setting changes are made by this experiment. Smaller row images can affect downstream CDC/audit consumers, so a performance result alone cannot authorize changing the default.


## feedback ABBA

| Arm | Health done | HTTP rejects | Queue drops | SOS done | SOS drops | Samples/s | COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | Health arrival p99 ms | SOS arrival p99 ms | Binlog bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| feedback_0 | 957 | 1 | 2 | 200 | 0 | 4900.69 | 16.596 | 16.318 | 14.258 | 179.115 | 186.519 | not measured |
| feedback_1 | 958 | 1 | 1 | 200 | 0 | 4874.76 | 39.811 | 40.082 | 38.506 | 166.402 | 168.111 | not measured |
| feedback_1 | 934 | 5 | 21 | 197 | 3 | 4874.93 | 39.811 | 37.824 | 202.708 | 531.900 | 677.546 | not measured |
| feedback_0 | 945 | 1 | 14 | 198 | 2 | 4860.03 | 22.909 | 23.299 | 22.319 | 410.886 | 530.838 | not measured |

| Arm | COMMIT digest/histogram count | Profile reads | Logins | Redo fsyncs | Redo bytes | Data bytes | Digest loss |
|---|---:|---:|---:|---:|---:|---:|---:|
| feedback_0 | 1225/1225 | 16724 | 34 | 4697 | 96985600 | 194634752 | 0 |
| feedback_1 | 1218/1220 | 16775 | 31 | 2918 | 98203136 | 192909312 | 0 |
| feedback_1 | 1190/1191 | 17157 | 30 | 2829 | 94327296 | 185498112 | 0 |
| feedback_0 | 1211/1211 | 16986 | 34 | 6416 | 95253504 | 186955776 | 0 |

Two-trial midpoints (not pooled percentiles): COMMIT p99 upper 19.752 -> 39.811 ms (+101.6%); throughput 4880.36 -> 4874.84 samples/s (-0.1%).

## binlog ABBA

| Arm | Health done | HTTP rejects | Queue drops | SOS done | SOS drops | Samples/s | COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | Health arrival p99 ms | SOS arrival p99 ms | Binlog bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| row_image_FULL | 957 | 1 | 2 | 200 | 0 | 4967.33 | 36.308 | 35.788 | 35.925 | 204.457 | 149.730 | 54778260 |
| row_image_MINIMAL | 944 | 2 | 14 | 197 | 3 | 4823.64 | 39.811 | 39.002 | 36.387 | 411.969 | 590.828 | 47150865 |
| row_image_MINIMAL | 946 | 2 | 12 | 198 | 2 | 4940.29 | 20.893 | 20.340 | 20.965 | 374.938 | 426.752 | 47260375 |
| row_image_FULL | 957 | 3 | 0 | 200 | 0 | 4971.40 | 11.482 | 11.369 | 10.380 | 164.398 | 126.093 | 54790678 |

| Arm | COMMIT digest/histogram count | Profile reads | Logins | Redo fsyncs | Redo bytes | Data bytes | Digest loss |
|---|---:|---:|---:|---:|---:|---:|---:|
| row_image_FULL | 1218/1219 | 16750 | 31 | 3099 | 97653760 | 196093440 | 0 |
| row_image_MINIMAL | 1200/1201 | 16783 | 30 | 2199 | 96112640 | 186624000 | 0 |
| row_image_MINIMAL | 1209/1210 | 16504 | 33 | 5462 | 95430656 | 190474752 | 0 |
| row_image_FULL | 1225/1225 | 16863 | 34 | 6783 | 95381504 | 189864448 | 0 |

Two-trial midpoints (not pooled percentiles): COMMIT p99 upper 23.895 -> 30.352 ms (+27.0%); throughput 4969.37 -> 4881.96 samples/s (-1.8%).
Measured mixed binlog bytes 54784469 -> 47205620 (-13.8%).

## flush ABBA

| Arm | Health done | HTTP rejects | Queue drops | SOS done | SOS drops | Samples/s | COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | Health arrival p99 ms | SOS arrival p99 ms | Binlog bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| flush_O_DIRECT | 944 | 1 | 15 | 198 | 2 | 4911.40 | 43.652 | 42.594 | 45.695 | 428.866 | 550.864 | 54028527 |
| flush_fsync | 941 | 2 | 17 | 197 | 3 | 4904.58 | 41.687 | 41.565 | 66.831 | 420.768 | 522.154 | 53860832 |
| flush_fsync | 955 | 3 | 2 | 200 | 0 | 4985.70 | 19.055 | 18.766 | 13.947 | 201.145 | 193.605 | 54672834 |
| flush_O_DIRECT | 941 | 16 | 3 | 200 | 0 | 4915.44 | 16.596 | 16.249 | 17.771 | 197.830 | 178.573 | 53877007 |

| Arm | COMMIT digest/histogram count | Profile reads | Logins | Redo fsyncs | Redo bytes | Data bytes | Digest loss |
|---|---:|---:|---:|---:|---:|---:|---:|
| flush_O_DIRECT | 1198/1200 | 16922 | 29 | 2152 | 96509440 | 191595520 | 0 |
| flush_fsync | 1196/1198 | 16593 | 30 | 2735 | 96948224 | 190158848 | 0 |
| flush_fsync | 1220/1221 | 16538 | 33 | 4637 | 97760768 | 191207424 | 0 |
| flush_O_DIRECT | 1207/1207 | 16286 | 33 | 4612 | 94978048 | 186679296 | 0 |

Two-trial midpoints (not pooled percentiles): COMMIT p99 upper 30.124 -> 30.371 ms (+0.8%); throughput 4913.42 -> 4945.14 samples/s (+0.6%).
Measured mixed binlog bytes 53952767 -> 54266833 (+0.6%).

## compress ABBA

| Arm | Health done | HTTP rejects | Queue drops | SOS done | SOS drops | Samples/s | COMMIT p99 upper ms | Health commit p99 ms | SOS commit p99 ms | Health arrival p99 ms | SOS arrival p99 ms | Binlog bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| compression_OFF | 939 | 3 | 18 | 197 | 3 | 4781.69 | 45.709 | 44.839 | 47.646 | 449.397 | 544.685 | 53747211 |
| compression_ON | 947 | 0 | 13 | 200 | 0 | 4838.06 | 39.811 | 39.747 | 34.040 | 441.108 | 134.947 | 29615176 |
| compression_ON | 955 | 3 | 2 | 200 | 0 | 4893.16 | 19.953 | 19.507 | 19.545 | 199.439 | 188.489 | 29870309 |
| compression_OFF | 959 | 0 | 1 | 200 | 0 | 4976.44 | 12.589 | 12.987 | 11.512 | 154.256 | 37.951 | 54904352 |

| Arm | COMMIT digest/histogram count | Profile reads | Logins | Redo fsyncs | Redo bytes | Data bytes | Digest loss |
|---|---:|---:|---:|---:|---:|---:|---:|
| compression_OFF | 1193/1196 | 17118 | 30 | 2161 | 95331840 | 193075200 | 0 |
| compression_ON | 1209/1211 | 16424 | 32 | 3520 | 96742912 | 194421248 | 0 |
| compression_ON | 1223/1223 | 16260 | 34 | 5506 | 96694272 | 198580736 | 0 |
| compression_OFF | 1227/1227 | 16673 | 34 | 6793 | 95857152 | 188124672 | 0 |

Two-trial midpoints (not pooled percentiles): COMMIT p99 upper 29.149 -> 29.882 ms (+2.5%); throughput 4879.06 -> 4865.61 samples/s (-0.3%).
Measured mixed binlog bytes 54325782 -> 29742742 (-45.3%).

All admitted successful work matched persisted counts, and there was no reported redo-capacity, buffer-pool free-page or business connection wait. Some digest/histogram deltas differ at snapshot boundaries. Results vary substantially on the shared host. Do not compare successful-only tail latencies without the offered/completed/drop counters. No claim of an overall stable production COMMIT improvement follows from a single favorable trial.

## Decision

Do not adopt any of the four candidates as a COMMIT-tail optimization. The fixed-threshold feedback failed both latency and completion objectives. MINIMAL reduces log volume but changes downstream row-image semantics and worsened the tail. The flush method comparison does not separate the candidates from host variation. Compression retained full row images and cut measured Binlog volume by 45.3% with nearly unchanged throughput, but did not establish a COMMIT P99 improvement; keep it as a storage/replication-bandwidth candidate pending restore and downstream consumer validation. The default database and transaction paths are unchanged. The 16 trials scheduled 15360 health uploads and 3200 SOS requests; 15179 health uploads and 3182 SOS requests were completed and checked against persisted rows. Eighteen SOS arrivals were dropped at the bounded test client, not accepted and lost by the server.
