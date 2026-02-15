from __future__ import annotations

from pathlib import Path

import pytest

from tabula.canvas_adapter import CanvasAdapter


def test_given_headless_adapter_when_activate_then_status_reports_prompt_and_headless(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    activation = adapter.canvas_session_open(session_id="s1", mode_hint="discussion")
    status = adapter.canvas_status(session_id="s1")

    assert activation["active"] is True
    assert activation["headless"] is True
    assert activation["mode_hint"] == "discussion"
    assert activation["canvas_process_alive"] is False
    assert activation["canvas_launch_error"] is None
    assert status["mode"] == "prompt"
    assert status["headless"] is True
    assert status["marks_total"] == 0
    assert status["canvas_process_alive"] is False
    assert status["canvas_launch_error"] is None


def test_given_text_then_clear_when_rendering_then_mode_review_then_prompt_and_history_updates(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    text = adapter.canvas_artifact_show(session_id="s1", kind="text", title="draft", markdown_or_text="hello")
    status_review = adapter.canvas_status(session_id="s1")
    clear = adapter.canvas_artifact_show(session_id="s1", kind="clear", reason="done")
    status_prompt = adapter.canvas_status(session_id="s1")

    assert text["kind"] == "text_artifact"
    assert text["mode"] == "review"
    assert status_review["active_artifact_kind"] == "text_artifact"
    assert clear["kind"] == "clear_canvas"
    assert status_prompt["mode"] == "prompt"


def test_given_image_and_pdf_when_rendered_then_status_and_history_are_consistent(tmp_path: Path) -> None:
    image = tmp_path / "img.png"
    image.write_bytes(b"x")
    pdf = tmp_path / "doc.pdf"
    pdf.write_bytes(b"%PDF-1.4")

    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    img_result = adapter.canvas_artifact_show(session_id="s1", kind="image", title="img", path=str(image))
    pdf_result = adapter.canvas_artifact_show(session_id="s1", kind="pdf", title="doc", path=str(pdf), page=0)
    status = adapter.canvas_status(session_id="s1")

    assert img_result["kind"] == "image_artifact"
    assert pdf_result["kind"] == "pdf_artifact"
    assert pdf_result["artifact"]["page"] == 0
    assert status["mode"] == "review"
    assert status["active_artifact_kind"] == "pdf_artifact"

    mark = adapter.canvas_mark_set(
        session_id="s1",
        mark_id=None,
        artifact_id=status["active_artifact_id"],
        intent="draft",
        mark_type="highlight",
        target_kind="text_range",
        target={"line_start": 1, "line_end": 1, "quote": "x"},
        comment=None,
        author=None,
    )
    assert mark["mark"]["intent"] == "draft"
    marks = adapter.canvas_marks_list(session_id="s1")
    assert marks["count"] == 1


def test_given_missing_image_path_when_rendering_then_error(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_artifact_show(session_id="s1", kind="image", title="img", path=str(tmp_path / "missing.png"))
