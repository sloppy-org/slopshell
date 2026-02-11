from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path


def _write_message(stream, payload: dict[str, object]) -> None:
    body = json.dumps(payload, separators=(",", ":"), ensure_ascii=True).encode("utf-8")
    header = f"Content-Length: {len(body)}\r\n\r\n".encode("utf-8")
    stream.write(header)
    stream.write(body)
    stream.flush()


def _read_message(stream) -> dict[str, object]:
    headers: dict[str, str] = {}
    while True:
        line = stream.readline()
        if not line:
            raise RuntimeError("unexpected EOF while reading MCP response headers")
        if line in (b"\r\n", b"\n"):
            break
        text = line.decode("utf-8").strip()
        key, value = text.split(":", 1)
        headers[key.strip().lower()] = value.strip()

    length = int(headers["content-length"])
    body = stream.read(length)
    return json.loads(body.decode("utf-8"))


def test_mcp_server_stdio_roundtrip_updates_canvas_state(tmp_path: Path) -> None:
    project = tmp_path / "proj"
    project.mkdir()
    env = os.environ.copy()
    env["PYTHONPATH"] = "src"

    proc = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "tabula",
            "mcp-server",
            "--project-dir",
            str(project),
            "--headless",
            "--no-canvas",
        ],
        cwd=Path.cwd(),
        env=env,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    try:
        assert proc.stdin is not None
        assert proc.stdout is not None

        _write_message(proc.stdin, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}})
        init_response = _read_message(proc.stdout)
        assert init_response["id"] == 1
        assert init_response["result"]["serverInfo"]["name"] == "tabula-canvas"

        _write_message(
            proc.stdin,
            {
                "jsonrpc": "2.0",
                "id": 2,
                "method": "tools/call",
                "params": {
                    "name": "canvas_render_text",
                    "arguments": {"session_id": "s1", "title": "t", "markdown_or_text": "hello"},
                },
            },
        )
        call_response = _read_message(proc.stdout)
        assert call_response["id"] == 2
        assert call_response["result"]["isError"] is False
        assert call_response["result"]["structuredContent"]["kind"] == "text_artifact"

        _write_message(
            proc.stdin,
            {
                "jsonrpc": "2.0",
                "id": 3,
                "method": "tools/call",
                "params": {
                    "name": "canvas_status",
                    "arguments": {"session_id": "s1"},
                },
            },
        )
        status_response = _read_message(proc.stdout)
        assert status_response["result"]["isError"] is False
        assert status_response["result"]["structuredContent"]["mode"] == "discussion"

        _write_message(
            proc.stdin,
            {
                "jsonrpc": "2.0",
                "id": 4,
                "method": "resources/read",
                "params": {"uri": "tabula://session/s1/history"},
            },
        )
        history_response = _read_message(proc.stdout)
        text = history_response["result"]["contents"][0]["text"]
        assert "text_artifact" in text
    finally:
        proc.terminate()
        proc.wait(timeout=5)
