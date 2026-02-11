from __future__ import annotations

import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, BinaryIO
from urllib.parse import urlparse

from .canvas_adapter import CanvasAdapter

SERVER_NAME = "tabula-canvas"
SERVER_VERSION = "0.1.0"
# Codex CLI 0.98 currently negotiates MCP 2024-11-05.
MCP_PROTOCOL_VERSION = "2024-11-05"


class RpcError(Exception):
    def __init__(self, code: int, message: str) -> None:
        self.code = code
        self.message = message
        super().__init__(message)


def _read_message_with_mode(stream: BinaryIO) -> tuple[dict[str, Any] | None, str]:
    first_line = stream.readline()
    if not first_line:
        return None, "framed"

    stripped = first_line.lstrip()
    if stripped.startswith(b"{"):
        try:
            return json.loads(first_line.decode("utf-8")), "jsonl"
        except json.JSONDecodeError as exc:  # pragma: no cover
            raise RpcError(-32700, f"invalid json: {exc.msg}") from exc

    headers: dict[str, str] = {}
    line = first_line
    while line:
        if line in (b"\r\n", b"\n"):
            break
        text = line.decode("utf-8").strip()
        if ":" not in text:
            raise RpcError(-32700, "invalid header line")
        key, value = text.split(":", 1)
        headers[key.strip().lower()] = value.strip()
        line = stream.readline()

    if "content-length" not in headers:
        raise RpcError(-32700, "missing content-length header")

    try:
        length = int(headers["content-length"])
    except ValueError as exc:
        raise RpcError(-32700, "invalid content-length header") from exc

    body = stream.read(length)
    if len(body) != length:
        raise RpcError(-32700, "unexpected EOF while reading message")

    try:
        payload = json.loads(body.decode("utf-8"))
    except json.JSONDecodeError as exc:
        raise RpcError(-32700, f"invalid json: {exc.msg}") from exc

    if not isinstance(payload, dict):
        raise RpcError(-32600, "request must be an object")
    return payload, "framed"


def read_message(stream: BinaryIO) -> dict[str, Any] | None:
    payload, _mode = _read_message_with_mode(stream)
    return payload


def write_message(stream: BinaryIO, payload: dict[str, Any], *, framed: bool = True) -> None:
    encoded = json.dumps(payload, separators=(",", ":"), ensure_ascii=True).encode("utf-8")
    if framed:
        header = f"Content-Length: {len(encoded)}\r\n\r\n".encode("utf-8")
        stream.write(header)
        stream.write(encoded)
    else:
        stream.write(encoded + b"\n")
    if hasattr(stream, "flush"):
        stream.flush()


def _tool_definitions() -> list[dict[str, Any]]:
    return [
        {
            "name": "canvas_activate",
            "description": "Activate canvas session and optionally launch UI window.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "mode_hint": {"type": "string"},
                },
                "required": ["session_id"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_render_text",
            "description": "Render text/markdown artifact to canvas and switch mode to discussion.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "title": {"type": "string", "minLength": 1},
                    "markdown_or_text": {"type": "string"},
                },
                "required": ["session_id", "title", "markdown_or_text"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_render_image",
            "description": "Render image artifact from local path.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "title": {"type": "string", "minLength": 1},
                    "path": {"type": "string", "minLength": 1},
                },
                "required": ["session_id", "title", "path"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_render_pdf",
            "description": "Render PDF artifact from local path and optional page index.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "title": {"type": "string", "minLength": 1},
                    "path": {"type": "string", "minLength": 1},
                    "page": {"type": "integer", "minimum": 0},
                },
                "required": ["session_id", "title", "path"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_clear",
            "description": "Clear current canvas artifact and switch mode back to prompt.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "reason": {"type": "string"},
                },
                "required": ["session_id"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_status",
            "description": "Get current mode/status for a canvas session.",
            "inputSchema": {
                "type": "object",
                "properties": {"session_id": {"type": "string", "minLength": 1}},
                "required": ["session_id"],
                "additionalProperties": False,
            },
        },
        {
            "name": "canvas_history",
            "description": "Get recent in-memory event history for a canvas session.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "minLength": 1},
                    "limit": {"type": "integer", "minimum": 1},
                },
                "required": ["session_id"],
                "additionalProperties": False,
            },
        },
    ]


