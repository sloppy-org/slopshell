from __future__ import annotations

import pytest

pyside = pytest.importorskip("PySide6")
from PySide6.QtWidgets import QApplication

from tabula.events import ClearCanvasEvent, TextArtifactEvent
from tabula.window import CanvasWindow


def test_window_mode_switches_prompt_discussion_prompt() -> None:
    app = QApplication.instance() or QApplication([])
    window = CanvasWindow(poll_interval_ms=10_000)

    assert "prompt" in window.mode_label.text()

    window.apply_event(
        TextArtifactEvent(
            event_id="e1",
            ts="2026-02-11T12:00:00Z",
            kind="text_artifact",
            title="draft",
            text="hello",
        )
    )
    assert "discussion" in window.mode_label.text()

    window.apply_event(
        ClearCanvasEvent(
            event_id="e2",
            ts="2026-02-11T12:00:01Z",
            kind="clear_canvas",
            reason=None,
        )
    )
    assert "prompt" in window.mode_label.text()


def test_window_poll_once_uses_internal_queues() -> None:
    app = QApplication.instance() or QApplication([])
    window = CanvasWindow(poll_interval_ms=10_000)

    window._incoming.put(
        TextArtifactEvent(
            event_id="e1",
            ts="2026-02-11T12:00:00Z",
            kind="text_artifact",
            title="draft",
            text="hello",
        )
    )
    window._errors.put("line 2: invalid JSON")
    window.poll_once()

    assert "discussion" in window.mode_label.text()
    assert "line 2: invalid JSON" in window.status_label.text()
