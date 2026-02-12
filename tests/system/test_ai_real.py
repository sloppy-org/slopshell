from __future__ import annotations

import asyncio
import json
import os
import shutil
from pathlib import Path

import pytest

from .conftest import make_serve_client

_SKIP_NO_GATE = "set TABULA_RUN_REAL_AI=1 to run real AI system tests"


@pytest.mark.skipif(shutil.which("claude") is None, reason="claude CLI not on PATH")
def test_real_claude_http_mcp(tmp_path: Path) -> None:
    if os.environ.get("TABULA_RUN_REAL_AI") != "1":
        pytest.skip(_SKIP_NO_GATE)

    async def _run() -> None:
        tc = await make_serve_client(tmp_path)
        async with tc:
            port = tc.port
            mcp_url = f"http://127.0.0.1:{port}/mcp"
            ws = await tc.ws_connect("/ws/canvas")

            cfg = {"mcpServers": {"tabula-canvas": {"url": mcp_url}}}
            prompt = (
                "Call MCP tool canvas_render_text with session_id='real-claude-sys', "
                "title='real-test', markdown_or_text='hello from real claude'. "
                "Then immediately exit with no further output."
            )
            proc = await asyncio.create_subprocess_exec(
                "claude", "--mcp-config", json.dumps(cfg, separators=(",", ":")),
                "-p", prompt,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=str(tmp_path),
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=120.0)

            msg = await asyncio.wait_for(ws.receive_str(), timeout=5.0)
            payload = json.loads(msg)
            assert payload["kind"] == "text_artifact"
            assert "hello from real claude" in payload["text"]

            await ws.close()

    asyncio.run(_run())


@pytest.mark.skipif(shutil.which("codex") is None, reason="codex CLI not on PATH")
def test_real_codex_http_mcp(tmp_path: Path) -> None:
    if os.environ.get("TABULA_RUN_REAL_AI") != "1":
        pytest.skip(_SKIP_NO_GATE)

    async def _run() -> None:
        tc = await make_serve_client(tmp_path)
        async with tc:
            port = tc.port
            mcp_url = f"http://127.0.0.1:{port}/mcp"

            proc = await asyncio.create_subprocess_exec(
                "codex", "--no-alt-screen", "--yolo",
                "-C", str(tmp_path),
                "-c", f"mcp_servers.tabula-canvas.url={json.dumps(mcp_url)}",
                "Call canvas_render_text with session_id='real-codex-sys', "
                "title='codex-test', markdown_or_text='hello from real codex'. "
                "Then exit.",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=120.0)
            assert proc.returncode == 0, f"stderr: {stderr.decode()}"

    asyncio.run(_run())
