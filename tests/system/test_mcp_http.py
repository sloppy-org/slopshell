from __future__ import annotations

import asyncio
import json
from pathlib import Path

from aiohttp.test_utils import TestClient

from .conftest import make_serve_client, rpc


async def _call_tool(client: TestClient, name: str, arguments: dict, msg_id: int = 1) -> dict:
    resp = await client.post("/mcp", json=rpc("tools/call", {
        "name": name, "arguments": arguments,
    }, msg_id=msg_id))
    data = await resp.json()
    assert data["result"]["isError"] is False
    return data["result"]["structuredContent"]


def test_mcp_initialize_handshake(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json=rpc("initialize"))
            assert resp.status == 200
            assert "Mcp-Session-Id" in resp.headers
            data = await resp.json()
            assert data["result"]["serverInfo"]["name"] == "tabula-canvas"
            assert "protocolVersion" in data["result"]
            assert "capabilities" in data["result"]

    asyncio.run(_run())


def test_mcp_all_tools_roundtrip(tmp_path: Path) -> None:
    img = tmp_path / "test.png"
    img.write_bytes(b"\x89PNG")
    pdf = tmp_path / "test.pdf"
    pdf.write_bytes(b"%PDF-1.4")

    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            sid = "sys-test"
            c = await _call_tool(client, "canvas_activate", {"session_id": sid}, 1)
            assert c["active"] is True

            c = await _call_tool(client, "canvas_render_text", {"session_id": sid, "title": "t", "markdown_or_text": "hello"}, 2)
            assert c["kind"] == "text_artifact"

            c = await _call_tool(client, "canvas_render_image", {"session_id": sid, "title": "img", "path": str(img)}, 3)
            assert c["kind"] == "image_artifact"

            c = await _call_tool(client, "canvas_render_pdf", {"session_id": sid, "title": "doc", "path": str(pdf)}, 4)
            assert c["kind"] == "pdf_artifact"

            c = await _call_tool(client, "canvas_status", {"session_id": sid}, 5)
            assert c["mode"] == "review"

            await _call_tool(client, "canvas_selection", {"session_id": sid}, 6)

            c = await _call_tool(client, "canvas_history", {"session_id": sid}, 7)
            assert c["count"] >= 3

            c = await _call_tool(client, "canvas_clear", {"session_id": sid}, 8)
            assert c["cleared"] is True

    asyncio.run(_run())


def test_mcp_event_broadcast_to_multiple_ws(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            ws1 = await client.ws_connect("/ws/canvas")
            ws2 = await client.ws_connect("/ws/canvas")

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "bc", "title": "t", "markdown_or_text": "broadcast"},
            }))

            msg1 = await asyncio.wait_for(ws1.receive_str(), timeout=2.0)
            msg2 = await asyncio.wait_for(ws2.receive_str(), timeout=2.0)

            p1 = json.loads(msg1)
            p2 = json.loads(msg2)
            assert p1["kind"] == "text_artifact"
            assert p2["kind"] == "text_artifact"
            assert p1["text"] == "broadcast"
            assert p2["text"] == "broadcast"

            await ws1.close()
            await ws2.close()

    asyncio.run(_run())


def test_mcp_error_bad_json(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post(
                "/mcp", data=b"not-json{{{",
                headers={"Content-Type": "application/json"},
            )
            data = await resp.json()
            assert data["error"]["code"] == -32700

    asyncio.run(_run())


def test_mcp_error_unknown_method(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json=rpc("nonexistent/method"))
            data = await resp.json()
            assert data["error"]["code"] == -32601

    asyncio.run(_run())


def test_mcp_error_invalid_params(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s"},
            }))
            data = await resp.json()
            assert data["result"]["isError"] is True

    asyncio.run(_run())


def test_mcp_notification_returns_202(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json={
                "jsonrpc": "2.0",
                "method": "notifications/initialized",
                "params": {},
            })
            assert resp.status == 202

    asyncio.run(_run())


def test_mcp_resources_list_and_read(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_activate",
                "arguments": {"session_id": "res-test"},
            }))

            resp = await client.post("/mcp", json=rpc("resources/list", msg_id=2))
            data = await resp.json()
            uris = [r["uri"] for r in data["result"]["resources"]]
            assert "tabula://sessions" in uris
            assert "tabula://session/res-test" in uris

            resp = await client.post("/mcp", json=rpc("resources/read", {
                "uri": "tabula://session/res-test",
            }, msg_id=3))
            data = await resp.json()
            contents = data["result"]["contents"]
            assert len(contents) == 1
            payload = json.loads(contents[0]["text"])
            assert "mode" in payload

            resp = await client.post("/mcp", json=rpc("resources/read", {
                "uri": "tabula://session/res-test/history",
            }, msg_id=4))
            data = await resp.json()
            contents = data["result"]["contents"]
            payload = json.loads(contents[0]["text"])
            assert "events" in payload

    asyncio.run(_run())


def test_mcp_sse_keepalive(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.get("/mcp")
            assert resp.status == 200
            assert resp.headers["Content-Type"].startswith("text/event-stream")
            chunk = await asyncio.wait_for(resp.content.readline(), timeout=35.0)
            assert chunk.strip() == b": keepalive"

    asyncio.run(_run())


def test_mcp_delete_returns_204(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.delete("/mcp")
            assert resp.status == 204

    asyncio.run(_run())
