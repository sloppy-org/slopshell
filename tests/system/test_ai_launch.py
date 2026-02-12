from __future__ import annotations

import asyncio
import json
import os
import shutil
import stat
import sys
import textwrap
from pathlib import Path

from aiohttp.test_utils import TestClient, TestServer

from tabula.serve import TabulaServeApp

from tabula.web.pty import LocalPtyTransport

from .conftest import make_serve_client, read_pty_until

MOCK_AI_SCRIPT = textwrap.dedent("""\
    import json
    import sys
    import urllib.request

    mcp_url = sys.argv[1]

    def rpc(method, params, msg_id=1):
        body = json.dumps({
            "jsonrpc": "2.0", "id": msg_id,
            "method": method, "params": params,
        }).encode()
        req = urllib.request.Request(
            mcp_url, data=body,
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read())

    result = rpc("initialize", {})
    assert result["result"]["serverInfo"]["name"] == "tabula-canvas"

    result = rpc("tools/call", {
        "name": "canvas_render_text",
        "arguments": {
            "session_id": "mock-ai",
            "title": "mock-artifact",
            "markdown_or_text": "hello from mock ai",
        },
    }, msg_id=2)
    assert result["result"]["isError"] is False

    result = rpc("tools/call", {
        "name": "canvas_status",
        "arguments": {"session_id": "mock-ai"},
    }, msg_id=3)
    assert result["result"]["isError"] is False

    print("MOCK_AI_SUCCESS")
""")


def _write_mock_script(tmp_path: Path, content: str, name: str = "mock_ai.py") -> Path:
    script = tmp_path / name
    script.write_text(content, encoding="utf-8")
    return script


def test_mock_ai_mcp_roundtrip(tmp_path: Path) -> None:
    script = _write_mock_script(tmp_path, MOCK_AI_SCRIPT)

    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            port = client.port
            url = f"http://127.0.0.1:{port}/mcp"
            proc = await asyncio.create_subprocess_exec(
                sys.executable, str(script), url,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=15.0)
            assert proc.returncode == 0, f"stderr: {stderr.decode()}"
            assert b"MOCK_AI_SUCCESS" in stdout

    asyncio.run(_run())


