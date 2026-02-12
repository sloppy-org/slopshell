from __future__ import annotations

import asyncio
import json
from pathlib import Path

from .conftest import make_serve_client, rpc


def test_mcp_to_ws_text_artifact(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            ws = await client.ws_connect("/ws/canvas")

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t", "markdown_or_text": "hello text"},
            }))

            msg = await asyncio.wait_for(ws.receive_str(), timeout=2.0)
            payload = json.loads(msg)
            assert payload["kind"] == "text_artifact"
            assert payload["text"] == "hello text"
            assert payload["title"] == "t"

            await ws.close()

    asyncio.run(_run())


def test_mcp_to_ws_image_artifact(tmp_path: Path) -> None:
    img = tmp_path / "pic.png"
    img.write_bytes(b"\x89PNG")

    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            ws = await client.ws_connect("/ws/canvas")

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_image",
                "arguments": {"session_id": "s1", "title": "img", "path": str(img)},
            }))

            msg = await asyncio.wait_for(ws.receive_str(), timeout=2.0)
            payload = json.loads(msg)
            assert payload["kind"] == "image_artifact"
            assert payload["title"] == "img"

            await ws.close()

    asyncio.run(_run())


def test_mcp_to_ws_clear_event(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            ws = await client.ws_connect("/ws/canvas")

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t", "markdown_or_text": "body"},
            }))
            await asyncio.wait_for(ws.receive_str(), timeout=2.0)

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_clear",
                "arguments": {"session_id": "s1", "reason": "done"},
            }, msg_id=2))

            msg = await asyncio.wait_for(ws.receive_str(), timeout=2.0)
            payload = json.loads(msg)
            assert payload["kind"] == "clear_canvas"
            assert payload["reason"] == "done"

            await ws.close()

    asyncio.run(_run())


def test_selection_feedback_roundtrip(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t", "markdown_or_text": "line1\nline2\nline3"},
            }))
            data = await resp.json()
            event_id = data["result"]["structuredContent"]["artifact_id"]

            ws = await client.ws_connect("/ws/canvas")
            await ws.send_json({
                "kind": "text_selection",
                "event_id": event_id,
                "line_start": 2,
                "line_end": 3,
                "text": "line2\nline3",
            })

            for _ in range(20):
                resp = await client.post("/mcp", json=rpc("tools/call", {
                    "name": "canvas_selection",
                    "arguments": {"session_id": "s1"},
                }, msg_id=2))
                data = await resp.json()
                sel = data["result"]["structuredContent"]["selection"]
                if sel["has_selection"]:
                    break
                await asyncio.sleep(0.05)

            assert sel["has_selection"] is True
            assert sel["text"] == "line2\nline3"
            assert sel["line_start"] == 2
            assert sel["line_end"] == 3

            await ws.close()

    asyncio.run(_run())


def test_selection_cleared_on_new_render(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t1", "markdown_or_text": "a\nb"},
            }))
            data = await resp.json()
            event_id = data["result"]["structuredContent"]["artifact_id"]

            ws = await client.ws_connect("/ws/canvas")
            await ws.send_json({
                "kind": "text_selection",
                "event_id": event_id,
                "line_start": 1,
                "line_end": 1,
                "text": "a",
            })

            for _ in range(20):
                resp = await client.post("/mcp", json=rpc("tools/call", {
                    "name": "canvas_selection",
                    "arguments": {"session_id": "s1"},
                }, msg_id=2))
                data = await resp.json()
                if data["result"]["structuredContent"]["selection"]["has_selection"]:
                    break
                await asyncio.sleep(0.05)
            assert data["result"]["structuredContent"]["selection"]["has_selection"] is True

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t2", "markdown_or_text": "new"},
            }, msg_id=3))

            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_selection",
                "arguments": {"session_id": "s1"},
            }, msg_id=4))
            data = await resp.json()
            assert data["result"]["structuredContent"]["selection"]["has_selection"] is False

            await ws.close()

    asyncio.run(_run())


def test_multiple_sessions_isolated(tmp_path: Path) -> None:
    async def _run() -> None:
        client = await make_serve_client(tmp_path)
        async with client:
            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "alpha", "title": "t", "markdown_or_text": "alpha-text"},
            }, msg_id=1))

            await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_render_text",
                "arguments": {"session_id": "beta", "title": "t", "markdown_or_text": "beta-text"},
            }, msg_id=2))

            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_history",
                "arguments": {"session_id": "alpha"},
            }, msg_id=3))
            data = await resp.json()
            events = data["result"]["structuredContent"]["events"]
            assert len(events) == 1
            assert events[0]["text"] == "alpha-text"

            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_history",
                "arguments": {"session_id": "beta"},
            }, msg_id=4))
            data = await resp.json()
            events = data["result"]["structuredContent"]["events"]
            assert len(events) == 1
            assert events[0]["text"] == "beta-text"

            resp = await client.post("/mcp", json=rpc("tools/call", {
                "name": "canvas_status",
                "arguments": {"session_id": "alpha"},
            }, msg_id=5))
            data = await resp.json()
            assert data["result"]["structuredContent"]["active_kind"] == "text_artifact"

    asyncio.run(_run())
