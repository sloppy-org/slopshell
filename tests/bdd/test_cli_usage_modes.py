from __future__ import annotations

import builtins
import json
import sys
import types
from dataclasses import dataclass
from pathlib import Path

from tabula.cli import main


def _write_jsonl(path: Path, payloads: list[dict[str, object]]) -> None:
    lines = [json.dumps(item) for item in payloads]
    path.write_text("\n".join(lines), encoding="utf-8")


def test_given_schema_mode_when_invoked_then_prints_contract(capsys) -> None:
    rc = main(["schema"])
    out = capsys.readouterr().out
    schema = json.loads(out)

    assert rc == 0
    assert schema["title"] == "TabulaCanvasEvent"
    assert len(schema["oneOf"]) == 4


def test_given_canvas_mode_when_invoked_then_calls_ui_runner(monkeypatch, tmp_path: Path) -> None:
    events_path = tmp_path / "events.jsonl"
    seen: dict[str, object] = {}

    def fake_run_canvas(events: Path, *, poll_interval_ms: int) -> int:
        seen["events"] = events
        seen["poll_ms"] = poll_interval_ms
        return 7

    monkeypatch.setitem(sys.modules, "tabula.window", types.SimpleNamespace(run_canvas=fake_run_canvas))

    rc = main(["canvas", "--events", str(events_path), "--poll-ms", "999"])
    assert rc == 7
    assert seen["events"] == events_path
    assert seen["poll_ms"] == 999


def test_given_canvas_mode_without_window_dependency_then_shows_install_hint(monkeypatch, tmp_path: Path, capsys) -> None:
    events_path = tmp_path / "events.jsonl"
    original_import = builtins.__import__

    def fake_import(name, globals=None, locals=None, fromlist=(), level=0):
        if name == "tabula.window":
            raise ModuleNotFoundError("No module named 'PySide6'")
        return original_import(name, globals, locals, fromlist, level)

    monkeypatch.delitem(sys.modules, "tabula.window", raising=False)
    monkeypatch.setattr(builtins, "__import__", fake_import)

    rc = main(["canvas", "--events", str(events_path)])
    err = capsys.readouterr().err

    assert rc == 2
    assert "PySide6 is required for 'tabula canvas'" in err


def test_given_missing_event_file_when_checking_then_nonzero_exit(tmp_path: Path, capsys) -> None:
    missing = tmp_path / "nope.jsonl"
    rc = main(["check-events", "--events", str(missing)])
    err = capsys.readouterr().err

    assert rc == 1
    assert "event file does not exist" in err


def test_given_valid_event_file_when_checking_then_passes(tmp_path: Path, capsys) -> None:
    events = tmp_path / "events.jsonl"
    image = tmp_path / "x.png"
    image.write_bytes(b"x")

    _write_jsonl(
        events,
        [
            {
                "event_id": "e1",
                "ts": "2026-02-11T12:00:00Z",
                "kind": "text_artifact",
                "title": "draft",
                "text": "hello",
            },
            {
                "event_id": "e2",
                "ts": "2026-02-11T12:00:01Z",
                "kind": "image_artifact",
                "title": "img",
                "path": str(image),
            },
            {
                "event_id": "e3",
                "ts": "2026-02-11T12:00:02Z",
                "kind": "clear_canvas",
            },
        ],
    )

    rc = main(["check-events", "--events", str(events)])
    out = capsys.readouterr().out

    assert rc == 0
    assert "event validation passed" in out


def test_given_invalid_event_lines_when_checking_then_reports_all_errors(tmp_path: Path, capsys) -> None:
    events = tmp_path / "events.jsonl"
    events.write_text(
        "\n".join(
            [
                '{"event_id":"e1","ts":"bad","kind":"text_artifact","title":"x","text":"y"}',
                '{"event_id":"e2","ts":"2026-02-11T12:00:00Z","kind":"clear_canvas","reason":42}',
                "{broken",
            ]
        ),
        encoding="utf-8",
    )

    rc = main(["check-events", "--events", str(events)])
    err = capsys.readouterr().err

    assert rc == 1
    assert "event validation failed:" in err
    assert "line 1:" in err
    assert "line 2:" in err
    assert "line 3:" in err


def test_given_bootstrap_mode_when_invoked_then_project_is_prepared(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool

    def fake_bootstrap(project_dir: Path):
        return _Result(paths=_Paths(project_dir=project_dir, agents_path=project_dir / "AGENTS.md"), git_initialized=True)

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["bootstrap", "--project-dir", str(tmp_path)])
    out = capsys.readouterr().out

    assert rc == 0
    assert "project prepared:" in out
    assert "git initialized" in out


def test_given_default_tabula_mode_when_invoked_then_cli_returns_workflow_status(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _WorkflowResult:
        returncode: int
        message: str

    def fake_workflow(
        *,
        user_prompt: str,
        project_dir: Path,
        mode: str,
        headless: bool,
        start_canvas: bool,
        poll_interval_ms: int,
    ):
        assert user_prompt == "draft text"
        assert project_dir == tmp_path
        assert mode == "project"
        assert headless is True
        assert start_canvas is False
        assert poll_interval_ms == 333
        return _WorkflowResult(returncode=0, message="ok")

    monkeypatch.setattr("tabula.cli.run_tabula_session", fake_workflow)
    rc = main(
        [
            "--project-dir",
            str(tmp_path),
            "--mode",
            "project",
            "--prompt",
            "draft text",
            "--headless",
            "--no-canvas",
            "--poll-ms",
            "333",
        ]
    )
    out = capsys.readouterr().out

    assert rc == 0
    assert "ok" in out


def test_given_positional_prompt_when_default_tabula_mode_then_prompt_is_forwarded(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _WorkflowResult:
        returncode: int
        message: str

    seen: dict[str, str] = {}

    def fake_workflow(**kwargs):
        seen["prompt"] = kwargs["user_prompt"]
        return _WorkflowResult(returncode=0, message="ok")

    monkeypatch.setattr("tabula.cli.run_tabula_session", fake_workflow)
    rc = main(["--project-dir", str(tmp_path), "hello", "world"])
    assert rc == 0
    assert seen["prompt"] == "hello world"
    assert "ok" in capsys.readouterr().out
