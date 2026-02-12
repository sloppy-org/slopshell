from __future__ import annotations

import asyncio
import os
import socket
from pathlib import Path

from aiohttp.test_utils import TestClient, TestServer

from tabula.serve import TabulaServeApp


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


async def read_pty_until(fd: int, marker: bytes, timeout: float = 5.0) -> bytes:
    output = b""
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        try:
            chunk = os.read(fd, 4096)
            output += chunk
            if marker in output:
                return output
        except BlockingIOError:
            await asyncio.sleep(0.05)
    return output


async def make_serve_client(project_dir: Path) -> TestClient:
    serve_app = TabulaServeApp(project_dir=project_dir)
    app = serve_app.create_app()
    return TestClient(TestServer(app))


def rpc(method: str, params: dict | None = None, *, msg_id: int = 1) -> dict:
    return {"jsonrpc": "2.0", "id": msg_id, "method": method, "params": params or {}}
