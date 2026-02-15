from __future__ import annotations

import io

from tabula.canvas_adapter import CanvasAdapter
from tabula.mcp_server import StdioTransport, TabulaMcpServer, read_message


def _call(transport: StdioTransport, request: dict[str, object]) -> dict[str, object]:
    out = io.BytesIO()
    transport.output_stream = out
    transport.handle_message(request)
    out.seek(0)
    response = read_message(out)
    assert response is not None
    return response


def _make_transport(tmp_path) -> StdioTransport:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter)
    return StdioTransport(server, input_stream=io.BytesIO(), output_stream=io.BytesIO())


def test_initialize_returns_server_capabilities(tmp_path) -> None:
    transport = _make_transport(tmp_path)

    response = _call(transport, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}})
    assert response["id"] == 1
    result = response["result"]
    assert result["serverInfo"]["name"] == "tabula-canvas"
    assert "tools" in result["capabilities"]
    assert "resources" in result["capabilities"]


def test_tools_list_exposes_canvas_tools(tmp_path) -> None:
    transport = _make_transport(tmp_path)

    response = _call(transport, {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
    names = [item["name"] for item in response["result"]["tools"]]
    assert "canvas_session_open" in names
    assert "canvas_artifact_show" in names
    assert "canvas_mark_set" in names
    assert "canvas_mark_delete" in names
    assert "canvas_marks_list" in names
    assert "canvas_mark_focus" in names
    assert "canvas_commit" in names
    assert "canvas_status" in names


def test_tools_call_render_text_updates_state_and_history(tmp_path) -> None:
    transport = _make_transport(tmp_path)

    render = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {
                "name": "canvas_artifact_show",
                "arguments": {
                    "session_id": "s1",
                    "kind": "text",
                    "title": "draft",
                    "markdown_or_text": "hello",
                },
            },
        },
    )
    assert render["result"]["isError"] is False
    assert render["result"]["structuredContent"]["kind"] == "text_artifact"

    status = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 4,
            "method": "tools/call",
            "params": {"name": "canvas_status", "arguments": {"session_id": "s1"}},
        },
    )
    assert status["result"]["structuredContent"]["mode"] == "review"
    assert status["result"]["structuredContent"]["marks_total"] == 0

    mark = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 41,
            "method": "tools/call",
            "params": {
                "name": "canvas_mark_set",
                "arguments": {
                    "session_id": "s1",
                    "intent": "draft",
                    "type": "highlight",
                    "target_kind": "text_range",
                    "target": {"line_start": 1, "line_end": 1, "quote": "hello"},
                },
            },
        },
    )
    assert mark["result"]["isError"] is False
    mark_id = mark["result"]["structuredContent"]["mark"]["mark_id"]

    marks = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 5,
            "method": "tools/call",
            "params": {"name": "canvas_marks_list", "arguments": {"session_id": "s1"}},
        },
    )
    assert marks["result"]["structuredContent"]["count"] == 1
    assert marks["result"]["structuredContent"]["marks"][0]["mark_id"] == mark_id


def test_resources_list_and_read_surface_session_state(tmp_path) -> None:
    transport = _make_transport(tmp_path)

    _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 6,
            "method": "tools/call",
            "params": {
                "name": "canvas_artifact_show",
                "arguments": {"session_id": "s1", "kind": "text", "title": "t", "markdown_or_text": "x"},
            },
        },
    )

    resources = _call(transport, {"jsonrpc": "2.0", "id": 7, "method": "resources/list", "params": {}})
    uris = [item["uri"] for item in resources["result"]["resources"]]
    assert "tabula://sessions" in uris
    assert "tabula://session/s1" in uris
    assert "tabula://session/s1/marks" in uris

    status_read = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 8,
            "method": "resources/read",
            "params": {"uri": "tabula://session/s1"},
        },
    )
    assert "review" in status_read["result"]["contents"][0]["text"]

    marks_read = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 81,
            "method": "resources/read",
            "params": {"uri": "tabula://session/s1/marks"},
        },
    )
    assert "\"marks\"" in marks_read["result"]["contents"][0]["text"]


def test_tools_call_unknown_tool_returns_error_payload(tmp_path) -> None:
    transport = _make_transport(tmp_path)

    response = _call(
        transport,
        {
            "jsonrpc": "2.0",
            "id": 9,
            "method": "tools/call",
            "params": {"name": "unknown_tool", "arguments": {}},
        },
    )
    assert response["result"]["isError"] is True
    assert "unknown tool" in response["result"]["structuredContent"]["error"]
