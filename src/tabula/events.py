from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Literal

EventKind = Literal["text_artifact", "image_artifact", "pdf_artifact", "clear_canvas"]


class EventValidationError(ValueError):
    def __init__(self, message: str, *, line_no: int | None = None) -> None:
        self.line_no = line_no
        prefix = f"line {line_no}: " if line_no is not None else ""
        super().__init__(prefix + message)


@dataclass(frozen=True)
class BaseEvent:
    event_id: str
    ts: str
    kind: EventKind


@dataclass(frozen=True)
class TextArtifactEvent(BaseEvent):
    kind: Literal["text_artifact"]
    title: str
    text: str


@dataclass(frozen=True)
class ImageArtifactEvent(BaseEvent):
    kind: Literal["image_artifact"]
    title: str
    path: str


@dataclass(frozen=True)
class PdfArtifactEvent(BaseEvent):
    kind: Literal["pdf_artifact"]
    title: str
    path: str
    page: int = 0


@dataclass(frozen=True)
class ClearCanvasEvent(BaseEvent):
    kind: Literal["clear_canvas"]
    reason: str | None = None


CanvasEvent = TextArtifactEvent | ImageArtifactEvent | PdfArtifactEvent | ClearCanvasEvent


def event_schema() -> dict[str, object]:
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "title": "TabulaCanvasEvent",
        "oneOf": [
            {
                "type": "object",
                "required": ["event_id", "ts", "kind", "title", "text"],
                "properties": {
                    "event_id": {"type": "string", "minLength": 1},
                    "ts": {"type": "string", "format": "date-time"},
                    "kind": {"const": "text_artifact"},
                    "title": {"type": "string", "minLength": 1},
                    "text": {"type": "string"},
                },
                "additionalProperties": False,
            },
            {
                "type": "object",
                "required": ["event_id", "ts", "kind", "title", "path"],
                "properties": {
                    "event_id": {"type": "string", "minLength": 1},
                    "ts": {"type": "string", "format": "date-time"},
                    "kind": {"const": "image_artifact"},
                    "title": {"type": "string", "minLength": 1},
                    "path": {"type": "string", "minLength": 1},
                },
                "additionalProperties": False,
            },
            {
                "type": "object",
                "required": ["event_id", "ts", "kind", "title", "path"],
                "properties": {
                    "event_id": {"type": "string", "minLength": 1},
                    "ts": {"type": "string", "format": "date-time"},
                    "kind": {"const": "pdf_artifact"},
                    "title": {"type": "string", "minLength": 1},
                    "path": {"type": "string", "minLength": 1},
                    "page": {"type": "integer", "minimum": 0},
                },
                "additionalProperties": False,
            },
            {
                "type": "object",
                "required": ["event_id", "ts", "kind"],
                "properties": {
                    "event_id": {"type": "string", "minLength": 1},
                    "ts": {"type": "string", "format": "date-time"},
                    "kind": {"const": "clear_canvas"},
                    "reason": {"type": "string"},
                },
                "additionalProperties": False,
            },
        ],
    }


def _require_str(data: dict[str, object], key: str, *, line_no: int | None) -> str:
    value = data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise EventValidationError(f"'{key}' must be a non-empty string", line_no=line_no)
    return value


def _validate_iso_ts(ts: str, *, line_no: int | None) -> None:
    normalized = ts.replace("Z", "+00:00")
    try:
        datetime.fromisoformat(normalized)
    except ValueError as exc:
        raise EventValidationError("'ts' must be ISO-8601", line_no=line_no) from exc


def _validate_local_existing_path(path_value: str, *, base_dir: Path, line_no: int | None) -> str:
    if "://" in path_value:
        raise EventValidationError("path must be local (URLs are not allowed)", line_no=line_no)

    path = Path(path_value)
    if not path.is_absolute():
        path = (base_dir / path).resolve()

    if not path.exists():
        raise EventValidationError(f"path does not exist: {path}", line_no=line_no)

    return str(path)


