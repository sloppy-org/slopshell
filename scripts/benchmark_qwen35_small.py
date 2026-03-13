#!/usr/bin/env python3
"""Benchmark small Qwen GGUF models on llama-server.

Measures:
- end-to-end latency per request (ms)
- throughput from llama.cpp timings (predicted_per_second)
- fallback throughput estimate from completion_tokens / latency

All requests are sent in non-thinking mode using:
chat_template_kwargs={"enable_thinking": false}
"""

from __future__ import annotations

import argparse
import json
import os
import signal
import statistics
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class ModelSpec:
    key: str
    name: str
    filename: str
    url: str


MODEL_SPECS = [
    ModelSpec(
        key="0.8b",
        name="Qwen3.5-0.8B-Q4_K_M",
        filename="Qwen3.5-0.8B-Q4_K_M.gguf",
        url="https://huggingface.co/lmstudio-community/Qwen3.5-0.8B-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf?download=true",
    ),
    ModelSpec(
        key="2b",
        name="Qwen3.5-2B-Q4_K_M",
        filename="Qwen3.5-2B-Q4_K_M.gguf",
        url="https://huggingface.co/lmstudio-community/Qwen3.5-2B-GGUF/resolve/main/Qwen3.5-2B-Q4_K_M.gguf?download=true",
    ),
    ModelSpec(
        key="4b",
        name="Qwen3.5-4B-Q4_K_M",
        filename="Qwen3.5-4B-Q4_K_M.gguf",
        url="https://huggingface.co/lmstudio-community/Qwen3.5-4B-GGUF/resolve/main/Qwen3.5-4B-Q4_K_M.gguf?download=true",
    ),
    ModelSpec(
        key="9b",
        name="Qwen3.5-9B-Q4_K_M",
        filename="Qwen3.5-9B-Q4_K_M.gguf",
        url="https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true",
    ),
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--llama-bin", default=os.getenv("LLAMA_SERVER_BIN", "llama-server"))
    parser.add_argument("--model-dir", default=os.getenv("TABURA_LLM_MODEL_DIR", str(Path.home() / ".local/share/tabura-llm/models")))
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=18426)
    parser.add_argument("--ctx-size", type=int, default=int(os.getenv("TABURA_LLM_CTX", "16384")))
    parser.add_argument("--threads", type=int, default=int(os.getenv("TABURA_LLM_THREADS", "4")))
    parser.add_argument("--ngl", type=int, default=int(os.getenv("TABURA_LLM_NGL", "99")))
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--warmup-runs", type=int, default=1)
    parser.add_argument("--max-tokens", type=int, default=128)
    parser.add_argument(
        "--prompt",
        default="What is the weather in Vienna right now? Reply in one short sentence.",
    )
    parser.add_argument(
        "--models",
        default="0.8b,2b,4b,9b",
        help="Comma-separated subset: 0.8b,2b,4b,9b",
    )
    parser.add_argument("--skip-download", action="store_true")
    parser.add_argument(
        "--results-json",
        default="",
        help="Optional explicit output path; default writes into .tabura/artifacts/benchmarks/",
    )
    return parser.parse_args()


def ensure_model(model_dir: Path, spec: ModelSpec, skip_download: bool) -> Path:
    model_dir.mkdir(parents=True, exist_ok=True)
    model_path = model_dir / spec.filename
    if model_path.exists() and model_path.stat().st_size > 0:
        return model_path
    if skip_download:
        raise RuntimeError(f"model missing and --skip-download is set: {model_path}")
    print(f"[download] {spec.name} -> {model_path}")
    subprocess.run(
        [
            "curl",
            "-fL",
            "--retry",
            "3",
            "--retry-delay",
            "2",
            "-o",
            str(model_path) + ".tmp",
            spec.url,
        ],
        check=True,
    )
    Path(str(model_path) + ".tmp").replace(model_path)
    return model_path


def wait_ready(base_url: str, timeout_s: float = 90.0) -> None:
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        for path in ("/health", "/v1/models"):
            try:
                with urllib.request.urlopen(base_url + path, timeout=2.5) as resp:
                    if resp.status == 200:
                        return
            except Exception:
                pass
        time.sleep(0.4)
    raise RuntimeError(f"llama-server did not become ready within {timeout_s}s")


def start_llama_server(args: argparse.Namespace, model_path: Path, log_file: Path) -> subprocess.Popen[Any]:
    cmd = [
        args.llama_bin,
        "-m",
        str(model_path),
        "--host",
        args.host,
        "--port",
        str(args.port),
        "-c",
        str(args.ctx_size),
        "--threads",
        str(args.threads),
        "-ngl",
        str(args.ngl),
    ]
    log_file.parent.mkdir(parents=True, exist_ok=True)
    log_handle = open(log_file, "w", encoding="utf-8")
    proc = subprocess.Popen(cmd, stdout=log_handle, stderr=subprocess.STDOUT, preexec_fn=os.setsid)
    proc._tabura_log_handle = log_handle  # type: ignore[attr-defined]
    return proc


def stop_llama_server(proc: subprocess.Popen[Any] | None) -> None:
    if proc is None:
        return
    if proc.poll() is None:
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except Exception:
            pass
        try:
            proc.wait(timeout=12)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except Exception:
                pass
            proc.wait(timeout=5)
    log_handle = getattr(proc, "_tabura_log_handle", None)
    if log_handle:
        try:
            log_handle.close()
        except Exception:
            pass


