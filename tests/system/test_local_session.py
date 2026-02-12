from __future__ import annotations

import asyncio
import json
from pathlib import Path
from unittest.mock import patch

import aiohttp
from aiohttp.test_utils import TestClient, TestServer

import tabula.web.server as _server_mod
from tabula.web.server import LOCAL_SESSION_ID, TabulaWebApp

from .conftest import free_port


async def _make_web_client(
    data_dir: Path, project_dir: Path,
) -> TestClient:
    app_obj = TabulaWebApp(data_dir=data_dir, local_project_dir=project_dir)
    app = app_obj.create_app()
    return TestClient(TestServer(app))


async def _ws_read_until(ws, marker: bytes, *, attempts: int = 50) -> bytes:
    output = b""
    for _ in range(attempts):
        try:
            msg = await asyncio.wait_for(ws.receive(), timeout=0.2)
            if msg.type == aiohttp.WSMsgType.BINARY:
                output += msg.data
            elif msg.type == aiohttp.WSMsgType.TEXT:
                output += msg.data.encode()
        except asyncio.TimeoutError:
            pass
        if marker in output:
            break
    return output


async def _authenticate(client: TestClient, password: str = "testpass") -> None:
    await client.post("/api/setup", json={"password": password})


def test_embedded_serve_starts(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                async with aiohttp.ClientSession() as cs:
                    url = f"http://127.0.0.1:{port}/health"
                    resp = await cs.get(url, timeout=aiohttp.ClientTimeout(total=5))
                    assert resp.status == 200
                    data = await resp.json()
                    assert data["status"] == "ok"

    asyncio.run(_run())


def test_setup_reports_local_session(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                resp = await client.get("/api/setup")
                data = await resp.json()
                assert data["local_session"] == LOCAL_SESSION_ID

    asyncio.run(_run())


def test_sessions_returns_local_details(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)
                resp = await client.get("/api/sessions")
                data = await resp.json()
                local = data["local_session"]
                assert local["session_id"] == LOCAL_SESSION_ID
                assert local["project_dir"] == str(project_dir)
                assert str(port) in local["mcp_url"]

    asyncio.run(_run())


def test_terminal_ws_echo(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)
                ws = await client.ws_connect(f"/ws/terminal/{LOCAL_SESSION_ID}")
                await ws.send_bytes(b"echo TERM_ECHO_TEST\n")
                output = await _ws_read_until(ws, b"TERM_ECHO_TEST")
                assert b"TERM_ECHO_TEST" in output
                await ws.close()

    asyncio.run(_run())


def test_terminal_ws_resize(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)
                ws = await client.ws_connect(f"/ws/terminal/{LOCAL_SESSION_ID}")
                await ws.send_str(json.dumps({"type": "resize", "cols": 200, "rows": 60}))
                await ws.send_bytes(b"echo COLS=$(tput cols)\n")
                output = await _ws_read_until(ws, b"COLS=200")
                assert b"COLS=200" in output
                await ws.close()

    asyncio.run(_run())


def test_canvas_ws_receives_mcp_events(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)

                canvas_ws = await client.ws_connect(f"/ws/canvas/{LOCAL_SESSION_ID}")

                async with aiohttp.ClientSession() as cs:
                    mcp_url = f"http://127.0.0.1:{port}/mcp"
                    await cs.post(mcp_url, json={
                        "jsonrpc": "2.0", "id": 1,
                        "method": "tools/call",
                        "params": {
                            "name": "canvas_render_text",
                            "arguments": {
                                "session_id": "local-test",
                                "title": "via-mcp",
                                "markdown_or_text": "relayed event",
                            },
                        },
                    })

                msg = await asyncio.wait_for(canvas_ws.receive_str(), timeout=5.0)
                payload = json.loads(msg)
                assert payload["kind"] == "text_artifact"
                assert payload["text"] == "relayed event"

                await canvas_ws.close()

    asyncio.run(_run())


def test_file_proxy_serves_local_files(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    test_file = project_dir / "readme.txt"
    test_file.write_text("local file content", encoding="utf-8")
    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)
                resp = await client.get(f"/api/files/{LOCAL_SESSION_ID}/readme.txt")
                assert resp.status == 200
                body = await resp.text()
                assert body == "local file content"

    asyncio.run(_run())
