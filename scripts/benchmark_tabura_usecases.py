#!/usr/bin/env python3
"""Benchmark Tabura-aligned use cases on local GGUF models via llama-server.

Use cases:
1) Fast intent classification (EN + DE)
2) Delegation policy (delegate or handle directly)
3) Tool/web-search routing with mock tool schema (EN + DE)
4) Instant feedback gating (whether to acknowledge long/tool tasks immediately)

Metrics:
- Latency and throughput per request
- Accuracy per scenario
- Confusion matrix (TP/FP/FN/TN) for binary decisions
- JSON validity rate
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

SCENARIO_INTENT = "intent_classification"
SCENARIO_DELEGATION = "delegation_policy"
SCENARIO_TOOL_ROUTING = "tool_routing"
SCENARIO_FEEDBACK = "instant_feedback_gate"

INTENT_SYSTEM_PROMPT = """You are Tabura Hub intent router.
Return JSON only with this schema:
{"action":"switch_project|switch_model|toggle_silent|toggle_conversation|cancel_work|show_status|delegate|chat","alias":"codex|gpt|spark","effort":"low|medium|high|xhigh","name":"","task":""}
Rules:
- If user asks to change project, use switch_project with name.
- If user asks to change model, use switch_model with alias and optional effort.
- If user asks to stop work, use cancel_work.
- If user asks for status, use show_status.
- If user asks to delegate work to another model, use delegate and include task.
- Otherwise use chat.
- Output strictly one JSON object and no prose."""

DELEGATION_SYSTEM_PROMPT = """You are a delegation policy classifier for Tabura.
Return JSON only:
{"delegate":true|false,"model":"codex|gpt|spark|none","instant_feedback":true|false,"feedback":""}
Rules:
- Delegate=true for complex multi-file coding, deep analysis, or web-research tasks.
- Choose codex for coding/refactor/debug tasks.
- Choose gpt for broad analysis/research/writing tasks.
- Choose spark only when user explicitly asks for spark or quick cheap pass.
- If delegate=true then instant_feedback=true and feedback must be a short confirmation in user language.
- If delegate=false then model="none", instant_feedback=false, feedback=""."""

TOOL_ROUTING_SYSTEM_PROMPT = """You route tool usage for Tabura.
Available mock tools:
1) weather(city)
2) web_search(query, lang)
3) show_status()
4) none()

Return JSON only:
{"tool":"weather|web_search|show_status|none","args":{},"instant_feedback":true|false,"feedback":""}

Rules:
- weather questions => weather(city)
- web/news/recent/facts lookup => web_search(query,lang)
- status requests => show_status()
- casual chat => none()
- For weather/web_search, instant_feedback must be true with short acknowledgement in user language.
- For show_status/none, instant_feedback should be false."""

FEEDBACK_GATE_SYSTEM_PROMPT = """You decide whether immediate short feedback is needed in voice UX.
Return JSON only:
{"needs_ack":true|false,"feedback":""}

