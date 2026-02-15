from __future__ import annotations

import hashlib
import json
import os
import signal
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Literal, Mapping
from uuid import uuid4

from .events import CanvasEvent, event_to_payload, parse_event_payload
from .state import CanvasState, reduce_state

MarkIntent = Literal["ephemeral", "draft", "persistent"]
MarkType = Literal["highlight", "underline", "strikeout", "squiggly", "comment_point"]
TargetKind = Literal["text_range", "pdf_quads", "pdf_point"]


def has_display(env: Mapping[str, str] | None = None) -> bool:
    source = env or os.environ
    return bool(source.get("DISPLAY") or source.get("WAYLAND_DISPLAY"))


def launch_canvas_background(project_dir: Path, *, poll_interval_ms: int = 250) -> subprocess.Popen[bytes]:
    cmd = [
        sys.executable,
        "-m",
        "tabula",
        "canvas",
        "--poll-ms",
        str(poll_interval_ms),
    ]
    return subprocess.Popen(
        cmd,
        cwd=project_dir,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )


@dataclass
class Mark:
    mark_id: str
    session_id: str
    artifact_id: str
    intent: MarkIntent
    type: MarkType
    target_kind: TargetKind
    target: dict[str, object]
    comment: str | None = None
    author: str | None = None
    state: Literal["active", "resolved"] = "active"
    created_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"))
    updated_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"))

    def to_payload(self) -> dict[str, object]:
        return {
            "mark_id": self.mark_id,
            "session_id": self.session_id,
            "artifact_id": self.artifact_id,
            "intent": self.intent,
            "type": self.type,
            "target_kind": self.target_kind,
            "target": self.target,
            "comment": self.comment,
            "author": self.author,
            "state": self.state,
            "created_at": self.created_at,
            "updated_at": self.updated_at,
        }


@dataclass
class SessionRecord:
    state: CanvasState
    opened: bool
    history: list[CanvasEvent] = field(default_factory=list)
    marks: dict[str, Mark] = field(default_factory=dict)
    focused_mark_id: str | None = None
    draft_mark_by_artifact: dict[str, str] = field(default_factory=dict)


