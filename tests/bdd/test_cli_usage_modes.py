from __future__ import annotations

import builtins
import json
import runpy
import sys
import types
import warnings
from dataclasses import dataclass
from pathlib import Path

import pytest

from tabula.cli import main


def test_given_schema_mode_when_invoked_then_prints_contract(capsys) -> None:
    rc = main(["schema"])
    out = capsys.readouterr().out
    schema = json.loads(out)

    assert rc == 0
    assert schema["title"] == "TabulaCanvasEvent"
    assert len(schema["oneOf"]) == 4


def test_given_canvas_mode_when_invoked_then_calls_ui_runner(monkeypatch) -> None:
    seen: dict[str, object] = {}

    def fake_run_canvas(*, poll_interval_ms: int) -> int:
        seen["poll_ms"] = poll_interval_ms
        return 7

    monkeypatch.setitem(sys.modules, "tabula.window", types.SimpleNamespace(run_canvas=fake_run_canvas))

    rc = main(["canvas", "--poll-ms", "999"])
    assert rc == 7
    assert seen["poll_ms"] == 999


def test_given_canvas_mode_without_window_dependency_then_shows_install_hint(monkeypatch, capsys) -> None:
    original_import = builtins.__import__

    def fake_import(name, globals=None, locals=None, fromlist=(), level=0):
        if name == "tabula.window":
            raise ModuleNotFoundError("No module named 'PySide6'")
        return original_import(name, globals, locals, fromlist, level)

    monkeypatch.delitem(sys.modules, "tabula.window", raising=False)
    monkeypatch.setattr(builtins, "__import__", fake_import)

    rc = main(["canvas"])
    err = capsys.readouterr().err

    assert rc == 2
    assert "PySide6 is required for 'tabula canvas'" in err


def test_given_bootstrap_mode_when_invoked_then_project_is_prepared(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir,
                agents_path=project_dir / "AGENTS.md",
                mcp_config_path=project_dir / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=True,
        )

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["bootstrap", "--project-dir", str(tmp_path)])
    out = capsys.readouterr().out

    assert rc == 0
    assert "project prepared:" in out
    assert "mcp config snippet:" in out
    assert "git initialized" in out


def test_given_bootstrap_runtime_failure_when_invoked_then_error_and_nonzero(monkeypatch, tmp_path: Path, capsys) -> None:
    def fake_bootstrap(_project_dir: Path):
        raise RuntimeError("bootstrap failed hard")

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)

    rc = main(["bootstrap", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 1
    assert "bootstrap failed hard" in err


def test_given_mcp_server_mode_when_invoked_then_bootstrap_and_server_runner_are_called(monkeypatch, tmp_path: Path) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool

    calls: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir.resolve(),
                agents_path=(project_dir / "AGENTS.md").resolve(),
                mcp_config_path=(project_dir / ".tabula" / "codex-mcp.toml").resolve(),
            ),
            git_initialized=False,
        )

    def fake_run_server(*, project_dir: Path, headless: bool, poll_interval_ms: int, start_canvas: bool) -> int:
        calls["project_dir"] = project_dir
        calls["headless"] = headless
        calls["poll"] = poll_interval_ms
        calls["start_canvas"] = start_canvas
        return 11

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.run_mcp_stdio_server", fake_run_server)

    rc = main(["mcp-server", "--project-dir", str(tmp_path), "--headless", "--no-canvas", "--poll-ms", "777"])
    assert rc == 11
    assert calls["project_dir"] == tmp_path.resolve()
    assert calls["headless"] is True
    assert calls["poll"] == 777
    assert calls["start_canvas"] is False


def test_given_mcp_server_bootstrap_failure_when_invoked_then_nonzero(monkeypatch, tmp_path: Path, capsys) -> None:
    def fake_bootstrap(_project_dir: Path):
        raise RuntimeError("mcp bootstrap failed")

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["mcp-server", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 1
    assert "mcp bootstrap failed" in err


def test_given_no_args_when_invoked_then_help_and_exit_2(capsys) -> None:
    rc = main([])
    err = capsys.readouterr().err
    assert rc == 2
    assert "usage: tabula" in err


def test_given_unknown_command_shape_when_main_dispatches_then_parser_error_branch_returns_2(monkeypatch) -> None:
    class FakeParser:
        def __init__(self) -> None:
            self.errors: list[str] = []

        def print_help(self, _stream) -> None:
            return None

        def parse_args(self, _argv):
            return type("Args", (), {"command": "unexpected"})()

        def error(self, message: str) -> None:
            self.errors.append(message)

    fake_parser = FakeParser()
    monkeypatch.setattr("tabula.cli._build_parser", lambda: fake_parser)

    rc = main(["unexpected"])
    assert rc == 2
    assert fake_parser.errors == ["unknown command"]


def test_given_cli_module_executed_as_main_when_schema_arg_then_main_guard_exits_zero(monkeypatch, capsys) -> None:
    monkeypatch.setattr(sys, "argv", ["tabula", "schema"])
    with warnings.catch_warnings():
        warnings.filterwarnings("ignore", category=RuntimeWarning, message=".*tabula\\.cli.*")
        with pytest.raises(SystemExit) as exc:
            runpy.run_module("tabula.cli", run_name="__main__")

    assert exc.value.code == 0
    out = capsys.readouterr().out
    assert '"title": "TabulaCanvasEvent"' in out
