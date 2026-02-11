from __future__ import annotations

from pathlib import Path

import pytest

from tabula.canvas_adapter import CanvasAdapter


def test_given_headless_adapter_when_activate_then_status_reports_prompt_and_headless(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    activation = adapter.canvas_activate(session_id="s1", mode_hint="discussion")
    status = adapter.canvas_status(session_id="s1")

    assert activation["active"] is True
    assert activation["headless"] is True
    assert status["mode"] == "prompt"
    assert status["headless"] is True


def test_given_text_then_clear_when_rendering_then_mode_discussion_then_prompt_and_history_updates(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    text = adapter.canvas_render_text(session_id="s1", title="draft", markdown_or_text="hello")
    clear = adapter.canvas_clear(session_id="s1", reason="done")
    history = adapter.canvas_history(session_id="s1", limit=10)

    assert text["kind"] == "text_artifact"
    assert text["mode"] == "discussion"
    assert clear["cleared"] is True
    assert clear["mode"] == "prompt"
    assert [row["kind"] for row in history["events"]] == ["text_artifact", "clear_canvas"]


def test_given_image_and_pdf_when_rendered_then_status_and_history_are_consistent(tmp_path: Path) -> None:
    image = tmp_path / "img.png"
    image.write_bytes(b"x")
    pdf = tmp_path / "doc.pdf"
    pdf.write_bytes(b"%PDF-1.4")

    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)

    img_result = adapter.canvas_render_image(session_id="s1", title="img", path=str(image))
    pdf_result = adapter.canvas_render_pdf(session_id="s1", title="doc", path=str(pdf), page=0)
    status = adapter.canvas_status(session_id="s1")
    history = adapter.canvas_history(session_id="s1", limit=10)

    assert img_result["kind"] == "image_artifact"
    assert pdf_result["kind"] == "pdf_artifact"
    assert pdf_result["page"] == 0
    assert status["mode"] == "discussion"
    assert status["active_kind"] == "pdf_artifact"
    assert [row["kind"] for row in history["events"]] == ["image_artifact", "pdf_artifact"]


def test_given_missing_image_path_when_rendering_then_error(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_render_image(session_id="s1", title="img", path=str(tmp_path / "missing.png"))