def post_chat_completion(base_url: str, payload: dict[str, Any], timeout_s: float = 90.0) -> tuple[dict[str, Any], float]:
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        base_url + "/v1/chat/completions",
        method="POST",
        headers={"Content-Type": "application/json"},
        data=body,
    )
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as err:
        detail = err.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {err.code}: {detail}") from err
    dt = time.perf_counter() - t0
    data = json.loads(raw.decode("utf-8", errors="replace"))
    return data, dt


def benchmark_model(spec: ModelSpec, model_path: Path, args: argparse.Namespace, artifacts_dir: Path) -> dict[str, Any]:
    base_url = f"http://{args.host}:{args.port}"
    log_file = artifacts_dir / "logs" / f"{spec.key}.log"
    proc = None
    try:
        print(f"[start] {spec.name}")
        proc = start_llama_server(args, model_path, log_file)
        wait_ready(base_url)

        payload = {
            "model": "local",
            "messages": [{"role": "user", "content": args.prompt}],
            "max_tokens": args.max_tokens,
            # Qwen3.5 recommended non-thinking sampling for general tasks.
            "temperature": 0.7,
            "top_p": 0.8,
            "top_k": 20,
            "min_p": 0.0,
            "presence_penalty": 1.5,
            "repetition_penalty": 1.0,
            "chat_template_kwargs": {"enable_thinking": False},
        }

        for _ in range(args.warmup_runs):
            _resp, _dt = post_chat_completion(base_url, payload)

        runs: list[dict[str, Any]] = []
        for i in range(args.runs):
            resp, dt_s = post_chat_completion(base_url, payload)
            usage = resp.get("usage") or {}
            timings = resp.get("timings") or {}
            completion_tokens = int(usage.get("completion_tokens") or 0)
            prompt_tokens = int(usage.get("prompt_tokens") or 0)
            predicted_per_second = timings.get("predicted_per_second")
            if isinstance(predicted_per_second, (int, float)):
                throughput_tps = float(predicted_per_second)
            else:
                throughput_tps = (completion_tokens / dt_s) if completion_tokens > 0 and dt_s > 0 else 0.0
            runs.append(
                {
                    "run": i + 1,
                    "latency_ms": round(dt_s * 1000.0, 2),
                    "completion_tokens": completion_tokens,
                    "prompt_tokens": prompt_tokens,
                    "throughput_tps": round(throughput_tps, 2),
                    "timings": timings,
                    "response_preview": str(((resp.get("choices") or [{}])[0].get("message") or {}).get("content") or "")[:160],
                }
            )

        latencies = [r["latency_ms"] for r in runs]
        throughputs = [r["throughput_tps"] for r in runs if r["throughput_tps"] > 0]
        completion_tokens = [r["completion_tokens"] for r in runs]
        return {
            "model_key": spec.key,
            "model_name": spec.name,
            "model_path": str(model_path),
            "runs": runs,
            "summary": {
                "median_latency_ms": round(statistics.median(latencies), 2),
                "p95_latency_ms": round(sorted(latencies)[max(0, int(len(latencies) * 0.95) - 1)], 2),
                "median_throughput_tps": round(statistics.median(throughputs), 2) if throughputs else 0.0,
                "median_completion_tokens": int(statistics.median(completion_tokens)) if completion_tokens else 0,
            },
        }
    finally:
        stop_llama_server(proc)


def default_results_path(repo_root: Path) -> Path:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    out_dir = repo_root / ".tabura" / "artifacts" / "benchmarks"
    out_dir.mkdir(parents=True, exist_ok=True)
    return out_dir / f"qwen35-small-benchmark-{stamp}.json"


def print_summary(results: list[dict[str, Any]]) -> None:
    print("\n=== Qwen3.5 Small Model Benchmark (non-thinking) ===")
    header = f"{'model':<22} {'median_latency_ms':>18} {'median_tps':>12} {'median_tokens':>14}"
    print(header)
    print("-" * len(header))
    for item in results:
        s = item["summary"]
        print(
            f"{item['model_name']:<22} "
            f"{s['median_latency_ms']:>18.2f} "
            f"{s['median_throughput_tps']:>12.2f} "
            f"{s['median_completion_tokens']:>14d}"
        )


def main() -> int:
    args = parse_args()
    model_keys = {part.strip().lower() for part in args.models.split(",") if part.strip()}
    selected = [m for m in MODEL_SPECS if m.key in model_keys]
    if not selected:
        raise SystemExit(
            f"No valid models selected from --models={args.models!r}. "
            "Expected subset of 0.8b,2b,4b,9b."
        )

    repo_root = Path(__file__).resolve().parents[1]
    artifacts_dir = repo_root / ".tabura" / "artifacts"
    model_dir = Path(args.model_dir).expanduser().resolve()

    all_results: list[dict[str, Any]] = []
    for spec in selected:
        model_path = ensure_model(model_dir, spec, args.skip_download)
        result = benchmark_model(spec, model_path, args, artifacts_dir)
        all_results.append(result)

    print_summary(all_results)
    output_path = Path(args.results_json).expanduser().resolve() if args.results_json else default_results_path(repo_root)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps({"results": all_results}, indent=2), encoding="utf-8")
    print(f"\nSaved JSON report: {output_path}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("\nInterrupted.", file=sys.stderr)
        raise SystemExit(130)
