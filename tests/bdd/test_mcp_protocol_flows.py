from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

import tabula.mcp_server as mcp
from tabula.canvas_adapter import CanvasAdapter
from tabula.mcp_server import RpcError, TabulaMcpServer, read_message, run_mcp_stdio_server, write_message


def _frame(payload: dict[str, object]) -> bytes:
    body = json.dumps(payload, separators=(",", ":"), ensure_ascii=True).encode("utf-8")
    return f"Content-Length: {len(body)}\r\n\r\n".encode("utf-8") + body


def _call(server: TabulaMcpServer, request: dict[str, object]) -> dict[str, object]:
    out = io.BytesIO()
    server.output_stream = out
    server.handle_message(request)
    out.seek(0)
    response = read_message(out)
    assert response is not None
    return response


def test_given_single_line_json_when_read_message_then_parsed() -> None:
    stream = io.BytesIO(b'{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}\n')
    parsed = read_message(stream)
    assert parsed is not None
    assert parsed["method"] == "ping"


@pytest.mark.parametrize(
    "blob",
    [
        b"BadHeader\r\n\r\n",
        b"X-Test: 1\r\n\r\n",
        b"Content-Length: nope\r\n\r\n{}",
        b"Content-Length: 3\r\n\r\n{}",
        b"Content-Length: 4\r\n\r\nnope",
        b"Content-Length: 2\r\n\r\n[]",
    ],
)
def test_given_invalid_framed_inputs_when_read_message_then_protocol_errors(blob: bytes) -> None:
    with pytest.raises(RpcError):
        read_message(io.BytesIO(blob))


def test_given_parse_error_then_run_forever_emits_error_and_continues(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    bad = b"BadHeader\r\n"
    good = _frame({"jsonrpc": "2.0", "id": 10, "method": "ping", "params": {}})
    stream_in = io.BytesIO(bad + good)
    stream_out = io.BytesIO()
    server = TabulaMcpServer(adapter, input_stream=stream_in, output_stream=stream_out)

    rc = server.run_forever()
    assert rc == 0

    stream_out.seek(0)
    first = read_message(stream_out)
    second = read_message(stream_out)
    assert first is not None and second is not None
    assert first["error"]["code"] == -32700
    assert second["id"] == 10
    assert second["result"] == {}


def test_given_notification_without_id_when_handled_then_no_output(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    out = io.BytesIO()
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=out)
    server.handle_message({"jsonrpc": "2.0", "method": "ping", "params": {}})
    assert out.getvalue() == b""


def test_given_request_missing_method_when_handled_then_jsonrpc_error(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())
    response = _call(server, {"jsonrpc": "2.0", "id": 1, "params": {}})
    assert response["error"]["code"] == -32600


def test_given_non_object_params_when_dispatch_then_invalid_params_error(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())
    response = _call(server, {"jsonrpc": "2.0", "id": 2, "method": "ping", "params": []})
    assert response["error"]["code"] == -32602


def test_given_resources_templates_when_called_then_templates_are_returned(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    response = _call(server, {"jsonrpc": "2.0", "id": 3, "method": "resources/templates/list", "params": {}})
    templates = response["result"]["resourceTemplates"]
    uris = [item["uriTemplate"] for item in templates]
    assert "tabula://session/{session_id}" in uris
    assert "tabula://session/{session_id}/history" in uris


def test_given_tool_specific_bad_args_when_called_then_is_error_result_payload(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    mode_hint_bad = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 5,
            "method": "tools/call",
            "params": {"name": "canvas_activate", "arguments": {"session_id": "s1", "mode_hint": 1}},
        },
    )
    assert mode_hint_bad["result"]["isError"] is True

    pdf_page_bad = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 6,
            "method": "tools/call",
            "params": {
                "name": "canvas_render_pdf",
                "arguments": {"session_id": "s1", "title": "doc", "path": "a.pdf", "page": "x"},
            },
        },
    )
    assert pdf_page_bad["result"]["isError"] is True

    reason_bad = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 7,
            "method": "tools/call",
            "params": {"name": "canvas_clear", "arguments": {"session_id": "s1", "reason": 9}},
        },
    )
    assert reason_bad["result"]["isError"] is True


