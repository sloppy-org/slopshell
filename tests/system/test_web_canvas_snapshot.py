from __future__ import annotations

import asyncio
import base64
from pathlib import Path
from unittest.mock import patch

import aiohttp
from aiohttp.test_utils import TestClient, TestServer

import tabula.web.server as _server_mod
from tabula.web.server import LOCAL_SESSION_ID, TabulaWebApp

from .conftest import free_port


async def _make_web_client(data_dir: Path, project_dir: Path) -> TestClient:
    app_obj = TabulaWebApp(data_dir=data_dir, local_project_dir=project_dir)
    app = app_obj.create_app()
    return TestClient(TestServer(app))


async def _authenticate(client: TestClient, password: str = "testpass") -> None:
    await client.post("/api/setup", json={"password": password})


async def _call_tool(port: int, *, msg_id: int, name: str, arguments: dict[str, object]) -> dict[str, object]:
    async with aiohttp.ClientSession() as cs:
        resp = await cs.post(
            f"http://127.0.0.1:{port}/mcp",
            json={
                "jsonrpc": "2.0",
                "id": msg_id,
                "method": "tools/call",
                "params": {"name": name, "arguments": arguments},
            },
            timeout=aiohttp.ClientTimeout(total=5),
        )
        payload = await resp.json()
        return payload["result"]["structuredContent"]


def _write_test_png(path: Path) -> None:
    png_b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7+X1cAAAAASUVORK5CYII="
    path.write_bytes(base64.b64decode(png_b64))


def _write_test_pdf(path: Path) -> None:
    path.write_bytes(
        b"%PDF-1.4\n"
        b"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
        b"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n"
        b"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>\nendobj\n"
        b"trailer\n<< /Root 1 0 R >>\n%%EOF\n"
    )


def test_canvas_snapshot_endpoint_reports_text_image_pdf_and_clear(tmp_path: Path) -> None:
    data_dir = tmp_path / "data"
    project_dir = tmp_path / "project"
    project_dir.mkdir()

    image_path = project_dir / "snapshot-image.png"
    pdf_path = project_dir / "snapshot-doc.pdf"
    _write_test_png(image_path)
    _write_test_pdf(pdf_path)

    port = free_port()

    async def _run() -> None:
        with patch.object(_server_mod, "DAEMON_PORT", port):
            client = await _make_web_client(data_dir, project_dir)
            async with client:
                await _authenticate(client)

                text = await _call_tool(
                    port,
                    msg_id=1,
                    name="canvas_artifact_show",
                    arguments={
                        "session_id": LOCAL_SESSION_ID,
                        "kind": "text",
                        "title": "Text Snapshot",
                        "markdown_or_text": "hello snapshot text",
                    },
                )
                assert text["kind"] == "text_artifact"
                resp = await client.get(f"/api/canvas/{LOCAL_SESSION_ID}/snapshot")
                snap = await resp.json()
                assert snap["status"]["mode"] == "review"
                assert snap["event"]["kind"] == "text_artifact"

                img = await _call_tool(
                    port,
                    msg_id=2,
                    name="canvas_artifact_show",
                    arguments={
                        "session_id": LOCAL_SESSION_ID,
                        "kind": "image",
                        "title": "Image Snapshot",
                        "path": str(image_path),
                    },
                )
                assert img["kind"] == "image_artifact"
                resp = await client.get(f"/api/canvas/{LOCAL_SESSION_ID}/snapshot")
                snap = await resp.json()
                assert snap["status"]["mode"] == "review"
                assert snap["event"]["kind"] == "image_artifact"

                pdf = await _call_tool(
                    port,
                    msg_id=3,
                    name="canvas_artifact_show",
                    arguments={
                        "session_id": LOCAL_SESSION_ID,
                        "kind": "pdf",
                        "title": "PDF Snapshot",
                        "path": str(pdf_path),
                        "page": 0,
                    },
                )
                assert pdf["kind"] == "pdf_artifact"
                resp = await client.get(f"/api/canvas/{LOCAL_SESSION_ID}/snapshot")
                snap = await resp.json()
                assert snap["status"]["mode"] == "review"
                assert snap["event"]["kind"] == "pdf_artifact"

                cleared = await _call_tool(
                    port,
                    msg_id=4,
                    name="canvas_artifact_show",
                    arguments={"session_id": LOCAL_SESSION_ID, "kind": "clear", "reason": "done"},
                )
                assert cleared["kind"] == "clear_canvas"
                resp = await client.get(f"/api/canvas/{LOCAL_SESSION_ID}/snapshot")
                snap = await resp.json()
                assert snap["status"]["mode"] == "prompt"
                assert snap["event"] is None

    asyncio.run(_run())
