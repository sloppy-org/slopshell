from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from .runner import SubprocessRunner

TABULA_DIR = Path(".tabula")
ARTIFACT_DIR = TABULA_DIR / "artifacts"
EVENTS_PATH = TABULA_DIR / "canvas-events.jsonl"
INJECTION_PATH = TABULA_DIR / "prompt-injection.txt"

AGENTS_PROTOCOL_BEGIN = "<!-- TABULA_PROTOCOL:BEGIN -->"
AGENTS_PROTOCOL_END = "<!-- TABULA_PROTOCOL:END -->"

GITIGNORE_BINARY_PATTERNS = [
    ".tabula/artifacts/*.pdf",
    ".tabula/artifacts/*.png",
    ".tabula/artifacts/*.jpg",
    ".tabula/artifacts/*.jpeg",
    ".tabula/artifacts/*.gif",
]


@dataclass(frozen=True)
class ProjectPaths:
    project_dir: Path
    artifacts_dir: Path
    events_path: Path
    injection_path: Path
    agents_path: Path


@dataclass(frozen=True)
class BootstrapResult:
    paths: ProjectPaths
    git_initialized: bool


def _protocol_block(artifacts_rel: Path, events_rel: Path, injection_rel: Path) -> str:
    lines = [
        AGENTS_PROTOCOL_BEGIN,
        "## Tabula Codex Protocol",
        "",
        "Use this protocol for Tabula interactive sessions in this project.",
        "",
        f"1. Read extra instructions from `{injection_rel.as_posix()}` and apply them.",
        f"2. Keep generated artifacts under `{artifacts_rel.as_posix()}` unless user explicitly overrides.",
        f"3. Emit canvas artifact events as strict JSONL lines in `{events_rel.as_posix()}`.",
        "4. Keep interaction terminal-first; do not replace the terminal with a custom REPL.",
        "5. Do not commit binary artifacts from `.tabula/artifacts/*` unless explicitly requested.",
        "",
        AGENTS_PROTOCOL_END,
        "",
    ]
    return "\n".join(lines)


def _upsert_protocol_block(existing: str, block: str) -> str:
    if AGENTS_PROTOCOL_BEGIN in existing and AGENTS_PROTOCOL_END in existing:
        prefix, remainder = existing.split(AGENTS_PROTOCOL_BEGIN, 1)
        _, suffix = remainder.split(AGENTS_PROTOCOL_END, 1)
        merged = prefix.rstrip() + "\n\n" + block + suffix.lstrip("\n")
        return merged
    if not existing.strip():
        return block
    return existing.rstrip() + "\n\n" + block


def _ensure_gitignore(project_dir: Path) -> None:
    path = project_dir / ".gitignore"
    existing_lines: list[str]
    if path.exists():
        existing_lines = path.read_text(encoding="utf-8").splitlines()
    else:
        existing_lines = []

    seen = set(existing_lines)
    appended = [pattern for pattern in GITIGNORE_BINARY_PATTERNS if pattern not in seen]
    if not appended:
        return

    if existing_lines and existing_lines[-1].strip():
        existing_lines.append("")
    existing_lines.extend(appended)
    path.write_text("\n".join(existing_lines) + "\n", encoding="utf-8")


def _ensure_git_repo(project_dir: Path, runner: SubprocessRunner) -> bool:
    if (project_dir / ".git").exists():
        return False
    result = runner.run(["git", "init"], cwd=project_dir, capture=True)
    if result.returncode != 0:
        message = result.stderr.strip() or result.stdout.strip() or "git init failed"
        raise RuntimeError(message)
    return True


def bootstrap_project(
    project_dir: Path,
    *,
    artifacts_rel: Path = ARTIFACT_DIR,
    events_rel: Path = EVENTS_PATH,
    injection_rel: Path = INJECTION_PATH,
    runner: SubprocessRunner | None = None,
) -> BootstrapResult:
    command_runner = runner or SubprocessRunner()
    project_dir = project_dir.resolve()
    project_dir.mkdir(parents=True, exist_ok=True)

    artifacts_dir = (project_dir / artifacts_rel).resolve()
    events_path = (project_dir / events_rel).resolve()
    injection_path = (project_dir / injection_rel).resolve()
    agents_path = (project_dir / "AGENTS.md").resolve()

    artifacts_dir.mkdir(parents=True, exist_ok=True)
    events_path.parent.mkdir(parents=True, exist_ok=True)
    injection_path.parent.mkdir(parents=True, exist_ok=True)

    if not events_path.exists():
        events_path.touch()
    if not injection_path.exists():
        injection_path.write_text(
            "Apply these extra instructions in all Tabula Codex prompts for this project.\n",
            encoding="utf-8",
        )

    _ensure_gitignore(project_dir)

    block = _protocol_block(artifacts_rel, events_rel, injection_rel)
    if agents_path.exists():
        existing = agents_path.read_text(encoding="utf-8")
    else:
        existing = "# AGENTS\n\n"
    agents_path.write_text(_upsert_protocol_block(existing, block), encoding="utf-8")

    git_initialized = _ensure_git_repo(project_dir, command_runner)
    return BootstrapResult(
        paths=ProjectPaths(
            project_dir=project_dir,
            artifacts_dir=artifacts_dir,
            events_path=events_path,
            injection_path=injection_path,
            agents_path=agents_path,
        ),
        git_initialized=git_initialized,
    )


def load_injection_text(injection_path: Path) -> str:
    if not injection_path.exists():
        return ""
    return injection_path.read_text(encoding="utf-8").strip()