def _resource_templates() -> list[dict[str, Any]]:
    return [
        {
            "uriTemplate": "tabula://session/{session_id}",
            "name": "Canvas Session Status",
            "mimeType": "application/json",
            "description": "Current status for a canvas session.",
        },
        {
            "uriTemplate": "tabula://session/{session_id}/history",
            "name": "Canvas Session History",
            "mimeType": "application/json",
            "description": "Recent in-memory history for a canvas session.",
        },
    ]


@dataclass
class TabulaMcpServer:
    adapter: CanvasAdapter
    input_stream: BinaryIO
    output_stream: BinaryIO

    def __init__(
        self,
        adapter: CanvasAdapter,
        *,
        input_stream: BinaryIO | None = None,
        output_stream: BinaryIO | None = None,
    ) -> None:
        self.adapter = adapter
        self.input_stream = input_stream or sys.stdin.buffer
        self.output_stream = output_stream or sys.stdout.buffer
        self._wire_mode: str | None = None

    def _write(self, payload: dict[str, Any]) -> None:
        mode = self._wire_mode or "framed"
        write_message(self.output_stream, payload, framed=(mode == "framed"))

    def run_forever(self) -> int:
        while True:
            try:
                message, wire_mode = _read_message_with_mode(self.input_stream)
                if self._wire_mode is None:
                    self._wire_mode = wire_mode
            except RpcError as exc:
                self._write(
                    {
                        "jsonrpc": "2.0",
                        "id": None,
                        "error": {"code": exc.code, "message": exc.message},
                    }
                )
                continue

            if message is None:
                return 0
            self.handle_message(message)

    def handle_message(self, message: dict[str, Any]) -> None:
        msg_id = message.get("id")
        method = message.get("method")
        params = message.get("params", {})

        if method is None:
            if msg_id is not None:
                self._write_error(msg_id, -32600, "missing method")
            return

        if msg_id is None:
            return

        try:
            result = self._dispatch(method, params)
        except RpcError as exc:
            self._write_error(msg_id, exc.code, exc.message)
            return
        except Exception as exc:  # pragma: no cover
            self._write_error(msg_id, -32603, str(exc))
            return

        self._write({"jsonrpc": "2.0", "id": msg_id, "result": result})

    def _write_error(self, msg_id: Any, code: int, message: str) -> None:
        self._write(
            {
                "jsonrpc": "2.0",
                "id": msg_id,
                "error": {"code": code, "message": message},
            }
        )

    def _dispatch(self, method: str, params: Any) -> dict[str, Any]:
        if not isinstance(params, dict):
            raise RpcError(-32602, "params must be an object")

        if method == "initialize":
            return {
                "protocolVersion": MCP_PROTOCOL_VERSION,
                "capabilities": {
                    "tools": {"listChanged": False},
                    "resources": {"listChanged": False, "subscribe": False},
                },
                "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
            }
        if method == "ping":
            return {}
        if method == "tools/list":
            return {"tools": _tool_definitions()}
        if method == "tools/call":
            return self._dispatch_tool_call(params)
        if method == "resources/list":
            return self._resources_list()
        if method == "resources/templates/list":
            return {"resourceTemplates": _resource_templates()}
        if method == "resources/read":
            return self._resources_read(params)

        raise RpcError(-32601, f"method not found: {method}")

    def _dispatch_tool_call(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name")
        arguments = params.get("arguments", {})
        if not isinstance(name, str) or not name:
            raise RpcError(-32602, "tools/call requires non-empty name")
        if not isinstance(arguments, dict):
            raise RpcError(-32602, "tools/call arguments must be an object")

        try:
            structured = self._call_tool(name, arguments)
            return {
                "content": [{"type": "text", "text": json.dumps(structured, sort_keys=True)}],
                "structuredContent": structured,
                "isError": False,
            }
        except ValueError as exc:
            return {
                "content": [{"type": "text", "text": str(exc)}],
                "structuredContent": {"error": str(exc)},
                "isError": True,
            }

    def _call_tool(self, name: str, args: dict[str, Any]) -> dict[str, Any]:
        if name == "canvas_activate":
            session_id = _require_str(args, "session_id")
            mode_hint = args.get("mode_hint")
            if mode_hint is not None and not isinstance(mode_hint, str):
                raise ValueError("mode_hint must be string")
            return self.adapter.canvas_activate(session_id=session_id, mode_hint=mode_hint)

        if name == "canvas_render_text":
            return self.adapter.canvas_render_text(
                session_id=_require_str(args, "session_id"),
                title=_require_str(args, "title"),
                markdown_or_text=_require_str(args, "markdown_or_text"),
            )

        if name == "canvas_render_image":
            return self.adapter.canvas_render_image(
                session_id=_require_str(args, "session_id"),
                title=_require_str(args, "title"),
                path=_require_str(args, "path"),
            )

        if name == "canvas_render_pdf":
            page_obj = args.get("page", 0)
            if not isinstance(page_obj, int):
                raise ValueError("page must be integer")
            return self.adapter.canvas_render_pdf(
                session_id=_require_str(args, "session_id"),
                title=_require_str(args, "title"),
                path=_require_str(args, "path"),
                page=page_obj,
            )

        if name == "canvas_clear":
            reason = args.get("reason")
            if reason is not None and not isinstance(reason, str):
                raise ValueError("reason must be string")
            return self.adapter.canvas_clear(session_id=_require_str(args, "session_id"), reason=reason)

        if name == "canvas_status":
            return self.adapter.canvas_status(session_id=_require_str(args, "session_id"))

        if name == "canvas_history":
            session_id = _require_str(args, "session_id")
            limit = args.get("limit", 20)
            if not isinstance(limit, int):
                raise ValueError("limit must be integer")
            return self.adapter.canvas_history(session_id=session_id, limit=limit)

        raise ValueError(f"unknown tool: {name}")

    def _resources_list(self) -> dict[str, Any]:
        resources = [
            {
                "uri": "tabula://sessions",
                "name": "Tabula Sessions",
                "mimeType": "application/json",
                "description": "List of known canvas sessions.",
            }
        ]
        for session_id in self.adapter.list_sessions():
            resources.append(
                {
                    "uri": f"tabula://session/{session_id}",
                    "name": f"Session {session_id}",
                    "mimeType": "application/json",
                    "description": "Canvas session status.",
                }
            )
            resources.append(
                {
                    "uri": f"tabula://session/{session_id}/history",
                    "name": f"Session {session_id} History",
                    "mimeType": "application/json",
                    "description": "Recent session history.",
                }
            )
        return {"resources": resources}

    def _resources_read(self, params: dict[str, Any]) -> dict[str, Any]:
        uri = params.get("uri")
        if not isinstance(uri, str) or not uri.strip():
            raise RpcError(-32602, "resources/read requires non-empty uri")

        payload = self._resource_payload(uri)
        return {
            "contents": [
                {
                    "uri": uri,
                    "mimeType": "application/json",
                    "text": json.dumps(payload, sort_keys=True),
                }
            ]
        }

    def _resource_payload(self, uri: str) -> dict[str, Any]:
        parsed = urlparse(uri)
        if parsed.scheme != "tabula":
            raise RpcError(-32602, f"unsupported uri scheme: {parsed.scheme}")

        if parsed.netloc == "sessions":
            return {"sessions": self.adapter.list_sessions()}

        if parsed.netloc != "session":
            raise RpcError(-32602, f"unsupported uri: {uri}")

        parts = [part for part in parsed.path.split("/") if part]
        if not parts:
            raise RpcError(-32602, f"unsupported uri: {uri}")

        session_id = parts[0]
        if len(parts) == 1:
            return self.adapter.canvas_status(session_id=session_id)
        if len(parts) == 2 and parts[1] == "history":
            return self.adapter.canvas_history(session_id=session_id)
        raise RpcError(-32602, f"unsupported uri: {uri}")


def _require_str(payload: dict[str, Any], key: str) -> str:
    value = payload.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{key} must be non-empty string")
    return value


def run_mcp_stdio_server(
    *,
    project_dir: Path,
    headless: bool = False,
    poll_interval_ms: int = 250,
    start_canvas: bool = True,
) -> int:
    adapter = CanvasAdapter(
        project_dir=project_dir,
        headless=headless,
        start_canvas=start_canvas,
        poll_interval_ms=poll_interval_ms,
    )
    server = TabulaMcpServer(adapter)
    return server.run_forever()