def test_mock_ai_canvas_event_on_ws(tmp_path: Path) -> None:
    script = _write_mock_script(tmp_path, MOCK_AI_SCRIPT)

    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            ws = await client.ws_connect("/ws/canvas")

            port = client.port
            url = f"http://127.0.0.1:{port}/mcp"
            proc = await asyncio.create_subprocess_exec(
                sys.executable, str(script), url,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            await asyncio.wait_for(proc.communicate(), timeout=15.0)

            msg = await asyncio.wait_for(ws.receive_str(), timeout=2.0)
            payload = json.loads(msg)
            assert payload["kind"] == "text_artifact"
            assert payload["text"] == "hello from mock ai"

            await ws.close()

    asyncio.run(_run())


def test_mock_ai_via_tabula_run(tmp_path: Path) -> None:
    serve_app = TabulaServeApp(project_dir=tmp_path)
    app = serve_app.create_app()

    mock_claude = tmp_path / "claude"
    mock_claude.write_text(textwrap.dedent(f"""\
        #!/usr/bin/env {sys.executable}
        import json
        import sys
        import urllib.request

        for i, arg in enumerate(sys.argv):
            if arg == "--mcp-config":
                cfg = json.loads(sys.argv[i + 1])
                mcp_url = cfg["mcpServers"]["tabula-canvas"]["url"]
                break
        else:
            print("no --mcp-config found", file=sys.stderr)
            sys.exit(1)

        body = json.dumps({{
            "jsonrpc": "2.0", "id": 1,
            "method": "initialize", "params": {{}},
        }}).encode()
        req = urllib.request.Request(
            mcp_url, data=body,
            headers={{"Content-Type": "application/json"}},
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
            assert data["result"]["serverInfo"]["name"] == "tabula-canvas"
        print("MOCK_CLAUDE_OK")
    """), encoding="utf-8")
    mock_claude.chmod(mock_claude.stat().st_mode | stat.S_IEXEC)

    async def _run() -> None:
        tc = TestClient(TestServer(app))
        async with tc:
            port = tc.port
            mcp_url = f"http://127.0.0.1:{port}/mcp"

            env = os.environ.copy()
            env["PATH"] = str(tmp_path) + os.pathsep + env.get("PATH", "")
            proc = await asyncio.create_subprocess_exec(
                sys.executable, "-m", "tabula", "run",
                "--assistant", "claude",
                "--mcp-url", mcp_url,
                "--project-dir", str(tmp_path),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=env,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=15.0)
            assert proc.returncode == 0, f"stderr: {stderr.decode()}"
            assert b"MOCK_CLAUDE_OK" in stdout

    asyncio.run(_run())


def test_ai_launch_unreachable_mcp_exits(tmp_path: Path) -> None:
    script_content = textwrap.dedent("""\
        import json
        import sys
        import urllib.request

        mcp_url = sys.argv[1]
        body = json.dumps({
            "jsonrpc": "2.0", "id": 1,
            "method": "initialize", "params": {},
        }).encode()
        req = urllib.request.Request(
            mcp_url, data=body,
            headers={"Content-Type": "application/json"},
        )
        try:
            urllib.request.urlopen(req, timeout=3)
        except Exception as e:
            print(f"MCP connection failed: {e}", file=sys.stderr)
            sys.exit(1)
    """)
    script = _write_mock_script(tmp_path, script_content)

    async def _run() -> None:
        proc = await asyncio.create_subprocess_exec(
            sys.executable, str(script), "http://127.0.0.1:1/mcp",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10.0)
        assert proc.returncode != 0
        assert b"MCP connection failed" in stderr or b"Connection refused" in stderr

    asyncio.run(_run())


def test_pty_launches_claude_with_mcp_config(tmp_path: Path) -> None:
    """End-to-end: the exact command launchAI() sends works through a PTY."""
    mock_claude = tmp_path / "claude"
    mock_claude.write_text(textwrap.dedent(f"""\
        #!/usr/bin/env {sys.executable}
        import json, sys, urllib.request
        for i, arg in enumerate(sys.argv):
            if arg == "--mcp-config":
                cfg = json.loads(sys.argv[i + 1])
                url = cfg["mcpServers"]["tabula-canvas"]["url"]
                break
        else:
            sys.exit(1)
        body = json.dumps({{"jsonrpc": "2.0", "id": 1,
                            "method": "initialize", "params": {{}}}}).encode()
        req = urllib.request.Request(url, data=body,
                                    headers={{"Content-Type": "application/json"}})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
            assert data["result"]["serverInfo"]["name"] == "tabula-canvas"
        print("PTY_LAUNCH_OK")
    """), encoding="utf-8")
    mock_claude.chmod(mock_claude.stat().st_mode | stat.S_IEXEC)

    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            mcp_url = f"http://127.0.0.1:{client.port}/mcp"
            cfg = json.dumps({"mcpServers": {"tabula-canvas": {"url": mcp_url}}})
            cmd = f"export PATH={tmp_path}:$PATH; claude --mcp-config '{cfg}'\n"

            transport = await LocalPtyTransport.open(str(tmp_path))
            try:
                await asyncio.sleep(0.3)
                transport.write(cmd.encode())
                output = await read_pty_until(
                    transport._fd, b"PTY_LAUNCH_OK", timeout=10.0,
                )
                assert b"PTY_LAUNCH_OK" in output
            finally:
                transport.close()

    asyncio.run(_run())


def test_ai_launch_missing_cli_exits(tmp_path: Path) -> None:
    fake_bin = tmp_path / "fake_bin"
    fake_bin.mkdir()
    for cmd in ("git", "bash", "sh"):
        real = shutil.which(cmd)
        if real:
            (fake_bin / cmd).symlink_to(real)

    async def _run() -> None:
        env = os.environ.copy()
        env["PATH"] = str(fake_bin)
        proc = await asyncio.create_subprocess_exec(
            sys.executable, "-m", "tabula", "run",
            "--assistant", "claude",
            "--mcp-url", "http://127.0.0.1:9420/mcp",
            "--project-dir", str(tmp_path),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=env,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10.0)
        assert proc.returncode != 0
        assert b"CLI not found" in stderr

    asyncio.run(_run())
