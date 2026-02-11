from __future__ import annotations

import os
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Literal, Mapping

from .protocol import (
    ARTIFACT_DIR,
    BootstrapResult,
    bootstrap_project,
    load_injection_text,
)
from .runner import RunResult, SubprocessRunner

CodexMode = Literal["project", "global"]


@dataclass(frozen=True)
class WorkflowResult:
    returncode: int
    message: str


def build_codex_prompt(
    user_prompt: str,
    *,
    artifacts_dir: Path,
    events_path: Path,
    injection_text: str,
) -> str:
    segments = [
        "You are running inside a Tabula interactive session.",
        "Session constraints:",
        f"- Keep artifacts under: {artifacts_dir.as_posix()}",
        f"- Emit strict JSONL canvas events to: {events_path.as_posix()}",
        "- Follow AGENTS.md protocol in this project.",
        "- Do not replace terminal interaction with a custom REPL.",
        "User task:",
        user_prompt,
    ]
    if injection_text:
        segments.extend(["Extra instructions:", injection_text])
    return "\n".join(segments)


def build_codex_command(prompt: str, *, project_dir: Path, mode: CodexMode) -> list[str]:
    cmd = ["codex", "-C", str(project_dir)]
    if mode == "global":
        cmd.append("--skip-git-repo-check")
    cmd.append(prompt)
    return cmd


def has_display(env: Mapping[str, str] | None = None) -> bool:
    source = env or os.environ
    return bool(source.get("DISPLAY") or source.get("WAYLAND_DISPLAY"))


def launch_canvas_background(project_dir: Path, events_path: Path, *, poll_interval_ms: int = 250) -> subprocess.Popen[bytes]:
    cmd = [
        sys.executable,
        "-m",
        "tabula",
        "canvas",
        "--events",
        str(events_path),
        "--poll-ms",
        str(poll_interval_ms),
    ]
    return subprocess.Popen(cmd, cwd=project_dir)


def run_codex_interactive(prompt: str, *, project_dir: Path, mode: CodexMode, runner: SubprocessRunner) -> RunResult:
    cmd = build_codex_command(prompt, project_dir=project_dir, mode=mode)
    return runner.run_interactive(cmd, cwd=project_dir)


def run_tabula_session(
    *,
    user_prompt: str,
    project_dir: Path,
    mode: CodexMode,
    headless: bool = False,
    poll_interval_ms: int = 250,
    start_canvas: bool = True,
    env: Mapping[str, str] | None = None,
    artifacts_rel: Path = ARTIFACT_DIR,
    runner: SubprocessRunner | None = None,
) -> WorkflowResult:
    command_runner = runner or SubprocessRunner()
    bootstrap: BootstrapResult = bootstrap_project(
        project_dir,
        artifacts_rel=artifacts_rel,
        runner=command_runner,
    )
    paths = bootstrap.paths
    injection_text = load_injection_text(paths.injection_path)

    display_ready = has_display(env)
    effective_headless = headless or not display_ready
    if start_canvas and not effective_headless:
        try:
            launch_canvas_background(paths.project_dir, paths.events_path, poll_interval_ms=poll_interval_ms)
        except OSError as exc:
            return WorkflowResult(returncode=1, message=f"failed to launch canvas window: {exc}")

    prompt = build_codex_prompt(
        user_prompt,
        artifacts_dir=paths.artifacts_dir,
        events_path=paths.events_path,
        injection_text=injection_text,
    )
    codex_result = run_codex_interactive(prompt, project_dir=paths.project_dir, mode=mode, runner=command_runner)
    if codex_result.returncode != 0:
        return WorkflowResult(returncode=codex_result.returncode, message="codex session failed")

    mode_note = "headless" if effective_headless else "canvas"
    return WorkflowResult(returncode=0, message=f"tabula completed ({mode_note})")
