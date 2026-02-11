from __future__ import annotations

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

    monkeypatch.setattr("tabula.cli.has_display", lambda _env=None: True)
    monkeypatch.setitem(sys.modules, "tabula.window", types.SimpleNamespace(run_canvas=fake_run_canvas))

    rc = main(["canvas", "--poll-ms", "999"])
    assert rc == 7
    assert seen["poll_ms"] == 999


def test_given_canvas_mode_when_ui_runner_raises_then_error_is_reported(monkeypatch, capsys) -> None:
    monkeypatch.setattr("tabula.cli.has_display", lambda _env=None: True)
    def fake_run_canvas(*, poll_interval_ms: int) -> int:
        raise RuntimeError(f"boom-{poll_interval_ms}")

    monkeypatch.setitem(sys.modules, "tabula.window", types.SimpleNamespace(run_canvas=fake_run_canvas))

    rc = main(["canvas", "--poll-ms", "55"])
    err = capsys.readouterr().err

    assert rc == 2
    assert "failed to start canvas window: boom-55" in err


def test_given_canvas_mode_without_display_then_shows_headless_hint(monkeypatch, capsys) -> None:
    monkeypatch.setattr("tabula.cli.has_display", lambda _env=None: False)
    rc = main(["canvas"])
    err = capsys.readouterr().err

    assert rc == 2
    assert "DISPLAY/WAYLAND_DISPLAY not found" in err


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
        agents_preserved: bool

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir,
                agents_path=project_dir / "AGENTS.md",
                mcp_config_path=project_dir / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=True,
            agents_preserved=False,
        )

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["bootstrap", "--project-dir", str(tmp_path)])
    out = capsys.readouterr().out

    assert rc == 0
    assert "project prepared:" in out
    assert "tabula sidecar protocol:" in out
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


def test_given_bootstrap_with_existing_agents_when_invoked_then_preserved_message_printed(
    monkeypatch, tmp_path: Path, capsys
) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir,
                agents_path=project_dir / "AGENTS.md",
                mcp_config_path=project_dir / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=True,
        )

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["bootstrap", "--project-dir", str(tmp_path)])
    out = capsys.readouterr().out

    assert rc == 0
    assert "existing AGENTS.md is preserved; tabula protocol is in sidecar" in out


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
        agents_preserved: bool

    calls: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir.resolve(),
                agents_path=(project_dir / "AGENTS.md").resolve(),
                mcp_config_path=(project_dir / ".tabula" / "codex-mcp.toml").resolve(),
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    def fake_run_server(
        *,
        project_dir: Path,
        headless: bool,
        fresh_canvas: bool,
        poll_interval_ms: int,
        start_canvas: bool,
    ) -> int:
        calls["project_dir"] = project_dir
        calls["headless"] = headless
        calls["fresh_canvas"] = fresh_canvas
        calls["poll"] = poll_interval_ms
        calls["start_canvas"] = start_canvas
        return 11

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.run_mcp_stdio_server", fake_run_server)

    rc = main(["mcp-server", "--project-dir", str(tmp_path), "--headless", "--no-canvas", "--poll-ms", "777"])
    assert rc == 11
    assert calls["project_dir"] == tmp_path.resolve()
    assert calls["headless"] is True
    assert calls["fresh_canvas"] is False
    assert calls["poll"] == 777
    assert calls["start_canvas"] is False


