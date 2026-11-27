# Yihe-Core

Rust analytics accelerator for Yihe. MySQL remains the only authoritative
store for business data, sessions, audit, blobs, transactions and recovery.
This crate contains no SQL client, persistent store, network listener or
authorization implementation. Its projections are disposable.

## Heart-rate projection

`HeartRateSeries` is an immutable structure-of-arrays projection of one
resident/source/device's instantaneous heart-rate series. It accepts at most
10,000 eligible observations, retains the original revision order, and uses two
binary searches to locate a half-open time window. Querying performs no heap
allocation. An empty covered range differs from a cache miss. Unsupported
inputs, out-of-order timestamps, unknown revisions and windows outside the
snapshot's coverage are not eligible.

The projection cannot establish that the data it receives is complete or
authorized. The Go adapter must establish that before admission. In particular:

- Resolve the resident and run current MySQL authorization on every request,
  including hits. Keep the existing API's source, metric and semantics filters,
  candidate limit, exact timestamp boundaries and error behavior.
- Obtain a strictly positive committed `health_owner_clock` revision and
  read a consistent snapshot; revalidate that revision before publishing it.
  Reject incomplete snapshots, mixed units or identities, legacy time basis,
  candidate-count discrepancies and unsupported timestamp ranges. Only reorder exact nonnegative integer values when every partial sum is
  provably representable; other values retain their original accumulation order.
- Serve only the same identity, revision and a fully covered window. Never
  return a cache hit when MySQL validation fails. Do not cache errors.
- Bound total projection memory, evict cold entries, and use the existing Go/MySQL
  implementation when admission or lookup fails. No user write is replayed.
- Keep native binaries under the existing build integrity and release gates.
  Do not load arbitrary libraries from a writable search path.

The experimental `yihecore` Go build tag enables a synchronous, statically
linked C ABI for the existing statistics endpoint. Go owns immutable columns
and a 16-shard LRU; the Rust kernel does not retain a Go pointer. The projection
and serialized result caches share the existing configured health cache
budget; projections expire after 30 seconds. Only fully covered,
microsecond-aligned instantaneous heart-rate windows qualify. The adapter
also checks each SQL timestamp index against the decrypted payload.
The same authorized transaction validates the current committed revision
before a hit is returned. Cache admission rechecks that revision after
the original database read. On a miss, unsupported input or ABI rejection,
the ordinary Go/MySQL path remains available.

For eligible integer heart-rate projections, retain the sample IDs and advance
an older view using at most 256 committed owner changes. Verify each event's
identity, the final revision, and the live owner clock. Replace corrected points,
remove tombstones and merge new points in timestamp order; unsupported inputs,
expired views or incomplete changes use a fresh MySQL snapshot. This does not
provide a durable second store.

`MYSQL_STATISTICS_MAX_CONNS` enables an independent statistics connection pool
on the same primary (0/default: shared; 1–64: dedicated). The existing business
pool remains capped at 20 and the liveness pool at 1. The latest-health
endpoint reuses its authorized transaction for the ready projection or an
owner-locked backfill. `MYSQL_SOS_MAX_CONNS` (0/default: shared; 1–8:
dedicated) reserves connections for the emergency write transaction, on the
same primary. Budget all pools and replicas against MySQL `max_connections`.
Connection acquisition remains context-bound; neither pool bypasses row locks,
live authorization, or durable commit. Session lookup and other middleware
still use the business pool.

A latest-series refresh fetches only rows tied at the maximum indexed
microsecond, compares their exact timestamps and revisions, and upserts the
winner only if its authenticated content, metadata or encryption key changes.
Historical corrections that leave the winner unchanged need no projection
write.

The experimental baseline path runs the 28-day daily-median and fixed-anchor
heart-rate algorithm in Rust after the original authorized MySQL read. It
keeps the resident, device, binding, source and date filters in Go. Small
or unsupported observations use the original Go evaluator. The personal
baseline itself is still recomputed from its authorized database snapshot.

The SIMD reducer uses AVX-512F, AVX2 or NEON when detected and the whole
series consists of nonnegative exact integers (excluding negative zero)
at most 900719925474. With at most 10,000 points, every partial sum is
exactly representable in f64. Other values retain ordered scalar arithmetic.
The default build and `purego` do not activate the native path. A native
process fault is not recoverable inside the same process; the build tag
can be removed to restore the ordinary backend.

## Experimental native build

Windows x64 (GNU Rust toolchain and matching GCC on PATH):

```powershell
cargo +stable-x86_64-pc-windows-gnu build --release --locked --target x86_64-pc-windows-gnu
Set-Location ../backend
$env:CGO_ENABLED = '1'
go test -tags yihecore ./internal/handlers -run 'TestCoreColumnCacheParity|TestCoreStatisticsMySQL'
```

Linux x64 or ARM64 (native Rust and C compiler): build with
`cargo build --release --locked` from this directory, then use
`CGO_ENABLED=1` and `go test -tags yihecore` in the backend.
Build the Rust archive again whenever its sources change. The ABI symbol
is versioned. Use only a trusted local build artifact.
MySQL tests require a disposable `TEST_MYSQL_DSN`; without it they skip.
The desktop packaging path remains unchanged. Linux and ARM64 builds,
production deployment and broader workload verification remain separate from
isolated integration and benchmark results.

## Verification

From this directory:

```powershell
cargo fmt --check
cargo test --locked --offline
cargo clippy --locked --offline --all-targets -- -D warnings
cargo run --locked --offline --release --example range_bench
```

The benchmark compares a full scan of the same 4,320 synthetic observations
with an already-built in-memory index. It excludes index construction,
authorization, MySQL, decryption and Go/Rust boundary costs. It is not a
claim about HTTP throughput.
