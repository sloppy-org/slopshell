from __future__ import annotations

import io

from tabula.canvas_adapter import CanvasAdapter
from tabula.mcp_server import TabulaMcpServer, read_message


def _call(server: TabulaMcpServer, request: dict[str, object]) -> dict[str, object]:
    out = io.BytesIO()
    server.output_stream = out
    server.handle_message(request)
    out.seek(0)
    response = read_message(out)
    assert response is not None
    return response


def test_initialize_returns_server_capabilities(tmp_path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    response = _call(server, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}})
    assert response["id"] == 1
    result = response["result"]
    assert result["serverInfo"]["name"] == "tabula-canvas"
    assert "tools" in result["capabilities"]
    assert "resources" in result["capabilities"]


def test_tools_list_exposes_canvas_tools(tmp_path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    response = _call(server, {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
    names = [item["name"] for item in response["result"]["tools"]]
    assert "canvas_activate" in names
    assert "canvas_render_text" in names
    assert "canvas_render_image" in names
    assert "canvas_render_pdf" in names
    assert "canvas_clear" in names
    assert "canvas_status" in names
    assert "canvas_history" in names


def test_tools_call_render_text_updates_state_and_history(tmp_path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    render = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {
                "name": "canvas_render_text",
                "arguments": {
                    "session_id": "s1",
                    "title": "draft",
                    "markdown_or_text": "hello",
                },
            },
        },
    )
    assert render["result"]["isError"] is False
    assert render["result"]["structuredContent"]["kind"] == "text_artifact"

    status = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 4,
            "method": "tools/call",
            "params": {"name": "canvas_status", "arguments": {"session_id": "s1"}},
        },
    )
    assert status["result"]["structuredContent"]["mode"] == "discussion"

    history = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 5,
            "method": "tools/call",
            "params": {"name": "canvas_history", "arguments": {"session_id": "s1", "limit": 10}},
        },
    )
    assert history["result"]["structuredContent"]["count"] == 1
    assert history["result"]["structuredContent"]["events"][0]["kind"] == "text_artifact"


def test_resources_list_and_read_surface_session_state(tmp_path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 6,
            "method": "tools/call",
            "params": {
                "name": "canvas_render_text",
                "arguments": {"session_id": "s1", "title": "t", "markdown_or_text": "x"},
            },
        },
    )

    resources = _call(server, {"jsonrpc": "2.0", "id": 7, "method": "resources/list", "params": {}})
    uris = [item["uri"] for item in resources["result"]["resources"]]
    assert "tabula://sessions" in uris
    assert "tabula://session/s1" in uris

    status_read = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 8,
            "method": "resources/read",
            "params": {"uri": "tabula://session/s1"},
        },
    )
    assert "discussion" in status_read["result"]["contents"][0]["text"]


def test_tools_call_unknown_tool_returns_error_payload(tmp_path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    response = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 9,
            "method": "tools/call",
            "params": {"name": "unknown_tool", "arguments": {}},
        },
    )
    assert response["result"]["isError"] is True
    assert "unknown tool" in response["result"]["structuredContent"]["error"]
