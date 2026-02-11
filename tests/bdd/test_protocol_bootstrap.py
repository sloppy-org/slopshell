from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from tabula.protocol import AGENTS_PROTOCOL_BEGIN, AGENTS_PROTOCOL_END, bootstrap_project
from tabula.runner import RunResult


@dataclass
class FakeRunner:
    def __post_init__(self) -> None:
        self.calls: list[list[str]] = []

    def run(self, argv: list[str], *, cwd: Path | None = None, capture: bool = False) -> RunResult:
        self.calls.append(argv)
        if argv[:2] == ["git", "init"]:
            return RunResult(returncode=0, stdout="initialized\n")
        return RunResult(returncode=0)


def test_given_new_project_when_bootstrapped_then_git_agents_and_binary_ignores_are_created(tmp_path: Path) -> None:
    runner = FakeRunner()
    result = bootstrap_project(tmp_path, runner=runner)

    assert result.git_initialized is True
    assert (tmp_path / ".gitignore").exists()
    assert (tmp_path / ".tabula" / "artifacts").exists()
    assert (tmp_path / ".tabula" / "prompt-injection.txt").exists()
    assert (tmp_path / ".tabula" / "canvas-events.jsonl").exists()
    assert (tmp_path / "AGENTS.md").exists()

    agents = (tmp_path / "AGENTS.md").read_text(encoding="utf-8")
    assert AGENTS_PROTOCOL_BEGIN in agents
    assert AGENTS_PROTOCOL_END in agents
    assert ".tabula/artifacts" in agents
    assert ".tabula/canvas-events.jsonl" in agents

    gitignore = (tmp_path / ".gitignore").read_text(encoding="utf-8")
    assert ".tabula/artifacts/*.pdf" in gitignore
    assert ".tabula/artifacts/*.png" in gitignore


def test_given_existing_agents_when_bootstrapped_then_protocol_block_is_upserted_without_losing_custom_text(
    tmp_path: Path,
) -> None:
    custom = "# AGENTS\n\nCustom section\n\n"
    (tmp_path / "AGENTS.md").write_text(custom, encoding="utf-8")
    runner = FakeRunner()

    bootstrap_project(tmp_path, runner=runner)
    content = (tmp_path / "AGENTS.md").read_text(encoding="utf-8")

    assert "Custom section" in content
    assert content.count(AGENTS_PROTOCOL_BEGIN) == 1
    assert content.count(AGENTS_PROTOCOL_END) == 1
