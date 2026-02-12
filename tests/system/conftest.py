from __future__ import annotations

import socket
from pathlib import Path

from aiohttp.test_utils import TestClient, TestServer

from tabula.serve import TabulaServeApp


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


async def make_serve_client(project_dir: Path) -> TestClient:
    serve_app = TabulaServeApp(project_dir=project_dir)
    app = serve_app.create_app()
    return TestClient(TestServer(app))


def rpc(method: str, params: dict | None = None, *, msg_id: int = 1) -> dict:
    return {"jsonrpc": "2.0", "id": msg_id, "method": method, "params": params or {}}
