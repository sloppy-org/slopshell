# Backend Runtime Benchmark (Draft)

- Timestamp (UTC): `2026-02-26T18:48:51.066178+00:00`
- Host: `mailuefterl`
- OS: `Linux 6.19.3-2-cachyos`
- Go: `go version go1.26.0-X:nodwarf5 linux/amd64`
- Node: `v22.22.0`
- Rust: `rustc 1.93.1 (01f6ddf75 2026-02-11) (Arch Linux rust 1:1.93.1-1.1)`
- Java: `javac 21.0.10`
- .NET: `10.0.103`
- Python: `3.14.3`
- Workload: `decode/classify/hash/encode`

## Method

- Synthetic backend-like CPU path per request: decode request, intent classification, token counting, SHA-256, encode response.
- Throughput measured on 300,000 ops; latency measured on 50,000 per-op samples.
- Compile/build time measured before execution for each runtime in this folder.
- Code size and complexity measured via `lizard` (NCSS + cyclomatic complexity).

## Performance

| Runtime | Compile Time (s) | Throughput (ops/s) | p50 (us) | p95 (us) | p99 (us) | Throughput vs Go | p95 vs Go |
|---|---:|---:|---:|---:|---:|---:|---:|
| go | 0.160 | 141193.53 | 6.783 | 10.480 | 13.526 | 1.00x | 1.00x |
| python | 0.035 | 134991.31 | 7.304 | 7.695 | 9.297 | 0.96x | 0.73x |
| typescript-node | 0.598 | 314158.90 | 3.146 | 5.781 | 6.112 | 2.23x | 0.55x |
| java | 0.418 | 154700.46 | 4.298 | 4.839 | 5.230 | 1.10x | 0.46x |
| csharp | 3.182 | 181756.99 | 2.600 | 2.900 | 3.000 | 1.29x | 0.28x |
| rust | 4.909 | 648639.34 | 1.523 | 1.613 | 1.643 | 4.59x | 0.15x |

## Code Size & Complexity

| Runtime | Raw LOC | NCSS | Functions | Avg CCN | Total CCN |
|---|---:|---:|---:|---:|---:|
| go | 178 | 165 | 5 | 4.40 | 22 |
| python | 126 | 109 | 5 | 3.60 | 18 |
| typescript-node | 144 | 132 | 3 | 1.00 | 3 |
| java | 193 | 176 | 8 | 4.25 | 34 |
| csharp | 196 | 168 | 6 | 3.17 | 19 |
| rust | 174 | 160 | 6 | 3.17 | 19 |

## Porting Decision Notes

- Favor runtime choices that improve p95 latency and throughput without unacceptable build-time and maintenance cost.
- Keep in mind this is microbenchmark data; validate with end-to-end Tabura workloads before a full migration.

- Highest throughput in this run: `rust`.
- Recommendation: **Evaluate `rust` in a focused prototype before considering any broad port.**
- It materially outperformed Go on throughput and p95 latency in this synthetic workload.
- Compile-time cost is significantly higher than Go for the current best-performing alternative.

## Caveats

- Java benchmark uses lightweight fixed-schema extraction for request fields due stdlib JSON constraints; this can favor Java in this synthetic setup.
- This does not model websocket fanout, SQLite contention, subprocess/tool execution, or real network latency.
- Use this as directional input, not final proof for a full-language migration.

