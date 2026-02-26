# Backend Runtime Benchmark Draft

This draft compares Go, Python, TypeScript/Node.js, Java, C#, and Rust for a backend-like CPU workload to inform a possible port away from Go.

## Workload

Each runtime executes the same synthetic request path:

1. JSON request decode
2. Text normalization + intent classification
3. Token counting
4. SHA-256 digest generation
5. JSON response encode

Dataset and run sizes:

- Dataset size: `512` request payloads
- Throughput run: `300,000` operations
- Latency run: `50,000` per-op samples

## Metrics

- Compile/build time (seconds)
- Throughput (operations/second)
- Latency (`p50`, `p95`, `p99`) in microseconds
- Code size (`Raw LOC`, `NCSS`)
- Cyclomatic complexity (`Avg CCN`, `Total CCN`)

## Run

From repository root:

```bash
python3 draft/backend-runtime-benchmark/run_benchmarks.py
```

Outputs:

- Raw data: `draft/backend-runtime-benchmark/results/latest.json`
- Report: `draft/backend-runtime-benchmark/report.md`

## Notes

- This is a synthetic microbenchmark and should be used as a directional signal.
- It does not include network stack, websocket fanout, database contention, or full Tabura end-to-end behavior.