def test_given_all_tool_happy_paths_when_called_then_status_and_mode_progression_are_consistent(tmp_path: Path) -> None:
    image = tmp_path / "img.png"
    image.write_bytes(b"x")
    pdf = tmp_path / "doc.pdf"
    pdf.write_bytes(b"%PDF-1.4")
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())

    _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 8,
            "method": "tools/call",
            "params": {
                "name": "canvas_render_image",
                "arguments": {"session_id": "s1", "title": "img", "path": str(image)},
            },
        },
    )
    _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 9,
            "method": "tools/call",
            "params": {
                "name": "canvas_render_pdf",
                "arguments": {"session_id": "s1", "title": "pdf", "path": str(pdf), "page": 0},
            },
        },
    )
    status = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 10,
            "method": "tools/call",
            "params": {"name": "canvas_status", "arguments": {"session_id": "s1"}},
        },
    )
    assert status["result"]["isError"] is False
    assert status["result"]["structuredContent"]["mode"] == "discussion"

    history = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 11,
            "method": "tools/call",
            "params": {"name": "canvas_history", "arguments": {"session_id": "s1", "limit": 10}},
        },
    )
    assert history["result"]["structuredContent"]["count"] == 2

    cleared = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 12,
            "method": "tools/call",
            "params": {"name": "canvas_clear", "arguments": {"session_id": "s1", "reason": "done"}},
        },
    )
    assert cleared["result"]["structuredContent"]["mode"] == "prompt"


def test_resources_read_unknown_uri_returns_jsonrpc_error(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    server = TabulaMcpServer(adapter, input_stream=io.BytesIO(), output_stream=io.BytesIO())
    response = _call(
        server,
        {
            "jsonrpc": "2.0",
            "id": 13,
            "method": "resources/read",
            "params": {"uri": "tabula://unknown"},
        },
    )
    assert response["error"]["code"] == -32602


def test_write_message_flushes_when_supported() -> None:
    class Flushable(io.BytesIO):
        def __init__(self):
            super().__init__()
            self.flushed = False

        def flush(self):
            self.flushed = True

    stream = Flushable()
    write_message(stream, {"jsonrpc": "2.0", "id": 1, "result": {}})
    assert stream.flushed is True
    stream.seek(0)
    parsed = read_message(stream)
    assert parsed is not None
    assert parsed["id"] == 1


def test_write_message_supports_jsonl_mode() -> None:
    stream = io.BytesIO()
    write_message(stream, {"jsonrpc": "2.0", "id": 2, "result": {}}, framed=False)
    stream.seek(0)
    line = stream.readline().decode("utf-8")
    payload = json.loads(line)
    assert payload["id"] == 2


def test_given_jsonl_input_when_run_forever_then_server_replies_in_jsonl(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    in_payload = {"jsonrpc": "2.0", "id": 31, "method": "ping", "params": {}}
    stream_in = io.BytesIO((json.dumps(in_payload) + "\n").encode("utf-8"))
    stream_out = io.BytesIO()
    server = TabulaMcpServer(adapter, input_stream=stream_in, output_stream=stream_out)

    rc = server.run_forever()
    assert rc == 0
    stream_out.seek(0)
    line = stream_out.readline().decode("utf-8")
    assert line.strip().startswith("{")
    assert "Content-Length" not in line
    payload = json.loads(line)
    assert payload["id"] == 31
    assert payload["result"] == {}


def test_run_mcp_stdio_server_constructs_adapter_and_runs(monkeypatch, tmp_path: Path) -> None:
    seen: dict[str, object] = {}

    class FakeServer:
        def __init__(self, adapter):
            seen["adapter"] = adapter

        def run_forever(self):
            return 77

    monkeypatch.setattr(mcp, "TabulaMcpServer", FakeServer)
    rc = run_mcp_stdio_server(
        project_dir=tmp_path,
        headless=True,
        start_canvas=False,
        poll_interval_ms=321,
    )
    assert rc == 77
    assert isinstance(seen["adapter"], CanvasAdapter)
