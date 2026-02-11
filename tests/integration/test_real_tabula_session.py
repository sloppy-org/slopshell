from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path


def test_real_tabula_headless_with_fake_codex(tmp_path: Path) -> None:
    proj = tmp_path / "project"
    proj.mkdir()

    fake_bin = tmp_path / "bin"
    fake_bin.mkdir()
    codex = fake_bin / "codex"
    codex.write_text(
        "\n".join(
            [
                "#!/usr/bin/env bash",
                "set -euo pipefail",
                "proj=''",
                "args=(\"$@\")",
                "for ((i=0;i<${#args[@]};i++)); do",
                "  if [[ \"${args[$i]}\" == \"-C\" ]]; then proj=\"${args[$((i+1))]}\"; fi",
                "done",
                "if [[ -z \"$proj\" ]]; then proj=\"$PWD\"; fi",
                "out=\"$proj/.tabula/artifacts/fake-output.txt\"",
                "mkdir -p \"$(dirname \"$out\")\"",
                "printf 'fake codex ran\\n' > \"$out\"",
                "exit 0",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    codex.chmod(0o755)

    env = os.environ.copy()
    env["PATH"] = f"{fake_bin}:{env['PATH']}"

    cmd = [
        "script",
        "-qefc",
        (
            f"env PATH={fake_bin}:{env['PATH']} PYTHONPATH=src "
            f"{sys.executable} -m tabula --project-dir {proj} --headless --prompt 'Do any short task'"
        ),
        "/dev/null",
    ]
    proc = subprocess.run(cmd, cwd=Path.cwd(), text=True, capture_output=True)
    assert proc.returncode == 0, proc.stderr
    assert "tabula completed (headless)" in proc.stdout

    output = proj / ".tabula" / "artifacts" / "fake-output.txt"
    events = proj / ".tabula" / "canvas-events.jsonl"
    agents = proj / "AGENTS.md"
    assert output.exists()
    assert events.exists()
    assert agents.exists()
