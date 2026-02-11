from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

import tabula.workflow as workflow
from tabula.runner import RunResult
from tabula.workflow import build_codex_command, run_tabula_session


@dataclass
class FakeRunner:
    fail_codex: bool = False

    def __post_init__(self) -> None:
        self.calls: list[list[str]] = []

    def run(self, argv: list[str], *, cwd: Path | None = None, capture: bool = False) -> RunResult:
        self.calls.append(argv)
        if argv[:2] == ["git", "init"]:
            return RunResult(returncode=0, stdout="initialized\n")
        return RunResult(returncode=0)

    def run_interactive(self, argv: list[str], *, cwd: Path | None = None) -> RunResult:
        self.calls.append(argv)
        if self.fail_codex:
            return RunResult(returncode=1, stderr="failed\n")
        return RunResult(returncode=0)


def test_given_project_mode_when_running_tabula_then_codex_invoked_once_without_global_flag(
    tmp_path: Path, monkeypatch
) -> None:
    runner = FakeRunner()
    launched: dict[str, object] = {}

    def fake_launch(project_dir: Path, events_path: Path, *, poll_interval_ms: int = 250):
        launched["project_dir"] = project_dir
        launched["events_path"] = events_path
        launched["poll_ms"] = poll_interval_ms
        return None

    monkeypatch.setattr(workflow, "launch_canvas_background", fake_launch)
    result = run_tabula_session(
        user_prompt="Write a summary",
        project_dir=tmp_path,
        mode="project",
        env={"DISPLAY": ":0"},
        runner=runner,
    )

    assert result.returncode == 0
    assert result.message == "tabula completed (canvas)"
    codex_calls = [call for call in runner.calls if call and call[0] == "codex"]
    assert len(codex_calls) == 1
    assert "--skip-git-repo-check" not in codex_calls[0]
    assert launched["project_dir"] == tmp_path.resolve()
    assert launched["poll_ms"] == 250


def test_given_global_mode_when_running_tabula_then_codex_command_has_skip_repo_check(tmp_path: Path) -> None:
    runner = FakeRunner()
    result = run_tabula_session(
        user_prompt="Write a summary",
        project_dir=tmp_path,
        mode="global",
        headless=True,
        runner=runner,
    )

    assert result.returncode == 0
    codex_calls = [call for call in runner.calls if call and call[0] == "codex"]
    assert len(codex_calls) == 1
    assert "--skip-git-repo-check" in codex_calls[0]


def test_given_no_display_when_running_tabula_then_session_auto_headless_and_no_canvas_launch(
    tmp_path: Path, monkeypatch
) -> None:
    runner = FakeRunner()
    called = {"value": False}

    def fake_launch(project_dir: Path, events_path: Path, *, poll_interval_ms: int = 250):
        called["value"] = True
        return None

    monkeypatch.setattr(workflow, "launch_canvas_background", fake_launch)
    result = run_tabula_session(
        user_prompt="Write a summary",
        project_dir=tmp_path,
        mode="project",
        env={},
        runner=runner,
    )

    assert result.returncode == 0
    assert "headless" in result.message
    assert called["value"] is False


def test_given_injection_text_when_running_tabula_then_prompt_contains_injection(tmp_path: Path) -> None:
    runner = FakeRunner()
    injection = tmp_path / ".tabula" / "prompt-injection.txt"
    injection.parent.mkdir(parents=True, exist_ok=True)
    injection.write_text("Always include a section called Notes.", encoding="utf-8")

    result = run_tabula_session(
        user_prompt="Write a summary",
        project_dir=tmp_path,
        mode="project",
        headless=True,
        runner=runner,
    )

    assert result.returncode == 0
    codex_calls = [call for call in runner.calls if call and call[0] == "codex"]
    assert len(codex_calls) == 1
    assert "Always include a section called Notes." in codex_calls[0][-1]


def test_given_codex_failure_when_running_tabula_then_failure_is_returned(tmp_path: Path) -> None:
    runner = FakeRunner(fail_codex=True)
    result = run_tabula_session(
        user_prompt="Write a summary",
        project_dir=tmp_path,
        mode="project",
        headless=True,
        runner=runner,
    )

    assert result.returncode == 1
    assert "codex session failed" in result.message


def test_build_codex_command_supports_project_and_global_modes(tmp_path: Path) -> None:
    project_cmd = build_codex_command("hello", project_dir=tmp_path, mode="project")
    global_cmd = build_codex_command("hello", project_dir=tmp_path, mode="global")

    assert project_cmd[:3] == ["codex", "-C", str(tmp_path)]
    assert "--skip-git-repo-check" not in project_cmd
    assert global_cmd[:3] == ["codex", "-C", str(tmp_path)]
    assert "--skip-git-repo-check" in global_cmd
