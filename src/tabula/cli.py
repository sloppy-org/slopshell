from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import cast

from .events import EventValidationError, event_schema, parse_event_line
from .protocol import bootstrap_project
from .workflow import CodexMode
from .workflow import run_tabula_session


def _build_command_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="tabula")
    sub = parser.add_subparsers(dest="command", required=True)

    p_canvas = sub.add_parser("canvas", help="launch canvas window")
    p_canvas.add_argument("--events", type=Path, default=Path(".tabula/canvas-events.jsonl"))
    p_canvas.add_argument("--poll-ms", type=int, default=250)

    p_check = sub.add_parser("check-events", help="validate JSONL event file")
    p_check.add_argument("--events", type=Path, required=True)

    sub.add_parser("schema", help="print JSON schema")

    p_bootstrap = sub.add_parser("bootstrap", help="initialize tabula project protocol files")
    p_bootstrap.add_argument("--project-dir", type=Path, default=Path("."))
    return parser


def _build_mainflow_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="tabula")
    parser.add_argument("--project-dir", type=Path, default=Path("."))
    parser.add_argument("--mode", choices=["project", "global"], default="project")
    parser.add_argument("--prompt", help="task prompt; if omitted, positional text or interactive input is used")
    parser.add_argument("--headless", action="store_true", help="force headless mode even when display exists")
    parser.add_argument("--no-canvas", action="store_true", help="do not auto-launch canvas window")
    parser.add_argument("--poll-ms", type=int, default=250)
    parser.add_argument("prompt_parts", nargs="*")
    return parser


def _cmd_canvas(events: Path, poll_ms: int) -> int:
    try:
        from .window import run_canvas
    except ModuleNotFoundError as exc:
        print(
            "PySide6 is required for 'tabula canvas'. Install with: python -m pip install -e .[gui]",
            file=sys.stderr,
        )
        return 2

    return run_canvas(events, poll_interval_ms=poll_ms)


def _cmd_check_events(events: Path) -> int:
    if not events.exists():
        print(f"event file does not exist: {events}", file=sys.stderr)
        return 1

    errors: list[str] = []
    for line_no, raw in enumerate(events.read_text(encoding="utf-8").splitlines(), start=1):
        if not raw.strip():
            continue
        try:
            parse_event_line(raw, line_no=line_no, base_dir=events.parent)
        except EventValidationError as exc:
            errors.append(str(exc))

    if errors:
        print("event validation failed:", file=sys.stderr)
        for err in errors:
            print(f"- {err}", file=sys.stderr)
        return 1

    print("event validation passed")
    return 0


def _cmd_schema() -> int:
    print(json.dumps(event_schema(), indent=2, sort_keys=True))
    return 0


def _cmd_bootstrap(project_dir: Path) -> int:
    try:
        result = bootstrap_project(project_dir)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    print(f"project prepared: {result.paths.project_dir}")
    print(f"agents protocol: {result.paths.agents_path}")
    if result.git_initialized:
        print("git initialized")
    return 0


def _cmd_tabula(
    project_dir: Path,
    mode: str,
    prompt: str,
    headless: bool,
    no_canvas: bool,
    poll_ms: int,
) -> int:
    try:
        result = run_tabula_session(
            user_prompt=prompt,
            project_dir=project_dir,
            mode=cast(CodexMode, mode),
            headless=headless,
            start_canvas=not no_canvas,
            poll_interval_ms=poll_ms,
        )
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    if result.returncode != 0:
        print(result.message, file=sys.stderr)
    else:
        print(result.message)
    return result.returncode


def main(argv: list[str] | None = None) -> int:
    raw_argv = list(sys.argv[1:] if argv is None else argv)
    command_names = {"canvas", "check-events", "schema", "bootstrap"}

    if raw_argv and raw_argv[0] in command_names:
        parser = _build_command_parser()
        args = parser.parse_args(raw_argv)
        if args.command == "canvas":
            return _cmd_canvas(args.events, args.poll_ms)
        if args.command == "check-events":
            return _cmd_check_events(args.events)
        if args.command == "schema":
            return _cmd_schema()
        if args.command == "bootstrap":
            return _cmd_bootstrap(args.project_dir)
        parser.error("unknown command")
        return 2

    parser = _build_mainflow_parser()
    args = parser.parse_args(raw_argv)
    prompt_text = (args.prompt or " ".join(args.prompt_parts)).strip()
    if not prompt_text:
        try:
            prompt_text = input("tabula prompt> ").strip()
        except EOFError:
            prompt_text = ""
    if not prompt_text:
        print("prompt is required", file=sys.stderr)
        return 2

    return _cmd_tabula(
        args.project_dir,
        args.mode,
        prompt_text,
        args.headless,
        args.no_canvas,
        args.poll_ms,
    )


if __name__ == "__main__":
    raise SystemExit(main())
