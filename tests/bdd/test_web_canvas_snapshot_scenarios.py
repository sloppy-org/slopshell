from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any

from tabula.web.server import TabulaWebApp


def test_given_review_status_and_text_history_when_snapshot_requested_then_latest_text_event_is_returned(
    tmp_path: Path,
) -> None:
    async def _run() -> None:
        app = TabulaWebApp(data_dir=tmp_path)

        async def _fake_call(*, tunnel_port: int, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
            assert tunnel_port == 9999
            assert arguments["session_id"] == "s1"
            if name == "canvas_status":
                return {
                    "mode": "review",
                    "active": True,
                    "active_artifact": {"event_id": "e1", "kind": "text_artifact", "title": "T", "text": "hello"},
                }
            raise AssertionError(f"unexpected tool call: {name}")

        app._mcp_tools_call = _fake_call  # type: ignore[method-assign]
        snapshot = await app._canvas_snapshot_for_tunnel(tunnel_port=9999, session_id="s1")

        assert snapshot["status"]["mode"] == "review"
        assert snapshot["event"]["kind"] == "text_artifact"

    asyncio.run(_run())


def test_given_review_status_and_image_history_when_snapshot_requested_then_latest_image_event_is_returned(
    tmp_path: Path,
) -> None:
    async def _run() -> None:
        app = TabulaWebApp(data_dir=tmp_path)

        async def _fake_call(*, tunnel_port: int, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
            if name == "canvas_status":
                return {
                    "mode": "review",
                    "active": True,
                    "active_artifact": {"event_id": "e2", "kind": "image_artifact", "title": "I", "path": "img.png"},
                }
            raise AssertionError(f"unexpected tool call: {name}")

        app._mcp_tools_call = _fake_call  # type: ignore[method-assign]
        snapshot = await app._canvas_snapshot_for_tunnel(tunnel_port=9999, session_id="s1")

        assert snapshot["status"]["mode"] == "review"
        assert snapshot["event"]["kind"] == "image_artifact"

    asyncio.run(_run())


def test_given_review_status_and_pdf_history_when_snapshot_requested_then_latest_pdf_event_is_returned(
    tmp_path: Path,
) -> None:
    async def _run() -> None:
        app = TabulaWebApp(data_dir=tmp_path)

        async def _fake_call(*, tunnel_port: int, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
            if name == "canvas_status":
                return {
                    "mode": "review",
                    "active": True,
                    "active_artifact": {
                        "event_id": "e3",
                        "kind": "pdf_artifact",
                        "title": "P",
                        "path": "doc.pdf",
                        "page": 0,
                    },
                }
            raise AssertionError(f"unexpected tool call: {name}")

        app._mcp_tools_call = _fake_call  # type: ignore[method-assign]
        snapshot = await app._canvas_snapshot_for_tunnel(tunnel_port=9999, session_id="s1")

        assert snapshot["status"]["mode"] == "review"
        assert snapshot["event"]["kind"] == "pdf_artifact"

    asyncio.run(_run())


def test_given_prompt_status_and_clear_history_when_snapshot_requested_then_clear_event_is_preserved(
    tmp_path: Path,
) -> None:
    async def _run() -> None:
        app = TabulaWebApp(data_dir=tmp_path)

        async def _fake_call(*, tunnel_port: int, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
            if name == "canvas_status":
                return {
                    "mode": "prompt",
                    "active": True,
                    "active_artifact": {"event_id": "e4", "kind": "clear_canvas", "reason": "done"},
                }
            raise AssertionError(f"unexpected tool call: {name}")

        app._mcp_tools_call = _fake_call  # type: ignore[method-assign]
        snapshot = await app._canvas_snapshot_for_tunnel(tunnel_port=9999, session_id="s1")

        assert snapshot["status"]["mode"] == "prompt"
        assert snapshot["event"]["kind"] == "clear_canvas"

    asyncio.run(_run())


def test_given_prompt_status_and_empty_history_when_snapshot_requested_then_event_is_none(tmp_path: Path) -> None:
    async def _run() -> None:
        app = TabulaWebApp(data_dir=tmp_path)

        async def _fake_call(*, tunnel_port: int, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
            if name == "canvas_status":
                return {"mode": "prompt", "active": True, "active_artifact": None}
            raise AssertionError(f"unexpected tool call: {name}")

        app._mcp_tools_call = _fake_call  # type: ignore[method-assign]
        snapshot = await app._canvas_snapshot_for_tunnel(tunnel_port=9999, session_id="s1")

        assert snapshot["status"]["mode"] == "prompt"
        assert snapshot["event"] is None

    asyncio.run(_run())