def test_given_mcp_server_with_fresh_canvas_flag_when_invoked_then_runner_receives_fresh_canvas(
    monkeypatch, tmp_path: Path
) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    calls: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        return _Result(
            paths=_Paths(
                project_dir=project_dir.resolve(),
                agents_path=(project_dir / "AGENTS.md").resolve(),
                mcp_config_path=(project_dir / ".tabula" / "codex-mcp.toml").resolve(),
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    def fake_run_server(
        *,
        project_dir: Path,
        headless: bool,
        fresh_canvas: bool,
        poll_interval_ms: int,
        start_canvas: bool,
    ) -> int:
        calls["project_dir"] = project_dir
        calls["headless"] = headless
        calls["fresh_canvas"] = fresh_canvas
        calls["poll"] = poll_interval_ms
        calls["start_canvas"] = start_canvas
        return 17

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.run_mcp_stdio_server", fake_run_server)

    rc = main(["mcp-server", "--project-dir", str(tmp_path), "--fresh-canvas"])
    assert rc == 17
    assert calls["fresh_canvas"] is True
    assert calls["headless"] is False
    assert calls["start_canvas"] is True


def test_given_mcp_server_bootstrap_failure_when_invoked_then_nonzero(monkeypatch, tmp_path: Path, capsys) -> None:
    def fake_bootstrap(_project_dir: Path):
        raise RuntimeError("mcp bootstrap failed")

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    rc = main(["mcp-server", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 1
    assert "mcp bootstrap failed" in err


def test_given_run_mode_when_invoked_then_codex_launches_with_inline_mcp_yolo_and_search(monkeypatch, tmp_path: Path) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    seen: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    class _RunResult:
        returncode = 19

    def fake_run(cmd):
        seen["cmd"] = cmd
        return _RunResult()

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", fake_run)

    rc = main(
        [
            "run",
            "--project-dir",
            str(tmp_path),
            "--headless",
            "--no-canvas",
            "--poll-ms",
            "777",
            "hello from tabula run",
        ]
    )

    assert rc == 19
    cmd = seen["cmd"]
    assert isinstance(cmd, list)
    assert cmd[0] == "codex"
    assert "--yolo" in cmd
    assert "--search" in cmd
    assert "--no-alt-screen" in cmd
    assert "hello from tabula run" in cmd

    # Inline MCP server config overrides must be present.
    command_override = cmd[cmd.index("-c") + 1]
    assert "mcp_servers.tabula-canvas.command" in command_override
    assert "bash" in command_override
    args_override = cmd[cmd.index("-c", cmd.index("-c") + 1) + 1]
    assert "mcp_servers.tabula-canvas.args=" in args_override
    assert "-lc" in args_override
    assert "python" in args_override
    assert "--headless" in args_override
    assert "--no-canvas" in args_override
    assert "--fresh-canvas" in args_override
    assert "777" in args_override


def test_given_run_mode_when_codex_missing_then_nonzero_and_hint(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    def fake_run(_cmd):
        raise FileNotFoundError("codex")

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", fake_run)

    rc = main(["run", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 1
    assert "codex CLI not found on PATH" in err


def test_given_run_mode_with_claude_assistant_when_invoked_then_claude_launches_with_inline_mcp(
    monkeypatch, tmp_path: Path
) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    seen: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    class _RunResult:
        returncode = 23

    def fake_run(cmd, cwd=None):
        seen["cmd"] = cmd
        seen["cwd"] = cwd
        return _RunResult()

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", fake_run)

    rc = main(
        [
            "run",
            "--assistant",
            "claude",
            "--project-dir",
            str(tmp_path),
            "--headless",
            "--no-canvas",
            "--poll-ms",
            "888",
            "hello from claude tabula run",
        ]
    )

    assert rc == 23
    cmd = seen["cmd"]
    assert isinstance(cmd, list)
    assert cmd[0] == "claude"
    assert "--mcp-config" in cmd
    assert "hello from claude tabula run" in cmd
    cfg = json.loads(cmd[cmd.index("--mcp-config") + 1])
    server = cfg["mcpServers"]["tabula-canvas"]
    assert server["command"] == "bash"
    assert server["args"][0] == "-lc"
    assert "--headless" in server["args"][1]
    assert "--no-canvas" in server["args"][1]
    assert "--fresh-canvas" in server["args"][1]
    assert "888" in server["args"][1]
    assert seen["cwd"] == tmp_path.resolve()


def test_given_run_mode_with_claude_assistant_when_claude_missing_then_nonzero_and_hint(
    monkeypatch, tmp_path: Path, capsys
) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    def fake_run(_cmd, cwd=None):
        raise FileNotFoundError("claude")

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", fake_run)

    rc = main(["run", "--assistant", "claude", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 1
    assert "claude CLI not found on PATH" in err


def test_given_run_mode_without_display_when_canvas_expected_then_headless_warning(monkeypatch, tmp_path: Path, capsys) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    class _RunResult:
        returncode = 0

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", lambda _cmd: _RunResult())
    monkeypatch.delenv("DISPLAY", raising=False)
    monkeypatch.delenv("WAYLAND_DISPLAY", raising=False)

    rc = main(["run", "--project-dir", str(tmp_path)])
    err = capsys.readouterr().err
    assert rc == 0
    assert "tabula-canvas will run headless" in err


def test_given_run_mode_with_display_env_when_invoked_then_display_vars_are_forwarded(monkeypatch, tmp_path: Path) -> None:
    @dataclass(frozen=True)
    class _Paths:
        project_dir: Path
        agents_path: Path
        mcp_config_path: Path

    @dataclass(frozen=True)
    class _Result:
        paths: _Paths
        git_initialized: bool
        agents_preserved: bool

    seen: dict[str, object] = {}

    def fake_bootstrap(project_dir: Path):
        resolved = project_dir.resolve()
        return _Result(
            paths=_Paths(
                project_dir=resolved,
                agents_path=resolved / "AGENTS.md",
                mcp_config_path=resolved / ".tabula" / "codex-mcp.toml",
            ),
            git_initialized=False,
            agents_preserved=False,
        )

    class _RunResult:
        returncode = 0

    def fake_run(cmd):
        seen["cmd"] = cmd
        return _RunResult()

    monkeypatch.setattr("tabula.cli.bootstrap_project", fake_bootstrap)
    monkeypatch.setattr("tabula.cli.subprocess.run", fake_run)
    monkeypatch.setenv("DISPLAY", ":0")
    monkeypatch.setenv("XAUTHORITY", "/tmp/xauth")

    rc = main(["run", "--project-dir", str(tmp_path)])
    assert rc == 0
    cmd = seen["cmd"]
    assert isinstance(cmd, list)
    args_override = cmd[cmd.index("-c", cmd.index("-c") + 1) + 1]
    assert "DISPLAY=" in args_override
    assert "XAUTHORITY=" in args_override


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
