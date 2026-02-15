from __future__ import annotations

from pathlib import Path

from tabula.protocol import AGENTS_PROTOCOL_BEGIN, AGENTS_PROTOCOL_END, bootstrap_project


def test_given_new_project_when_bootstrapped_then_git_agents_mcp_and_binary_ignores_are_created(tmp_path: Path) -> None:
    result = bootstrap_project(tmp_path)

    assert result.git_initialized is True
    assert result.agents_preserved is False
    assert (tmp_path / ".git").exists()
    assert (tmp_path / ".gitignore").exists()
    assert (tmp_path / ".tabula" / "artifacts").exists()
    assert (tmp_path / ".tabula" / "prompt-injection.txt").exists()
    assert (tmp_path / ".tabula" / "codex-mcp.toml").exists()
    assert (tmp_path / ".tabula" / "AGENTS.tabula.md").exists()
    assert (tmp_path / "AGENTS.md").exists()

    agents = (tmp_path / "AGENTS.md").read_text(encoding="utf-8")
    assert AGENTS_PROTOCOL_BEGIN in agents
    assert AGENTS_PROTOCOL_END in agents
    assert ".tabula/artifacts" in agents
    assert "do not rely on filesystem event logs" in agents
    assert "canvas_session_open" in agents
    assert "tabula-canvas" in agents

    mcp_cfg = (tmp_path / ".tabula" / "codex-mcp.toml").read_text(encoding="utf-8")
    assert "[mcp_servers.tabula-canvas]" in mcp_cfg
    assert 'command = "tabula"' in mcp_cfg
    assert '--project-dir' in mcp_cfg

    gitignore = (tmp_path / ".gitignore").read_text(encoding="utf-8")
    assert ".tabula/artifacts/" in gitignore


def test_given_existing_agents_when_bootstrapped_then_existing_agents_is_preserved_and_sidecar_is_written(
    tmp_path: Path,
) -> None:
    custom = "# AGENTS\n\nCustom section\n\n"
    (tmp_path / "AGENTS.md").write_text(custom, encoding="utf-8")

    result = bootstrap_project(tmp_path)
    assert result.agents_preserved is True
    content = (tmp_path / "AGENTS.md").read_text(encoding="utf-8")
    sidecar = (tmp_path / ".tabula" / "AGENTS.tabula.md").read_text(encoding="utf-8")

    assert content == custom
    assert AGENTS_PROTOCOL_BEGIN in sidecar
    assert AGENTS_PROTOCOL_END in sidecar


def test_given_existing_git_repo_when_bootstrapped_then_does_not_reinitialize(tmp_path: Path) -> None:
    (tmp_path / ".git").mkdir()
    result = bootstrap_project(tmp_path)
    assert result.git_initialized is False