Rules:
- needs_ack=true for long/delegated/tool/web-search tasks.
- needs_ack=false for short direct replies.
- If needs_ack=true, feedback must be short and in user language.
- If needs_ack=false, feedback must be empty."""

SCENARIOS: list[dict[str, Any]] = [
    {
        "name": SCENARIO_INTENT,
        "system_prompt": INTENT_SYSTEM_PROMPT,
        "max_tokens": 96,
        "examples": [
            {"lang": "en", "text": "switch to codex with high effort", "expected": {"action": "switch_model"}},
            {"lang": "en", "text": "set model to gpt medium", "expected": {"action": "switch_model"}},
            {"lang": "en", "text": "switch to project backend", "expected": {"action": "switch_project"}},
            {"lang": "en", "text": "cancel current work now", "expected": {"action": "cancel_work"}},
            {"lang": "en", "text": "show me status", "expected": {"action": "show_status"}},
            {"lang": "en", "text": "toggle conversation mode", "expected": {"action": "toggle_conversation"}},
            {"lang": "en", "text": "please delegate to codex: fix flaky tests", "expected": {"action": "delegate"}},
            {"lang": "en", "text": "tell me a short joke", "expected": {"action": "chat"}},
            {"lang": "de", "text": "wechsle auf projekt alpha", "expected": {"action": "switch_project"}},
            {"lang": "de", "text": "nutze codex mit hoher stufe", "expected": {"action": "switch_model"}},
            {"lang": "de", "text": "bitte laufende arbeit abbrechen", "expected": {"action": "cancel_work"}},
            {"lang": "de", "text": "zeige mir den status", "expected": {"action": "show_status"}},
            {"lang": "de", "text": "schalte auf lautlos", "expected": {"action": "toggle_silent"}},
            {"lang": "de", "text": "delegiere an gpt und recherchiere cloud preise", "expected": {"action": "delegate"}},
            {"lang": "de", "text": "erzähl mir kurz was neues", "expected": {"action": "chat"}},
        ],
    },
    {
        "name": SCENARIO_DELEGATION,
        "system_prompt": DELEGATION_SYSTEM_PROMPT,
        "max_tokens": 96,
        "examples": [
            {
                "lang": "en",
                "text": "Refactor auth module across the repo and fix failing integration tests.",
                "expected": {"delegate": True, "model": "codex", "instant_feedback": True},
            },
            {"lang": "en", "text": "Rename one variable in this snippet.", "expected": {"delegate": False, "model": "none", "instant_feedback": False}},
            {
                "lang": "en",
                "text": "Use the big model to compare three cloud providers with current market trends.",
                "expected": {"delegate": True, "model": "gpt", "instant_feedback": True},
            },
            {"lang": "en", "text": "Translate this one sentence to German.", "expected": {"delegate": False, "model": "none", "instant_feedback": False}},
            {
                "lang": "en",
                "text": "Use spark to quickly check formatting only.",
                "expected": {"delegate": True, "model": "spark", "instant_feedback": True},
            },
            {
                "lang": "de",
                "text": "Bitte analysiere die gesamte codebasis und erstelle einen migrationsplan.",
                "expected": {"delegate": True, "model": "codex", "instant_feedback": True},
            },
            {
                "lang": "de",
                "text": "Nutze das grosse modell fuer eine marktanalyse.",
                "expected": {"delegate": True, "model": "gpt", "instant_feedback": True},
            },
            {"lang": "de", "text": "Sag einfach hallo.", "expected": {"delegate": False, "model": "none", "instant_feedback": False}},
            {
                "lang": "de",
                "text": "Bitte lass spark nur kurz eine kleine format-pruefung machen.",
                "expected": {"delegate": True, "model": "spark", "instant_feedback": True},
            },
            {"lang": "de", "text": "Nenne mir die aktuelle uhrzeit.", "expected": {"delegate": False, "model": "none", "instant_feedback": False}},
        ],
    },
    {
        "name": SCENARIO_TOOL_ROUTING,
        "system_prompt": TOOL_ROUTING_SYSTEM_PROMPT,
        "max_tokens": 96,
        "examples": [
            {"lang": "en", "text": "What's the weather in Vienna today?", "expected": {"tool": "weather", "city": "Vienna", "instant_feedback": True}},
            {"lang": "de", "text": "Wie ist das wetter in Berlin?", "expected": {"tool": "weather", "city": "Berlin", "instant_feedback": True}},
            {
                "lang": "en",
                "text": "Search the web for latest Linux suspend bug Nvidia driver.",
                "expected": {"tool": "web_search", "query_keyword": "linux", "instant_feedback": True},
            },
            {
                "lang": "de",
                "text": "Suche im web nach aktuellen OpenAI API preisen.",
                "expected": {"tool": "web_search", "query_keyword": "preise", "instant_feedback": True},
            },
            {"lang": "en", "text": "Show current project status.", "expected": {"tool": "show_status", "instant_feedback": False}},
            {"lang": "de", "text": "Zeig den aktuellen projektstatus.", "expected": {"tool": "show_status", "instant_feedback": False}},
            {"lang": "en", "text": "Tell me a joke about databases.", "expected": {"tool": "none", "instant_feedback": False}},
            {"lang": "de", "text": "Erzaehl mir einen witz.", "expected": {"tool": "none", "instant_feedback": False}},
            {
                "lang": "en",
                "text": "Find web results about Tabura release notes.",
                "expected": {"tool": "web_search", "query_keyword": "tabura", "instant_feedback": True},
            },
            {"lang": "de", "text": "Wetter in Muenchen bitte.", "expected": {"tool": "weather", "city": "Muenchen", "instant_feedback": True}},
        ],
    },
    {
        "name": SCENARIO_FEEDBACK,
        "system_prompt": FEEDBACK_GATE_SYSTEM_PROMPT,
        "max_tokens": 64,
        "examples": [
            {"lang": "en", "text": "Refactor this repo and fix failing tests.", "expected": {"needs_ack": True}},
            {"lang": "en", "text": "What is 2+2?", "expected": {"needs_ack": False}},
            {"lang": "en", "text": "Search the web for latest CVE updates.", "expected": {"needs_ack": True}},
            {"lang": "en", "text": "Translate hello to German.", "expected": {"needs_ack": False}},
            {"lang": "de", "text": "Bitte recherchiere aktuelle cloud preise im web.", "expected": {"needs_ack": True}},
            {"lang": "de", "text": "Wie spaet ist es?", "expected": {"needs_ack": False}},
            {"lang": "de", "text": "Bitte delegiere eine grosse code-analyse.", "expected": {"needs_ack": True}},
            {"lang": "de", "text": "Sag kurz hallo.", "expected": {"needs_ack": False}},
        ],
    },
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--llama-bin", default=os.getenv("LLAMA_SERVER_BIN", "llama-server"))
    parser.add_argument("--model-dir", default=os.getenv("TABURA_LLM_MODEL_DIR", str(Path.home() / ".local/share/tabura-llm/models")))
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=18427)
    parser.add_argument("--ctx-size", type=int, default=int(os.getenv("TABURA_LLM_CTX", "16384")))
    parser.add_argument("--threads", type=int, default=int(os.getenv("TABURA_LLM_THREADS", "4")))
    parser.add_argument("--ngl", type=int, default=int(os.getenv("TABURA_LLM_NGL", "99")))
    parser.add_argument("--models", default="0.8b,2b,4b,9b", help="Comma-separated subset: 0.8b,2b,4b,9b")
    parser.add_argument("--skip-download", action="store_true")
    parser.add_argument("--skip-missing", action="store_true", help="Skip missing models instead of failing.")
    parser.add_argument("--warmup-runs", type=int, default=2)
    parser.add_argument("--results-json", default="", help="Optional output path. Default: .tabura/artifacts/benchmarks/")
    return parser.parse_args()


def ensure_model(model_dir: Path, spec: ModelSpec, skip_download: bool, skip_missing: bool) -> Path | None:
    model_dir.mkdir(parents=True, exist_ok=True)
    model_path = model_dir / spec.filename
    if model_path.exists() and model_path.stat().st_size > 0:
        return model_path
    if skip_download:
        if skip_missing:
            print(f"[skip] missing model: {model_path}")
            return None
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
            "-C",
            "-",
            "-o",
            str(model_path) + ".tmp",
            spec.url,
        ],
        check=True,
    )
    Path(str(model_path) + ".tmp").replace(model_path)
    return model_path


def wait_ready(base_url: str, timeout_s: float = 120.0) -> None:
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
    req = urllib.request.Request(
        base_url + "/v1/chat/completions",
        method="POST",
        headers={"Content-Type": "application/json"},
        data=json.dumps(payload).encode("utf-8"),
    )
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as err:
        detail = err.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {err.code}: {detail}") from err
    dt = time.perf_counter() - t0
    return json.loads(raw.decode("utf-8", errors="replace")), dt


def strip_code_fences(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        lines = t.splitlines()
        if len(lines) >= 2 and lines[-1].strip() == "```":
            return "\n".join(lines[1:-1]).strip()
    return t


def extract_json_object(text: str) -> dict[str, Any] | None:
    t = strip_code_fences(text)
    try:
        obj = json.loads(t)
        if isinstance(obj, dict):
            return obj
    except Exception:
        pass
    start = t.find("{")
    if start < 0:
        return None
    depth = 0
    for i in range(start, len(t)):
        ch = t[i]
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                candidate = t[start : i + 1]
                try:
                    obj = json.loads(candidate)
                    if isinstance(obj, dict):
                        return obj
                except Exception:
                    return None
    return None


def as_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return value != 0
    text = str(value).strip().lower()
    return text in {"true", "1", "yes", "y"}


def as_text(value: Any) -> str:
    return str(value).strip().lower()


def p95(values: list[float]) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    idx = max(0, int(len(ordered) * 0.95) - 1)
    return float(ordered[idx])


def safe_pct(n: int, d: int) -> float:
    if d <= 0:
        return 0.0
    return (100.0 * n) / float(d)


def confusion_stats(tp: int, fp: int, fn: int, tn: int) -> dict[str, float]:
    precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
    recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
    f1 = (2 * precision * recall / (precision + recall)) if (precision + recall) > 0 else 0.0
    return {"precision": precision, "recall": recall, "f1": f1}


def evaluate_case(scenario: str, expected: dict[str, Any], parsed: dict[str, Any] | None) -> dict[str, Any]:
    out: dict[str, Any] = {"valid_json": parsed is not None, "main_correct": False}
    if parsed is None:
        if scenario == SCENARIO_INTENT:
            out.update({"expected_positive": expected["action"] != "chat", "pred_positive": False})
        elif scenario == SCENARIO_DELEGATION:
            out.update({"expected_positive": bool(expected["delegate"]), "pred_positive": False})
        elif scenario == SCENARIO_TOOL_ROUTING:
            out.update({"expected_positive": expected["tool"] != "none", "pred_positive": False})
        elif scenario == SCENARIO_FEEDBACK:
            out.update({"expected_positive": bool(expected["needs_ack"]), "pred_positive": False})
        return out

    if scenario == SCENARIO_INTENT:
        pred_action = as_text(parsed.get("action", ""))
        expected_action = as_text(expected.get("action", ""))
        expected_pos = expected_action != "chat"
        pred_pos = pred_action != "" and pred_action != "chat"
        out.update(
            {
                "pred_action": pred_action,
                "expected_positive": expected_pos,
                "pred_positive": pred_pos,
                "main_correct": pred_action == expected_action,
            }
        )
        return out

    if scenario == SCENARIO_DELEGATION:
        pred_delegate = as_bool(parsed.get("delegate"))
        expected_delegate = bool(expected.get("delegate"))
        pred_model = as_text(parsed.get("model", ""))
        expected_model = as_text(expected.get("model", ""))
        pred_feedback = as_bool(parsed.get("instant_feedback"))
        expected_feedback = bool(expected.get("instant_feedback"))
        model_correct = (not expected_delegate and pred_model in {"none", ""}) or (
            expected_delegate and pred_model == expected_model
        )
        out.update(
            {
                "expected_positive": expected_delegate,
                "pred_positive": pred_delegate,
                "main_correct": pred_delegate == expected_delegate,
                "model_correct": model_correct,
                "feedback_correct": pred_feedback == expected_feedback,
            }
        )
        return out

    if scenario == SCENARIO_TOOL_ROUTING:
        pred_tool = as_text(parsed.get("tool", ""))
        expected_tool = as_text(expected.get("tool", ""))
        expected_pos = expected_tool != "none"
        pred_pos = pred_tool != "" and pred_tool != "none"
        args = parsed.get("args")
        if not isinstance(args, dict):
            args = {}
        arg_ok = True
        if expected_tool == "weather":
            city = as_text(args.get("city", ""))
            exp_city = as_text(expected.get("city", ""))
            arg_ok = exp_city in city if exp_city else city != ""
        elif expected_tool == "web_search":
            query = as_text(args.get("query", ""))
            key = as_text(expected.get("query_keyword", ""))
            arg_ok = (key in query) if key else query != ""
        pred_feedback = as_bool(parsed.get("instant_feedback"))
        expected_feedback = bool(expected.get("instant_feedback"))
        out.update(
            {
                "expected_positive": expected_pos,
                "pred_positive": pred_pos,
                "main_correct": pred_tool == expected_tool,
                "arg_correct": arg_ok,
                "feedback_correct": pred_feedback == expected_feedback,
            }
        )
        return out

    if scenario == SCENARIO_FEEDBACK:
        pred_needs_ack = as_bool(parsed.get("needs_ack"))
        expected_needs_ack = bool(expected.get("needs_ack"))
        feedback = str(parsed.get("feedback", "")).strip()
        feedback_ok = (not expected_needs_ack and feedback == "") or (expected_needs_ack and feedback != "")
        out.update(
            {
                "expected_positive": expected_needs_ack,
                "pred_positive": pred_needs_ack,
                "main_correct": pred_needs_ack == expected_needs_ack,
                "feedback_correct": feedback_ok,
            }
        )
        return out

    return out


def summarize_scenario(name: str, rows: list[dict[str, Any]]) -> dict[str, Any]:
    latencies = [float(r["latency_ms"]) for r in rows]
    throughputs = [float(r["throughput_tps"]) for r in rows if float(r["throughput_tps"]) > 0]
    valid = sum(1 for r in rows if r["eval"]["valid_json"])
    correct = sum(1 for r in rows if r["eval"]["main_correct"])

    tp = fp = fn = tn = 0
    for r in rows:
        ev = r["eval"]
        if "expected_positive" not in ev or "pred_positive" not in ev:
            continue
        exp = bool(ev["expected_positive"])
        pred = bool(ev["pred_positive"])
        if exp and pred:
            tp += 1
        elif exp and (not pred):
            fn += 1
        elif (not exp) and pred:
            fp += 1
        else:
            tn += 1
    conf = confusion_stats(tp, fp, fn, tn)

    by_lang: dict[str, dict[str, int]] = {}
    for r in rows:
        lang = str(r.get("lang", "")).strip().lower() or "unknown"
        bucket = by_lang.setdefault(lang, {"total": 0, "correct": 0})
        bucket["total"] += 1
        if r["eval"]["main_correct"]:
            bucket["correct"] += 1

    extra: dict[str, float] = {}
    if name == SCENARIO_DELEGATION:
        extra["model_accuracy_pct"] = safe_pct(sum(1 for r in rows if r["eval"].get("model_correct")), len(rows))
        extra["feedback_accuracy_pct"] = safe_pct(sum(1 for r in rows if r["eval"].get("feedback_correct")), len(rows))
    elif name == SCENARIO_TOOL_ROUTING:
        extra["arg_accuracy_pct"] = safe_pct(sum(1 for r in rows if r["eval"].get("arg_correct")), len(rows))
        extra["feedback_accuracy_pct"] = safe_pct(sum(1 for r in rows if r["eval"].get("feedback_correct")), len(rows))
    elif name == SCENARIO_FEEDBACK:
        extra["feedback_accuracy_pct"] = safe_pct(sum(1 for r in rows if r["eval"].get("feedback_correct")), len(rows))

    return {
        "scenario": name,
        "requests": len(rows),
        "valid_json_pct": round(safe_pct(valid, len(rows)), 2),
        "accuracy_pct": round(safe_pct(correct, len(rows)), 2),
        "latency_ms_median": round(statistics.median(latencies), 2) if latencies else 0.0,
        "latency_ms_p95": round(p95(latencies), 2),
        "throughput_tps_median": round(statistics.median(throughputs), 2) if throughputs else 0.0,
        "confusion": {
            "tp": tp,
            "fp": fp,
            "fn": fn,
            "tn": tn,
            "precision": round(conf["precision"], 4),
            "recall": round(conf["recall"], 4),
            "f1": round(conf["f1"], 4),
        },
        "accuracy_by_lang_pct": {
            lang: round(safe_pct(v["correct"], v["total"]), 2) for lang, v in by_lang.items()
        },
        "extra": extra,
    }


def benchmark_model(spec: ModelSpec, model_path: Path, args: argparse.Namespace, artifacts_dir: Path) -> dict[str, Any]:
    base_url = f"http://{args.host}:{args.port}"
    log_file = artifacts_dir / "logs" / f"usecases-{spec.key}.log"
    proc = None
    try:
        print(f"[start] {spec.name}")
        proc = start_llama_server(args, model_path, log_file)
        wait_ready(base_url)

        # Global warmup.
        first = SCENARIOS[0]["examples"][0]
        warmup_payload = {
            "model": "local",
            "messages": [
                {"role": "system", "content": SCENARIOS[0]["system_prompt"]},
                {"role": "user", "content": first["text"]},
            ],
            "max_tokens": 64,
            "temperature": 0.7,
            "top_p": 0.8,
            "top_k": 20,
            "min_p": 0.0,
            "presence_penalty": 1.5,
            "repetition_penalty": 1.0,
            "chat_template_kwargs": {"enable_thinking": False},
        }
        for _ in range(args.warmup_runs):
            _resp, _dt = post_chat_completion(base_url, warmup_payload)

        per_scenario_rows: dict[str, list[dict[str, Any]]] = {s["name"]: [] for s in SCENARIOS}
        all_rows: list[dict[str, Any]] = []

        for scenario in SCENARIOS:
            for idx, ex in enumerate(scenario["examples"], start=1):
                payload = {
                    "model": "local",
                    "messages": [
                        {"role": "system", "content": scenario["system_prompt"]},
                        {"role": "user", "content": ex["text"]},
                    ],
                    "max_tokens": int(scenario["max_tokens"]),
                    "temperature": 0.7,
                    "top_p": 0.8,
                    "top_k": 20,
                    "min_p": 0.0,
                    "presence_penalty": 1.5,
                    "repetition_penalty": 1.0,
                    "chat_template_kwargs": {"enable_thinking": False},
                }
                resp, dt_s = post_chat_completion(base_url, payload)
                usage = resp.get("usage") or {}
                timings = resp.get("timings") or {}
                completion_tokens = int(usage.get("completion_tokens") or 0)
                predicted_per_second = timings.get("predicted_per_second")
                if isinstance(predicted_per_second, (int, float)):
                    throughput_tps = float(predicted_per_second)
                else:
                    throughput_tps = (completion_tokens / dt_s) if completion_tokens > 0 and dt_s > 0 else 0.0
                text = str(((resp.get("choices") or [{}])[0].get("message") or {}).get("content") or "")
                parsed = extract_json_object(text)
                ev = evaluate_case(scenario["name"], ex["expected"], parsed)
                row = {
                    "scenario": scenario["name"],
                    "case": idx,
                    "lang": ex["lang"],
                    "input": ex["text"],
                    "expected": ex["expected"],
                    "latency_ms": round(dt_s * 1000.0, 2),
                    "throughput_tps": round(throughput_tps, 2),
                    "completion_tokens": completion_tokens,
                    "raw_output": text,
                    "parsed_output": parsed,
                    "eval": ev,
                }
                per_scenario_rows[scenario["name"]].append(row)
                all_rows.append(row)

        scenario_summaries = [summarize_scenario(name, rows) for name, rows in per_scenario_rows.items()]
        all_lat = [float(r["latency_ms"]) for r in all_rows]
        all_tps = [float(r["throughput_tps"]) for r in all_rows if float(r["throughput_tps"]) > 0]
        main_acc_values = [float(s["accuracy_pct"]) for s in scenario_summaries]
        return {
            "model_key": spec.key,
            "model_name": spec.name,
            "model_path": str(model_path),
            "summary": {
                "overall_latency_ms_median": round(statistics.median(all_lat), 2) if all_lat else 0.0,
                "overall_latency_ms_p95": round(p95(all_lat), 2),
                "overall_throughput_tps_median": round(statistics.median(all_tps), 2) if all_tps else 0.0,
                "overall_main_accuracy_pct_avg": round(statistics.mean(main_acc_values), 2) if main_acc_values else 0.0,
                "requests_total": len(all_rows),
            },
            "scenario_summaries": scenario_summaries,
            "rows": all_rows,
        }
    finally:
        stop_llama_server(proc)


def default_results_path(repo_root: Path) -> Path:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    out_dir = repo_root / ".tabura" / "artifacts" / "benchmarks"
    out_dir.mkdir(parents=True, exist_ok=True)
    return out_dir / f"tabura-usecases-benchmark-{stamp}.json"


def print_summary(results: list[dict[str, Any]]) -> None:
    print("\n=== Tabura Use-Case Benchmark (non-thinking, EN+DE) ===")
    header = (
        f"{'model':<22} {'med_ms':>8} {'p95_ms':>8} {'med_tps':>10} "
        f"{'acc_avg%':>10} {'n':>6}"
    )
    print(header)
    print("-" * len(header))
    for r in results:
        s = r["summary"]
        print(
            f"{r['model_name']:<22} "
            f"{s['overall_latency_ms_median']:>8.2f} "
            f"{s['overall_latency_ms_p95']:>8.2f} "
            f"{s['overall_throughput_tps_median']:>10.2f} "
            f"{s['overall_main_accuracy_pct_avg']:>10.2f} "
            f"{s['requests_total']:>6d}"
        )

    for scenario_name in [SCENARIO_INTENT, SCENARIO_DELEGATION, SCENARIO_TOOL_ROUTING, SCENARIO_FEEDBACK]:
        print(f"\n--- {scenario_name} ---")
        header = (
            f"{'model':<22} {'lat_ms':>8} {'acc%':>8} {'json%':>8} "
            f"{'f1':>7} {'fp':>4} {'fn':>4} {'en%':>7} {'de%':>7}"
        )
        print(header)
        print("-" * len(header))
        for r in results:
            s = next((x for x in r["scenario_summaries"] if x["scenario"] == scenario_name), None)
            if not s:
                continue
            lang = s.get("accuracy_by_lang_pct", {})
            print(
                f"{r['model_name']:<22} "
                f"{s['latency_ms_median']:>8.2f} "
                f"{s['accuracy_pct']:>8.2f} "
                f"{s['valid_json_pct']:>8.2f} "
                f"{s['confusion']['f1']:>7.3f} "
                f"{s['confusion']['fp']:>4d} "
                f"{s['confusion']['fn']:>4d} "
                f"{lang.get('en', 0.0):>7.2f} "
                f"{lang.get('de', 0.0):>7.2f}"
            )


def main() -> int:
    args = parse_args()
    model_keys = {part.strip().lower() for part in args.models.split(",") if part.strip()}
    selected = [m for m in MODEL_SPECS if m.key in model_keys]
    if not selected:
        raise SystemExit("No valid models selected. Expected subset of 0.8b,2b,4b,9b.")

    repo_root = Path(__file__).resolve().parents[1]
    artifacts_dir = repo_root / ".tabura" / "artifacts"
    model_dir = Path(args.model_dir).expanduser().resolve()

    results: list[dict[str, Any]] = []
    for spec in selected:
        model_path = ensure_model(model_dir, spec, args.skip_download, args.skip_missing)
        if model_path is None:
            continue
        results.append(benchmark_model(spec, model_path, args, artifacts_dir))

    if not results:
        raise SystemExit("No benchmarks executed (all selected models missing/skipped).")

    print_summary(results)
    output_path = Path(args.results_json).expanduser().resolve() if args.results_json else default_results_path(repo_root)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps({"results": results}, indent=2), encoding="utf-8")
    print(f"\nSaved JSON report: {output_path}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("\nInterrupted.", file=sys.stderr)
        raise SystemExit(130)