class CanvasAdapter:
    def __init__(
        self,
        *,
        project_dir: Path,
        start_canvas: bool = True,
        headless: bool = False,
        fresh_canvas: bool = False,
        poll_interval_ms: int = 250,
        env: Mapping[str, str] | None = None,
        on_event: Callable[[CanvasEvent], None] | None = None,
    ) -> None:
        self._project_dir = project_dir.resolve()
        self._start_canvas = start_canvas
        self._headless_override = headless
        self._fresh_canvas = fresh_canvas
        self._poll_interval_ms = poll_interval_ms
        self._env = env
        self._on_event = on_event

        self._lock = threading.RLock()
        self._sessions: dict[str, SessionRecord] = {}
        self._event_to_session: dict[str, str] = {}
        self._canvas_proc: subprocess.Popen[bytes] | None = None
        self._canvas_feedback_thread: threading.Thread | None = None
        self._canvas_feedback_pid: int | None = None
        self._canvas_launch_error: str | None = None

    def _effective_headless(self) -> bool:
        return self._headless_override or not has_display(self._env)

    def _canvas_pid_path(self) -> Path:
        return self._project_dir / ".tabula" / "canvas.pid"

    @staticmethod
    def _now_iso() -> str:
        return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

    @staticmethod
    def _is_tabula_canvas_pid(pid: int) -> bool:
        cmdline_path = Path("/proc") / str(pid) / "cmdline"
        try:
            raw = cmdline_path.read_bytes()
        except OSError:
            return False
        text = raw.decode("utf-8", errors="ignore")
        return ("tabula" in text) and ("canvas" in text)

    def _clear_canvas_pid_file(self) -> None:
        try:
            self._canvas_pid_path().unlink()
        except FileNotFoundError:
            return

    def _write_canvas_pid_file(self, pid: int) -> None:
        path = self._canvas_pid_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(str(pid), encoding="utf-8")

    def _terminate_stale_canvas_from_pid_file(self) -> None:
        path = self._canvas_pid_path()
        if not path.exists():
            return

        try:
            pid = int(path.read_text(encoding="utf-8").strip())
        except ValueError:
            self._clear_canvas_pid_file()
            return

        if pid <= 0 or not self._is_tabula_canvas_pid(pid):
            self._clear_canvas_pid_file()
            return

        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            self._clear_canvas_pid_file()
            return
        except PermissionError:
            return

        deadline = time.time() + 1.0
        while time.time() < deadline:
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                self._clear_canvas_pid_file()
                return
            except PermissionError:
                return
            time.sleep(0.05)

        try:
            os.kill(pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        self._clear_canvas_pid_file()

    def _ensure_canvas_process(self) -> None:
        if self._effective_headless() or not self._start_canvas:
            self._canvas_launch_error = None
            return
        if self._canvas_proc is not None and self._canvas_proc.poll() is None:
            return
        if self._fresh_canvas:
            self._terminate_stale_canvas_from_pid_file()
        self._canvas_proc = launch_canvas_background(self._project_dir, poll_interval_ms=self._poll_interval_ms)
        if self._canvas_proc.pid > 0:
            self._write_canvas_pid_file(self._canvas_proc.pid)

        for _ in range(5):
            if self._canvas_proc.poll() is not None:
                break
            time.sleep(0.05)
        if self._canvas_proc.poll() is None:
            self._canvas_launch_error = None
            self._start_canvas_feedback_reader()
            return

        exit_code = self._canvas_proc.poll()
        err_text = ""
        try:
            if self._canvas_proc.stderr is not None:
                raw_err = self._canvas_proc.stderr.read() or b""
                err_text = raw_err.decode("utf-8", errors="replace").strip()
        except OSError:
            err_text = ""

        detail = f"canvas process exited early with code {exit_code}"
        if err_text:
            detail += f": {err_text.splitlines()[-1]}"
        self._canvas_launch_error = detail
        self._canvas_proc = None
        self._clear_canvas_pid_file()

    def _canvas_process_alive(self) -> bool:
        return self._canvas_proc is not None and self._canvas_proc.poll() is None

    def _start_canvas_feedback_reader(self) -> None:
        proc = self._canvas_proc
        if proc is None or proc.stdout is None:
            return
        if self._canvas_feedback_pid == proc.pid and self._canvas_feedback_thread is not None:
            if self._canvas_feedback_thread.is_alive():
                return

        self._canvas_feedback_pid = proc.pid
        self._canvas_feedback_thread = threading.Thread(
            target=self._canvas_feedback_reader_loop,
            args=(proc,),
            daemon=True,
        )
        self._canvas_feedback_thread.start()

    def _canvas_feedback_reader_loop(self, proc: subprocess.Popen[bytes]) -> None:
        if proc.stdout is None:
            return
        while True:
            try:
                raw = proc.stdout.readline()
            except OSError:
                return
            if not raw:
                return
            line = raw.decode("utf-8", errors="replace").strip()
            if not line:
                continue
            self._handle_canvas_feedback_line(line)

    def _ensure_session(self, session_id: str) -> SessionRecord:
        if session_id not in self._sessions:
            self._sessions[session_id] = SessionRecord(state=CanvasState(), opened=False)
        return self._sessions[session_id]

    def _active_artifact_id(self, record: SessionRecord) -> str | None:
        active = record.state.active_event
        if active is None:
            return None
        return active.event_id

    @staticmethod
    def _base_payload(kind: str) -> dict[str, object]:
        return {
            "event_id": str(uuid4()),
            "ts": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "kind": kind,
        }

    def _emit_to_canvas(self, event: CanvasEvent) -> None:
        if self._on_event is not None:
            self._on_event(event)
        self._ensure_canvas_process()
        proc = self._canvas_proc
        if proc is None or proc.stdin is None:
            return

        try:
            line = json.dumps(event_to_payload(event), separators=(",", ":")) + "\n"
            proc.stdin.write(line.encode("utf-8"))
            proc.stdin.flush()
        except (BrokenPipeError, OSError):
            self._canvas_proc = None
            self._clear_canvas_pid_file()

    def _record_event(self, session_id: str, event: CanvasEvent) -> SessionRecord:
        with self._lock:
            record = self._ensure_session(session_id)
            record.state = reduce_state(record.state, event)
            record.history.append(event)
            self._event_to_session[event.event_id] = session_id
            if event.kind == "clear_canvas":
                record.focused_mark_id = None
        self._emit_to_canvas(event)
        return record

    def _get_event_for_artifact(self, record: SessionRecord, artifact_id: str) -> CanvasEvent | None:
        for event in reversed(record.history):
            if event.event_id == artifact_id:
                return event
        return None

    @staticmethod
    def _validate_mark_type(value: object) -> MarkType:
        allowed = {"highlight", "underline", "strikeout", "squiggly", "comment_point"}
        if not isinstance(value, str) or value not in allowed:
            raise ValueError("type must be one of highlight, underline, strikeout, squiggly, comment_point")
        return value  # type: ignore[return-value]

    @staticmethod
    def _validate_intent(value: object) -> MarkIntent:
        allowed = {"ephemeral", "draft", "persistent"}
        if not isinstance(value, str) or value not in allowed:
            raise ValueError("intent must be one of ephemeral, draft, persistent")
        return value  # type: ignore[return-value]

    @staticmethod
    def _validate_target_kind(value: object) -> TargetKind:
        allowed = {"text_range", "pdf_quads", "pdf_point"}
        if not isinstance(value, str) or value not in allowed:
            raise ValueError("target_kind must be one of text_range, pdf_quads, pdf_point")
        return value  # type: ignore[return-value]

    @staticmethod
    def _as_float(value: object, *, field_name: str) -> float:
        if isinstance(value, (int, float)):
            return float(value)
        raise ValueError(f"{field_name} must be numeric")

    def _validate_target(self, *, target_kind: TargetKind, target: object, mark_type: MarkType) -> dict[str, object]:
        if not isinstance(target, dict):
            raise ValueError("target must be an object")

        if target_kind == "text_range":
            normalized: dict[str, object] = {}
            start_offset = target.get("start_offset")
            end_offset = target.get("end_offset")
            if start_offset is not None:
                if not isinstance(start_offset, int) or start_offset < 0:
                    raise ValueError("target.start_offset must be integer >= 0")
                normalized["start_offset"] = start_offset
            if end_offset is not None:
                if not isinstance(end_offset, int) or end_offset < 0:
                    raise ValueError("target.end_offset must be integer >= 0")
                normalized["end_offset"] = end_offset
            if "start_offset" in normalized and "end_offset" in normalized:
                if int(normalized["end_offset"]) < int(normalized["start_offset"]):
                    raise ValueError("target.end_offset must be >= target.start_offset")

            line_start = target.get("line_start")
            line_end = target.get("line_end")
            if line_start is not None:
                if not isinstance(line_start, int) or line_start < 1:
                    raise ValueError("target.line_start must be integer >= 1")
                normalized["line_start"] = line_start
            if line_end is not None:
                if not isinstance(line_end, int) or line_end < 1:
                    raise ValueError("target.line_end must be integer >= 1")
                normalized["line_end"] = line_end
            if "line_start" in normalized and "line_end" in normalized:
                if int(normalized["line_end"]) < int(normalized["line_start"]):
                    raise ValueError("target.line_end must be >= target.line_start")

            quote = target.get("quote")
            if quote is not None:
                if not isinstance(quote, str):
                    raise ValueError("target.quote must be a string")
                normalized["quote"] = quote

            rects = target.get("rects")
            if rects is not None:
                if not isinstance(rects, list):
                    raise ValueError("target.rects must be a list")
                normalized_rects: list[list[float]] = []
                for idx, rect in enumerate(rects):
                    if not isinstance(rect, list) or len(rect) != 4:
                        raise ValueError(f"target.rects[{idx}] must be [x,y,w,h]")
                    normalized_rects.append([
                        self._as_float(rect[0], field_name=f"target.rects[{idx}][0]"),
                        self._as_float(rect[1], field_name=f"target.rects[{idx}][1]"),
                        self._as_float(rect[2], field_name=f"target.rects[{idx}][2]"),
                        self._as_float(rect[3], field_name=f"target.rects[{idx}][3]"),
                    ])
                normalized["rects"] = normalized_rects

            if not normalized:
                raise ValueError("target for text_range cannot be empty")
            return normalized

        if target_kind == "pdf_quads":
            if mark_type == "comment_point":
                raise ValueError("comment_point requires target_kind=pdf_point")

            page = target.get("page")
            if not isinstance(page, int) or page < 0:
                raise ValueError("target.page must be integer >= 0")
            quads = target.get("quads")
            if not isinstance(quads, list) or len(quads) < 8 or len(quads) % 8 != 0:
                raise ValueError("target.quads must be a numeric list with length multiple of 8")
            quad_vals = [self._as_float(v, field_name="target.quads[]") for v in quads]

            rect = target.get("rect")
            if rect is None:
                xs = quad_vals[0::2]
                ys = quad_vals[1::2]
                rect_vals = [min(xs), min(ys), max(xs), max(ys)]
            else:
                if not isinstance(rect, list) or len(rect) != 4:
                    raise ValueError("target.rect must be [x1,y1,x2,y2]")
                rect_vals = [self._as_float(v, field_name="target.rect[]") for v in rect]

            return {"page": page, "quads": quad_vals, "rect": rect_vals}

        if target_kind == "pdf_point":
            if mark_type != "comment_point":
                raise ValueError("pdf_point target is only valid for type=comment_point")
            page = target.get("page")
            if not isinstance(page, int) or page < 0:
                raise ValueError("target.page must be integer >= 0")
            x = self._as_float(target.get("x"), field_name="target.x")
            y = self._as_float(target.get("y"), field_name="target.y")
            rect = target.get("rect")
            if rect is None:
                rect_vals = [x - 8.0, y - 8.0, x + 8.0, y + 8.0]
            else:
                if not isinstance(rect, list) or len(rect) != 4:
                    raise ValueError("target.rect must be [x1,y1,x2,y2]")
                rect_vals = [self._as_float(v, field_name="target.rect[]") for v in rect]
            return {"page": page, "x": x, "y": y, "rect": rect_vals}

        raise ValueError(f"unsupported target_kind: {target_kind}")

    def _set_mark_internal(
        self,
        *,
        session_id: str,
        mark_id: str | None,
        artifact_id: str | None,
        intent: MarkIntent,
        mark_type: MarkType,
        target_kind: TargetKind,
        target: dict[str, object],
        comment: str | None,
        author: str | None,
        state: Literal["active", "resolved"] = "active",
    ) -> Mark:
        record = self._ensure_session(session_id)
        if artifact_id is None:
            artifact_id = self._active_artifact_id(record)
        if artifact_id is None:
            raise ValueError("artifact_id is required when there is no active artifact")

        artifact_event = self._get_event_for_artifact(record, artifact_id)
        if artifact_event is None:
            raise ValueError("unknown artifact_id")

        existing = record.marks.get(mark_id) if mark_id else None
        now = self._now_iso()
        if existing is None:
            resolved_mark_id = mark_id or str(uuid4())
            mark = Mark(
                mark_id=resolved_mark_id,
                session_id=session_id,
                artifact_id=artifact_id,
                intent=intent,
                type=mark_type,
                target_kind=target_kind,
                target=target,
                comment=comment,
                author=author,
                state=state,
                created_at=now,
                updated_at=now,
            )
        else:
            mark = Mark(
                mark_id=existing.mark_id,
                session_id=session_id,
                artifact_id=artifact_id,
                intent=intent,
                type=mark_type,
                target_kind=target_kind,
                target=target,
                comment=comment,
                author=author,
                state=state,
                created_at=existing.created_at,
                updated_at=now,
            )

        record.marks[mark.mark_id] = mark
        record.focused_mark_id = mark.mark_id
        if mark.intent == "draft":
            record.draft_mark_by_artifact[mark.artifact_id] = mark.mark_id
        return mark

    def _artifact_sidecar_path(self, event: CanvasEvent) -> Path:
        artifact_tag = "unknown"
        if event.kind == "text_artifact":
            artifact_tag = f"text:{event.event_id}"
        elif event.kind == "image_artifact":
            artifact_tag = f"image:{event.path}"
        elif event.kind == "pdf_artifact":
            artifact_tag = f"pdf:{event.path}"
        digest = hashlib.sha256(artifact_tag.encode("utf-8")).hexdigest()[:16]
        out_dir = self._project_dir / ".tabula" / "artifacts" / "annotations"
        out_dir.mkdir(parents=True, exist_ok=True)
        return out_dir / f"{digest}.annotations.json"

    @staticmethod
    def _pdf_date_now() -> str:
        now = datetime.now(timezone.utc)
        return now.strftime("D:%Y%m%d%H%M%S+00'00'")

    def _write_pdf_annotations(self, *, pdf_path: Path, marks: list[Mark]) -> int:
        from pypdf import PdfReader, PdfWriter
        from pypdf.generic import ArrayObject, DictionaryObject, FloatObject, NameObject, TextStringObject

        reader = PdfReader(str(pdf_path))
        writer = PdfWriter()
        writer.clone_document_from_reader(reader)

        added = 0
        for mark in marks:
            if mark.target_kind not in {"pdf_quads", "pdf_point"}:
                continue
            target = mark.target
            page_idx = target.get("page")
            if not isinstance(page_idx, int) or page_idx < 0 or page_idx >= len(writer.pages):
                continue
            page = writer.pages[page_idx]

            annot = DictionaryObject()
            annot[NameObject("/Type")] = NameObject("/Annot")
            annot[NameObject("/M")] = TextStringObject(self._pdf_date_now())
            annot[NameObject("/NM")] = TextStringObject(mark.mark_id)

            if mark.target_kind == "pdf_quads":
                subtype = {
                    "highlight": "/Highlight",
                    "underline": "/Underline",
                    "strikeout": "/StrikeOut",
                    "squiggly": "/Squiggly",
                }.get(mark.type)
                if subtype is None:
                    continue
                rect = target.get("rect")
                quads = target.get("quads")
                if not isinstance(rect, list) or len(rect) != 4:
                    continue
                if not isinstance(quads, list) or len(quads) < 8 or len(quads) % 8 != 0:
                    continue
                annot[NameObject("/Subtype")] = NameObject(subtype)
                annot[NameObject("/Rect")] = ArrayObject([FloatObject(float(v)) for v in rect])
                annot[NameObject("/QuadPoints")] = ArrayObject([FloatObject(float(v)) for v in quads])
                if mark.comment:
                    annot[NameObject("/Contents")] = TextStringObject(mark.comment)
                if mark.author:
                    annot[NameObject("/T")] = TextStringObject(mark.author)
            else:
                rect = target.get("rect")
                if not isinstance(rect, list) or len(rect) != 4:
                    continue
                annot[NameObject("/Subtype")] = NameObject("/Text")
                annot[NameObject("/Rect")] = ArrayObject([FloatObject(float(v)) for v in rect])
                annot[NameObject("/Name")] = NameObject("/Comment")
                annot[NameObject("/Contents")] = TextStringObject(mark.comment or "")
                if mark.author:
                    annot[NameObject("/T")] = TextStringObject(mark.author)

            annot_ref = writer._add_object(annot)
            annots_obj = page.get("/Annots")
            if annots_obj is None:
                annots = ArrayObject()
                page[NameObject("/Annots")] = annots
            else:
                annots = annots_obj.get_object() if hasattr(annots_obj, "get_object") else annots_obj
            annots.append(annot_ref)
            added += 1

        with pdf_path.open("wb") as f:
            writer.write(f)

        return added

    def _write_sidecar(self, *, session_id: str, event: CanvasEvent, marks: list[Mark]) -> Path:
        path = self._artifact_sidecar_path(event)
        payload = {
            "version": 1,
            "updated_at": self._now_iso(),
            "session_id": session_id,
            "artifact": event_to_payload(event),
            "marks": [m.to_payload() for m in marks],
        }
        path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        return path

    def _handle_text_selection_feedback(self, payload: dict[str, object]) -> None:
        event_id = payload.get("event_id")
        if not isinstance(event_id, str) or not event_id.strip():
            return

        line_start = payload.get("line_start")
        line_end = payload.get("line_end")
        text = payload.get("text")
        start_offset = payload.get("start_offset")
        end_offset = payload.get("end_offset")
        mark_type_raw = payload.get("mark_type", "highlight")
        comment_raw = payload.get("comment")

        if not isinstance(text, str) or text == "":
            # Blur-driven null-selection feedback is intentionally ignored so focus switches
            # do not destroy the active draft mark.
            return

        with self._lock:
            session_id = self._event_to_session.get(event_id)
            if session_id is None:
                return
            record = self._ensure_session(session_id)
            active = record.state.active_event
            if active is None or active.event_id != event_id or active.kind != "text_artifact":
                return

            target: dict[str, object] = {"quote": text}
            if isinstance(line_start, int) and line_start >= 1:
                target["line_start"] = line_start
            if isinstance(line_end, int) and line_end >= int(target.get("line_start", 1)):
                target["line_end"] = line_end
            if isinstance(start_offset, int) and start_offset >= 0:
                target["start_offset"] = start_offset
            if isinstance(end_offset, int) and end_offset >= int(target.get("start_offset", 0)):
                target["end_offset"] = end_offset
            rects = payload.get("rects")
            if isinstance(rects, list):
                target["rects"] = rects

            try:
                mark_type = self._validate_mark_type(mark_type_raw)
            except ValueError:
                mark_type = "highlight"
            comment = comment_raw if isinstance(comment_raw, str) and comment_raw.strip() else None

            draft_mark_id = record.draft_mark_by_artifact.get(event_id)
            mark = self._set_mark_internal(
                session_id=session_id,
                mark_id=draft_mark_id,
                artifact_id=event_id,
                intent="draft",
                mark_type=mark_type,
                target_kind="text_range",
                target=self._validate_target(target_kind="text_range", target=target, mark_type=mark_type),
                comment=comment,
                author=None,
            )
            record.draft_mark_by_artifact[event_id] = mark.mark_id

    def _handle_canvas_feedback_line(self, line: str) -> None:
        try:
            payload = json.loads(line)
        except json.JSONDecodeError:
            return
        if not isinstance(payload, dict):
            return
        kind = payload.get("kind")

        if kind == "text_selection":
            self._handle_text_selection_feedback(payload)
            return

        if kind == "mark_set":
            session_id = payload.get("session_id")
            if not isinstance(session_id, str) or not session_id.strip():
                return
            try:
                self.canvas_mark_set(
                    session_id=session_id,
                    mark_id=payload.get("mark_id") if isinstance(payload.get("mark_id"), str) else None,
                    artifact_id=payload.get("artifact_id") if isinstance(payload.get("artifact_id"), str) else None,
                    intent=payload.get("intent", "draft"),
                    mark_type=payload.get("type", "highlight"),
                    target_kind=payload.get("target_kind", "text_range"),
                    target=payload.get("target", {}),
                    comment=payload.get("comment") if isinstance(payload.get("comment"), str) else None,
                    author=payload.get("author") if isinstance(payload.get("author"), str) else None,
                )
            except ValueError:
                return
            return

        if kind == "mark_delete":
            session_id = payload.get("session_id")
            mark_id = payload.get("mark_id")
            if not isinstance(session_id, str) or not isinstance(mark_id, str):
                return
            try:
                self.canvas_mark_delete(session_id=session_id, mark_id=mark_id)
            except ValueError:
                return
            return

        if kind == "mark_commit":
            session_id = payload.get("session_id")
            if not isinstance(session_id, str) or not session_id.strip():
                return
            include_draft = payload.get("include_draft", True)
            if not isinstance(include_draft, bool):
                include_draft = True
            artifact_id = payload.get("artifact_id") if isinstance(payload.get("artifact_id"), str) else None
            try:
                self.canvas_commit(
                    session_id=session_id,
                    artifact_id=artifact_id,
                    include_draft=include_draft,
                )
            except ValueError:
                return
            return

        if kind == "mark_clear_draft":
            session_id = payload.get("session_id")
            artifact_id = payload.get("artifact_id")
            if not isinstance(session_id, str) or not session_id.strip():
                return
            if artifact_id is not None and not isinstance(artifact_id, str):
                return
            with self._lock:
                record = self._ensure_session(session_id)
                resolved_artifact = artifact_id or self._active_artifact_id(record)
                if resolved_artifact is None:
                    return
                draft_id = record.draft_mark_by_artifact.get(resolved_artifact)
                if draft_id is None:
                    return
                record.marks.pop(draft_id, None)
                del record.draft_mark_by_artifact[resolved_artifact]
                if record.focused_mark_id == draft_id:
                    record.focused_mark_id = None

    def handle_feedback(self, line: str) -> None:
        self._handle_canvas_feedback_line(line)

    def list_sessions(self) -> list[str]:
        with self._lock:
            return sorted(self._sessions.keys())

    def canvas_session_open(self, *, session_id: str, mode_hint: str | None = None) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        with self._lock:
            record = self._ensure_session(session_id)
            record.opened = True
            self._ensure_canvas_process()
            active = record.state.active_event
            return {
                "active": True,
                "mode": record.state.mode,
                "mode_hint": mode_hint,
                "active_artifact_id": active.event_id if active is not None else None,
                "active_artifact_kind": active.kind if active is not None else None,
                "marks_total": len(record.marks),
                "focused_mark_id": record.focused_mark_id,
                "headless": self._effective_headless(),
                "canvas_process_alive": self._canvas_process_alive(),
                "canvas_launch_error": self._canvas_launch_error,
            }

    def canvas_artifact_show(
        self,
        *,
        session_id: str,
        kind: str,
        title: str | None = None,
        markdown_or_text: str | None = None,
        path: str | None = None,
        page: int = 0,
        reason: str | None = None,
    ) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")

        with self._lock:
            self.canvas_session_open(session_id=session_id)
            if kind == "text":
                if not isinstance(title, str) or not title.strip():
                    raise ValueError("title must be non-empty for text")
                if not isinstance(markdown_or_text, str):
                    raise ValueError("markdown_or_text must be string for text")
                payload = self._base_payload("text_artifact")
                payload.update({"title": title, "text": markdown_or_text})
                event = parse_event_payload(payload, base_dir=self._project_dir)
            elif kind == "image":
                if not isinstance(title, str) or not title.strip():
                    raise ValueError("title must be non-empty for image")
                if not isinstance(path, str) or not path.strip():
                    raise ValueError("path must be non-empty for image")
                payload = self._base_payload("image_artifact")
                payload.update({"title": title, "path": path})
                event = parse_event_payload(payload, base_dir=self._project_dir)
            elif kind == "pdf":
                if not isinstance(title, str) or not title.strip():
                    raise ValueError("title must be non-empty for pdf")
                if not isinstance(path, str) or not path.strip():
                    raise ValueError("path must be non-empty for pdf")
                if not isinstance(page, int) or page < 0:
                    raise ValueError("page must be integer >= 0")
                payload = self._base_payload("pdf_artifact")
                payload.update({"title": title, "path": path, "page": page})
                event = parse_event_payload(payload, base_dir=self._project_dir)
            elif kind == "clear":
                payload = self._base_payload("clear_canvas")
                if reason is not None:
                    if not isinstance(reason, str):
                        raise ValueError("reason must be string")
                    payload["reason"] = reason
                event = parse_event_payload(payload, base_dir=self._project_dir)
            else:
                raise ValueError("kind must be one of text, image, pdf, clear")

            record = self._record_event(session_id, event)
            return {
                "artifact_id": event.event_id,
                "kind": event.kind,
                "mode": record.state.mode,
                "artifact": event_to_payload(event),
            }

    def canvas_mark_set(
        self,
        *,
        session_id: str,
        mark_id: str | None,
        artifact_id: str | None,
        intent: object,
        mark_type: object,
        target_kind: object,
        target: object,
        comment: str | None,
        author: str | None,
    ) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        if mark_id is not None and (not isinstance(mark_id, str) or not mark_id.strip()):
            raise ValueError("mark_id must be non-empty string when provided")
        if artifact_id is not None and (not isinstance(artifact_id, str) or not artifact_id.strip()):
            raise ValueError("artifact_id must be non-empty string when provided")
        if comment is not None and not isinstance(comment, str):
            raise ValueError("comment must be string")
        if author is not None and not isinstance(author, str):
            raise ValueError("author must be string")

        parsed_intent = self._validate_intent(intent)
        parsed_type = self._validate_mark_type(mark_type)
        parsed_target_kind = self._validate_target_kind(target_kind)
        parsed_target = self._validate_target(target_kind=parsed_target_kind, target=target, mark_type=parsed_type)

        with self._lock:
            record = self._ensure_session(session_id)
            mark = self._set_mark_internal(
                session_id=session_id,
                mark_id=mark_id,
                artifact_id=artifact_id,
                intent=parsed_intent,
                mark_type=parsed_type,
                target_kind=parsed_target_kind,
                target=parsed_target,
                comment=comment,
                author=author,
            )
            return {
                "mark": mark.to_payload(),
                "marks_total": len(record.marks),
                "focused_mark_id": record.focused_mark_id,
            }

    def canvas_mark_delete(self, *, session_id: str, mark_id: str) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        if not isinstance(mark_id, str) or not mark_id.strip():
            raise ValueError("mark_id must be non-empty string")

        with self._lock:
            record = self._ensure_session(session_id)
            mark = record.marks.pop(mark_id, None)
            if mark is None:
                raise ValueError("mark not found")
            if record.focused_mark_id == mark_id:
                record.focused_mark_id = None
            if record.draft_mark_by_artifact.get(mark.artifact_id) == mark_id:
                del record.draft_mark_by_artifact[mark.artifact_id]
            return {"deleted": True, "mark_id": mark_id, "marks_total": len(record.marks)}

    def canvas_marks_list(
        self,
        *,
        session_id: str,
        artifact_id: str | None = None,
        intent: str | None = None,
        limit: int = 200,
    ) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        if artifact_id is not None and (not isinstance(artifact_id, str) or not artifact_id.strip()):
            raise ValueError("artifact_id must be non-empty string")
        if intent is not None:
            self._validate_intent(intent)
        if not isinstance(limit, int) or limit <= 0:
            raise ValueError("limit must be integer > 0")

        with self._lock:
            record = self._ensure_session(session_id)
            marks = list(record.marks.values())
            marks.sort(key=lambda m: m.created_at)
            if artifact_id is not None:
                marks = [m for m in marks if m.artifact_id == artifact_id]
            if intent is not None:
                marks = [m for m in marks if m.intent == intent]
            marks = marks[-limit:]
            return {
                "session_id": session_id,
                "count": len(marks),
                "focused_mark_id": record.focused_mark_id,
                "marks": [m.to_payload() for m in marks],
            }

    def canvas_mark_focus(self, *, session_id: str, mark_id: str | None) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        if mark_id is not None and (not isinstance(mark_id, str) or not mark_id.strip()):
            raise ValueError("mark_id must be non-empty string")

        with self._lock:
            record = self._ensure_session(session_id)
            if mark_id is not None and mark_id not in record.marks:
                raise ValueError("mark not found")
            record.focused_mark_id = mark_id
            focused = record.marks.get(mark_id) if mark_id else None
            return {
                "focused_mark_id": mark_id,
                "focused_mark": focused.to_payload() if focused else None,
            }

    def canvas_commit(
        self,
        *,
        session_id: str,
        artifact_id: str | None = None,
        include_draft: bool = True,
    ) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        if artifact_id is not None and (not isinstance(artifact_id, str) or not artifact_id.strip()):
            raise ValueError("artifact_id must be non-empty string")
        if not isinstance(include_draft, bool):
            raise ValueError("include_draft must be boolean")

        with self._lock:
            record = self._ensure_session(session_id)
            resolved_artifact_id = artifact_id or self._active_artifact_id(record)
            if resolved_artifact_id is None:
                raise ValueError("artifact_id is required when no active artifact exists")

            artifact_event = self._get_event_for_artifact(record, resolved_artifact_id)
            if artifact_event is None:
                raise ValueError("unknown artifact_id")

            converted = 0
            for mid, mark in list(record.marks.items()):
                if mark.artifact_id != resolved_artifact_id:
                    continue
                if include_draft and mark.intent == "draft":
                    record.marks[mid] = Mark(
                        mark_id=mark.mark_id,
                        session_id=mark.session_id,
                        artifact_id=mark.artifact_id,
                        intent="persistent",
                        type=mark.type,
                        target_kind=mark.target_kind,
                        target=mark.target,
                        comment=mark.comment,
                        author=mark.author,
                        state=mark.state,
                        created_at=mark.created_at,
                        updated_at=self._now_iso(),
                    )
                    converted += 1

            persistent_marks = [
                m for m in record.marks.values()
                if m.artifact_id == resolved_artifact_id and m.intent == "persistent"
            ]
            persistent_marks.sort(key=lambda m: m.created_at)
            sidecar_path = self._write_sidecar(session_id=session_id, event=artifact_event, marks=persistent_marks)

            pdf_annotations_written = 0
            if artifact_event.kind == "pdf_artifact":
                try:
                    pdf_annotations_written = self._write_pdf_annotations(
                        pdf_path=Path(artifact_event.path),
                        marks=persistent_marks,
                    )
                except ModuleNotFoundError as exc:
                    raise ValueError(
                        "pypdf is required for PDF annotation commit; install project dependencies."
                    ) from exc

            return {
                "session_id": session_id,
                "artifact_id": resolved_artifact_id,
                "converted_to_persistent": converted,
                "persistent_count": len(persistent_marks),
                "sidecar_path": str(sidecar_path),
                "pdf_annotations_written": pdf_annotations_written,
            }

    def canvas_status(self, *, session_id: str) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")

        with self._lock:
            record = self._ensure_session(session_id)
            active_event = record.state.active_event
            active_id = active_event.event_id if active_event is not None else None
            marks_for_active = 0
            if active_id is not None:
                marks_for_active = len([m for m in record.marks.values() if m.artifact_id == active_id])
            focused = record.marks.get(record.focused_mark_id) if record.focused_mark_id else None
            return {
                "mode": record.state.mode,
                "active": record.opened,
                "active_artifact_id": active_id,
                "active_artifact_kind": active_event.kind if active_event is not None else None,
                "active_artifact": event_to_payload(active_event) if active_event is not None else None,
                "history_size": len(record.history),
                "marks_total": len(record.marks),
                "marks_for_active": marks_for_active,
                "focused_mark_id": record.focused_mark_id,
                "focused_mark": focused.to_payload() if focused else None,
                "headless": self._effective_headless(),
                "canvas_process_alive": self._canvas_process_alive(),
                "canvas_launch_error": self._canvas_launch_error,
            }

    # Compatibility shims for non-updated call sites. These are intentionally
    # not advertised in MCP tools/list and can be removed in a later cleanup.
    def canvas_activate(self, *, session_id: str, mode_hint: str | None = None) -> dict[str, object]:
        return self.canvas_session_open(session_id=session_id, mode_hint=mode_hint)

    def canvas_render_text(self, *, session_id: str, title: str, markdown_or_text: str) -> dict[str, object]:
        return self.canvas_artifact_show(
            session_id=session_id,
            kind="text",
            title=title,
            markdown_or_text=markdown_or_text,
        )

    def canvas_render_image(self, *, session_id: str, title: str, path: str) -> dict[str, object]:
        return self.canvas_artifact_show(
            session_id=session_id,
            kind="image",
            title=title,
            path=path,
        )

    def canvas_render_pdf(self, *, session_id: str, title: str, path: str, page: int = 0) -> dict[str, object]:
        return self.canvas_artifact_show(
            session_id=session_id,
            kind="pdf",
            title=title,
            path=path,
            page=page,
        )

    def canvas_clear(self, *, session_id: str, reason: str | None = None) -> dict[str, object]:
        return self.canvas_artifact_show(
            session_id=session_id,
            kind="clear",
            reason=reason,
        )

    def canvas_history(self, *, session_id: str, limit: int = 20) -> dict[str, object]:
        if not isinstance(limit, int) or limit <= 0:
            raise ValueError("limit must be integer > 0")
        with self._lock:
            record = self._ensure_session(session_id)
            selected = record.history[-limit:]
            return {
                "session_id": session_id,
                "count": len(selected),
                "events": [event_to_payload(event) for event in selected],
            }

    def canvas_selection(self, *, session_id: str) -> dict[str, object]:
        with self._lock:
            record = self._ensure_session(session_id)
            active_id = self._active_artifact_id(record)
            selected_mark: Mark | None = None
            if record.focused_mark_id:
                candidate = record.marks.get(record.focused_mark_id)
                if candidate is not None and candidate.target_kind == "text_range":
                    selected_mark = candidate
            if selected_mark is None and active_id is not None:
                draft_id = record.draft_mark_by_artifact.get(active_id)
                if draft_id:
                    candidate = record.marks.get(draft_id)
                    if candidate is not None and candidate.target_kind == "text_range":
                        selected_mark = candidate

            if selected_mark is None:
                selection = {
                    "has_selection": False,
                    "event_id": None,
                    "line_start": None,
                    "line_end": None,
                    "text": None,
                }
            else:
                target = selected_mark.target
                selection = {
                    "has_selection": True,
                    "event_id": selected_mark.artifact_id,
                    "line_start": target.get("line_start"),
                    "line_end": target.get("line_end"),
                    "text": target.get("quote"),
                }
            return {
                "session_id": session_id,
                "mode": record.state.mode,
                "active_event_id": active_id,
                "selection": selection,
            }