def parse_event_payload(payload: dict[str, object], *, line_no: int | None = None, base_dir: Path | None = None) -> CanvasEvent:
    if not isinstance(payload, dict):
        raise EventValidationError("event payload must be a JSON object", line_no=line_no)

    event_id = _require_str(payload, "event_id", line_no=line_no)
    ts = _require_str(payload, "ts", line_no=line_no)
    kind = _require_str(payload, "kind", line_no=line_no)

    _validate_iso_ts(ts, line_no=line_no)

    base = base_dir or Path.cwd()

    if kind == "text_artifact":
        allowed = {"event_id", "ts", "kind", "title", "text"}
        if set(payload) != allowed:
            raise EventValidationError("text_artifact has invalid fields", line_no=line_no)
        title = _require_str(payload, "title", line_no=line_no)
        text = payload.get("text")
        if not isinstance(text, str):
            raise EventValidationError("'text' must be a string", line_no=line_no)
        return TextArtifactEvent(event_id=event_id, ts=ts, kind="text_artifact", title=title, text=text)

    if kind == "image_artifact":
        allowed = {"event_id", "ts", "kind", "title", "path"}
        if set(payload) != allowed:
            raise EventValidationError("image_artifact has invalid fields", line_no=line_no)
        title = _require_str(payload, "title", line_no=line_no)
        path_value = _require_str(payload, "path", line_no=line_no)
        path_value = _validate_local_existing_path(path_value, base_dir=base, line_no=line_no)
        return ImageArtifactEvent(event_id=event_id, ts=ts, kind="image_artifact", title=title, path=path_value)

    if kind == "pdf_artifact":
        allowed = {"event_id", "ts", "kind", "title", "path", "page"}
        if not set(payload).issubset(allowed):
            raise EventValidationError("pdf_artifact has invalid fields", line_no=line_no)
        if not {"event_id", "ts", "kind", "title", "path"}.issubset(set(payload)):
            raise EventValidationError("pdf_artifact missing required fields", line_no=line_no)
        title = _require_str(payload, "title", line_no=line_no)
        path_value = _require_str(payload, "path", line_no=line_no)
        path_value = _validate_local_existing_path(path_value, base_dir=base, line_no=line_no)
        page_obj = payload.get("page", 0)
        if not isinstance(page_obj, int) or page_obj < 0:
            raise EventValidationError("'page' must be integer >= 0", line_no=line_no)
        return PdfArtifactEvent(
            event_id=event_id,
            ts=ts,
            kind="pdf_artifact",
            title=title,
            path=path_value,
            page=page_obj,
        )

    if kind == "clear_canvas":
        allowed = {"event_id", "ts", "kind", "reason"}
        if not set(payload).issubset(allowed):
            raise EventValidationError("clear_canvas has invalid fields", line_no=line_no)
        reason = payload.get("reason")
        if reason is not None and not isinstance(reason, str):
            raise EventValidationError("'reason' must be a string", line_no=line_no)
        return ClearCanvasEvent(event_id=event_id, ts=ts, kind="clear_canvas", reason=reason)

    raise EventValidationError(f"unsupported kind: {kind}", line_no=line_no)


def parse_event_line(line: str, *, line_no: int | None = None, base_dir: Path | None = None) -> CanvasEvent:
    if not line.strip():
        raise EventValidationError("empty event line", line_no=line_no)

    try:
        payload = json.loads(line)
    except json.JSONDecodeError as exc:
        raise EventValidationError(f"invalid JSON: {exc.msg}", line_no=line_no) from exc
    if not isinstance(payload, dict):
        raise EventValidationError("event payload must be a JSON object", line_no=line_no)
    return parse_event_payload(payload, line_no=line_no, base_dir=base_dir)


def event_to_payload(event: CanvasEvent) -> dict[str, object]:
    payload: dict[str, object] = {
        "event_id": event.event_id,
        "ts": event.ts,
        "kind": event.kind,
    }
    if event.kind == "text_artifact":
        payload["title"] = event.title
        payload["text"] = event.text
    elif event.kind == "image_artifact":
        payload["title"] = event.title
        payload["path"] = event.path
    elif event.kind == "pdf_artifact":
        payload["title"] = event.title
        payload["path"] = event.path
        payload["page"] = event.page
    elif event.kind == "clear_canvas" and event.reason is not None:
        payload["reason"] = event.reason
    return payload
