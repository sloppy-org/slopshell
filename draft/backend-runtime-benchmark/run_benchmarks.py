#!/usr/bin/env python3
import json
import platform
import shutil
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parent
RESULTS_DIR = ROOT / "results"
LATEST_JSON = RESULTS_DIR / "latest.json"
LATEST_REPORT = ROOT / "report.md"

RUNTIME_SOURCES = {
    "go": [Path("go_bench/main.go")],
    "python": [Path("python_bench.py")],
    "typescript-node": [Path("node_bench.ts")],
    "java": [Path("java_bench/Main.java")],
    "csharp": [Path("cs_bench/Program.cs")],
    "rust": [Path("rust_bench/src/main.rs")],
}


def run_command(cmd, cwd=None):
    started = time.perf_counter()
    proc = subprocess.run(
        cmd,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    elapsed = time.perf_counter() - started
    if proc.returncode != 0:
        raise RuntimeError(
            f"command failed ({' '.join(cmd)}):\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}"
        )
    return elapsed, proc.stdout.strip()


def ensure_typescript_toolchain():
    node_modules = ROOT / "node_modules"
    if node_modules.exists():
        return
    run_command(["npm", "install"], cwd=ROOT)


def ensure_lizard_python():
    venv_dir = ROOT / ".venv"
    python_bin = venv_dir / "bin" / "python"
    if not python_bin.exists():
        run_command(["python3", "-m", "venv", str(venv_dir)], cwd=ROOT)
    try:
        run_command([str(python_bin), "-c", "import lizard"], cwd=ROOT)
    except RuntimeError:
        run_command([str(python_bin), "-m", "pip", "install", "lizard"], cwd=ROOT)
    return python_bin


def cleanup_for_compile():
    for path in [ROOT / "build", ROOT / "__pycache__", ROOT / "rust_bench" / "target", ROOT / "cs_bench" / "bin", ROOT / "cs_bench" / "obj"]:
        if path.exists():
            shutil.rmtree(path)
    for class_file in (ROOT / "java_bench").glob("*.class"):
        class_file.unlink(missing_ok=True)


def bench_go():
    bin_path = ROOT / "build" / "go_bench"
    bin_path.parent.mkdir(parents=True, exist_ok=True)
    compile_s, _ = run_command(["go", "build", "-o", str(bin_path), "./go_bench"], cwd=ROOT)
    _, out = run_command([str(bin_path)], cwd=ROOT)
    return {"runtime": "go", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def bench_python():
    compile_s, _ = run_command(["python3", "-m", "py_compile", "python_bench.py"], cwd=ROOT)
    _, out = run_command(["python3", "python_bench.py"], cwd=ROOT)
    return {"runtime": "python", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def bench_typescript():
    ensure_typescript_toolchain()
    ts_output = ROOT / "build" / "ts"
    if ts_output.exists():
        shutil.rmtree(ts_output)
    tsc_path = ROOT / "node_modules" / ".bin" / "tsc"
    compile_s, _ = run_command([str(tsc_path), "--project", "tsconfig.json"], cwd=ROOT)
    js_path = ROOT / "build" / "ts" / "node_bench.js"
    _, out = run_command(["node", str(js_path)], cwd=ROOT)
    return {"runtime": "typescript-node", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def bench_java():
    java_out = ROOT / "build" / "java"
    if java_out.exists():
        shutil.rmtree(java_out)
    java_out.mkdir(parents=True, exist_ok=True)
    compile_s, _ = run_command(["javac", "-d", str(java_out), "java_bench/Main.java"], cwd=ROOT)
    _, out = run_command(["java", "-cp", str(java_out), "Main"], cwd=ROOT)
    return {"runtime": "java", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def bench_csharp():
    run_command(["dotnet", "restore", "cs_bench/Bench.csproj"], cwd=ROOT)
    cs_out = ROOT / "build" / "cs"
    if cs_out.exists():
        shutil.rmtree(cs_out)
    compile_s, _ = run_command(
        [
            "dotnet",
            "build",
            "cs_bench/Bench.csproj",
            "-c",
            "Release",
            "-o",
            str(cs_out),
            "--nologo",
        ],
        cwd=ROOT,
    )
    _, out = run_command(["dotnet", str(cs_out / "Bench.dll")], cwd=ROOT)
    return {"runtime": "csharp", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def bench_rust():
    compile_s, _ = run_command(
        ["cargo", "build", "--release", "--manifest-path", "rust_bench/Cargo.toml"],
        cwd=ROOT,
    )
    bin_path = ROOT / "rust_bench" / "target" / "release" / "tabura_runtime_bench"
    _, out = run_command([str(bin_path)], cwd=ROOT)
    return {"runtime": "rust", "compile_seconds": compile_s, "benchmark": json.loads(out)}


def parse_lizard_for_file(lizard_python, source_path):
    _, xml_text = run_command([str(lizard_python), "-m", "lizard", "-X", str(source_path)], cwd=ROOT)
    root = ET.fromstring(xml_text)
    file_measure = None
    for measure in root.findall("measure"):
        if measure.attrib.get("type") == "File":
            file_measure = measure
            break
    if file_measure is None:
        return {"ncss": 0, "ccn_total": 0, "functions": 0}
    item = file_measure.find("item")
    if item is None:
        return {"ncss": 0, "ccn_total": 0, "functions": 0}
    values = [int(v.text or "0") for v in item.findall("value")]
    # values: Nr, NCSS, CCN, Functions
    if len(values) < 4:
        return {"ncss": 0, "ccn_total": 0, "functions": 0}
    return {
        "ncss": values[1],
        "ccn_total": values[2],
        "functions": values[3],
    }


def raw_loc(path):
    lines = path.read_text(encoding="utf-8").splitlines()
    return len(lines)


def collect_source_stats(lizard_python):
    stats = {}
    for runtime, rel_files in RUNTIME_SOURCES.items():
        files = [ROOT / rel for rel in rel_files]
        raw_lines = 0
        ncss = 0
        ccn_total = 0
        functions = 0
        for file_path in files:
            raw_lines += raw_loc(file_path)
            metrics = parse_lizard_for_file(lizard_python, file_path)
            ncss += int(metrics["ncss"])
            ccn_total += int(metrics["ccn_total"])
            functions += int(metrics["functions"])
        ccn_avg = (ccn_total / functions) if functions > 0 else 0.0
        stats[runtime] = {
            "files": [str(rel) for rel in rel_files],
            "raw_loc": raw_lines,
            "ncss": ncss,
            "ccn_total": ccn_total,
            "functions": functions,
            "ccn_avg": ccn_avg,
        }
    return stats


def format_float(value, digits=2):
    return f"{value:.{digits}f}"


def make_report(payload):
    rows = payload["runs"]
    source_stats = payload["source_stats"]
    go_row = next((r for r in rows if r["runtime"] == "go"), None)
    if go_row is None:
        raise RuntimeError("go result missing")
    go_tp = float(go_row["benchmark"]["throughput_ops_per_sec"])
    go_p95 = float(go_row["benchmark"]["latency_us"]["p95"])
    go_compile = float(go_row["compile_seconds"])

    lines = []
    lines.append("# Backend Runtime Benchmark (Draft)")
    lines.append("")
    lines.append(f"- Timestamp (UTC): `{payload['metadata']['timestamp_utc']}`")
    lines.append(f"- Host: `{payload['metadata']['host']}`")
    lines.append(f"- OS: `{payload['metadata']['os']}`")
    lines.append(f"- Go: `{payload['metadata']['go']}`")
    lines.append(f"- Node: `{payload['metadata']['node']}`")
    lines.append(f"- Rust: `{payload['metadata']['rust']}`")
    lines.append(f"- Java: `{payload['metadata']['java']}`")
    lines.append(f"- .NET: `{payload['metadata']['dotnet']}`")
    lines.append(f"- Python: `{payload['metadata']['python']}`")
    lines.append(f"- Workload: `{payload['metadata']['workload']}`")
    lines.append("")
    lines.append("## Method")
    lines.append("")
    lines.append("- Synthetic backend-like CPU path per request: decode request, intent classification, token counting, SHA-256, encode response.")
    lines.append("- Throughput measured on 300,000 ops; latency measured on 50,000 per-op samples.")
    lines.append("- Compile/build time measured before execution for each runtime in this folder.")
    lines.append("- Code size and complexity measured via `lizard` (NCSS + cyclomatic complexity).")
    lines.append("")
    lines.append("## Performance")
    lines.append("")
    lines.append("| Runtime | Compile Time (s) | Throughput (ops/s) | p50 (us) | p95 (us) | p99 (us) | Throughput vs Go | p95 vs Go |")
    lines.append("|---|---:|---:|---:|---:|---:|---:|---:|")

    for row in rows:
        bench = row["benchmark"]
        tp = float(bench["throughput_ops_per_sec"])
        p50 = float(bench["latency_us"]["p50"])
        p95 = float(bench["latency_us"]["p95"])
        p99 = float(bench["latency_us"]["p99"])
        compile_s = float(row["compile_seconds"])
        tp_vs_go = tp / go_tp
        p95_vs_go = p95 / go_p95 if go_p95 else 0.0
        lines.append(
            f"| {row['runtime']} | {format_float(compile_s, 3)} | {format_float(tp)} | "
            f"{format_float(p50, 3)} | {format_float(p95, 3)} | {format_float(p99, 3)} | "
            f"{format_float(tp_vs_go, 2)}x | {format_float(p95_vs_go, 2)}x |"
        )

    lines.append("")
    lines.append("## Code Size & Complexity")
    lines.append("")
    lines.append("| Runtime | Raw LOC | NCSS | Functions | Avg CCN | Total CCN |")
    lines.append("|---|---:|---:|---:|---:|---:|")
    for row in rows:
        runtime = row["runtime"]
        stat = source_stats[runtime]
        lines.append(
            f"| {runtime} | {stat['raw_loc']} | {stat['ncss']} | {stat['functions']} | "
            f"{format_float(stat['ccn_avg'], 2)} | {stat['ccn_total']} |"
        )

    lines.append("")
    lines.append("## Porting Decision Notes")
    lines.append("")
    lines.append("- Favor runtime choices that improve p95 latency and throughput without unacceptable build-time and maintenance cost.")
    lines.append("- Keep in mind this is microbenchmark data; validate with end-to-end Tabura workloads before a full migration.")
    lines.append("")

    ranking = sorted(rows, key=lambda r: float(r["benchmark"]["throughput_ops_per_sec"]), reverse=True)
    best = ranking[0]
    lines.append(f"- Highest throughput in this run: `{best['runtime']}`.")

    recommendation = "Keep Go as default backend; prototype alternatives only for proven CPU hotspots."
    rationale = []
    top_non_go = next((r for r in ranking if r["runtime"] != "go"), None)
    if top_non_go is not None:
        top_tp = float(top_non_go["benchmark"]["throughput_ops_per_sec"])
        top_p95 = float(top_non_go["benchmark"]["latency_us"]["p95"])
        top_compile = float(top_non_go["compile_seconds"])
        top_stats = source_stats[top_non_go["runtime"]]
        go_stats = source_stats["go"]

        if top_tp > go_tp * 1.10 and top_p95 < go_p95 * 0.95:
            recommendation = f"Evaluate `{top_non_go['runtime']}` in a focused prototype before considering any broad port."
            rationale.append("It materially outperformed Go on throughput and p95 latency in this synthetic workload.")
        else:
            rationale.append("No alternative showed a decisive win over Go on both throughput and p95 latency.")
        if top_compile > go_compile * 3.0:
            rationale.append("Compile-time cost is significantly higher than Go for the current best-performing alternative.")
        if top_stats["ccn_avg"] > go_stats["ccn_avg"] * 1.20:
            rationale.append("Per-function cyclomatic complexity is notably higher than Go in this draft implementation.")

    lines.append(f"- Recommendation: **{recommendation}**")
    for item in rationale:
        lines.append(f"- {item}")
    lines.append("")
    lines.append("## Caveats")
    lines.append("")
    lines.append("- Java benchmark uses lightweight fixed-schema extraction for request fields due stdlib JSON constraints; this can favor Java in this synthetic setup.")
    lines.append("- This does not model websocket fanout, SQLite contention, subprocess/tool execution, or real network latency.")
    lines.append("- Use this as directional input, not final proof for a full-language migration.")
    lines.append("")
    return "\n".join(lines) + "\n"


def main():
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    cleanup_for_compile()

    lizard_python = ensure_lizard_python()
    source_stats = collect_source_stats(lizard_python)

    _, go_version = run_command(["go", "version"], cwd=ROOT)
    _, node_version = run_command(["node", "--version"], cwd=ROOT)
    _, rust_version = run_command(["rustc", "--version"], cwd=ROOT)
    _, java_version = run_command(["javac", "-version"], cwd=ROOT)
    _, dotnet_version = run_command(["dotnet", "--version"], cwd=ROOT)

    runs = [
        bench_go(),
        bench_python(),
        bench_typescript(),
        bench_java(),
        bench_csharp(),
        bench_rust(),
    ]

    payload = {
        "metadata": {
            "timestamp_utc": datetime.now(timezone.utc).isoformat(),
            "host": platform.node(),
            "os": f"{platform.system()} {platform.release()}",
            "python": platform.python_version(),
            "go": go_version,
            "node": node_version,
            "rust": rust_version,
            "java": java_version,
            "dotnet": dotnet_version,
            "workload": "decode/classify/hash/encode",
        },
        "source_stats": source_stats,
        "runs": runs,
    }

    LATEST_JSON.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    report = make_report(payload)
    LATEST_REPORT.write_text(report, encoding="utf-8")
    print(report)
    print(f"\nWrote:\n- {LATEST_JSON}\n- {LATEST_REPORT}")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"benchmark run failed: {exc}", file=sys.stderr)
        sys.exit(1)
