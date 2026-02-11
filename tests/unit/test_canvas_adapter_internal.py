from __future__ import annotations

from pathlib import Path

import pytest

import tabula.canvas_adapter as adapter_module
from tabula.canvas_adapter import CanvasAdapter, has_display, launch_canvas_background


def test_has_display_detects_display_or_wayland() -> None:
    assert has_display({"DISPLAY": ":0"}) is True
    assert has_display({"WAYLAND_DISPLAY": "wayland-0"}) is True
    assert has_display({}) is False


def test_launch_canvas_background_uses_expected_command(monkeypatch, tmp_path: Path) -> None:
    seen: dict[str, object] = {}

    class FakeProc:
        stdin = None

        def poll(self):
            return 0

    def fake_popen(cmd, cwd=None, stdin=None):
        seen["cmd"] = cmd
        seen["cwd"] = cwd
        seen["stdin"] = stdin
        return FakeProc()

    monkeypatch.setattr(adapter_module.subprocess, "Popen", fake_popen)
    proc = launch_canvas_background(tmp_path, poll_interval_ms=321)
    assert proc is not None
    assert seen["cwd"] == tmp_path
    cmd = seen["cmd"]
    assert cmd[1:4] == ["-m", "tabula", "canvas"]
    assert "--poll-ms" in cmd


def test_canvas_process_launch_and_reuse(monkeypatch, tmp_path: Path) -> None:
    launched = {"count": 0}

    class FakeProc:
        stdin = None

        def __init__(self, poll_value: int | None):
            self._poll_value = poll_value

        def poll(self):
            return self._poll_value

    def fake_launch(project_dir: Path, *, poll_interval_ms: int = 250):
        launched["count"] += 1
        return FakeProc(None)

    monkeypatch.setattr(adapter_module, "launch_canvas_background", fake_launch)
    adapter = CanvasAdapter(
        project_dir=tmp_path,
        headless=False,
        start_canvas=True,
        env={"DISPLAY": ":0"},
    )

    adapter.canvas_activate(session_id="s1")
    adapter.canvas_activate(session_id="s1")
    assert launched["count"] == 1

    adapter._canvas_proc = FakeProc(1)  # type: ignore[attr-defined]
    adapter.canvas_activate(session_id="s1")
    assert launched["count"] == 2


def test_canvas_activate_requires_nonempty_session_id(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_activate(session_id=" ")


def test_render_validates_inputs(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_render_text(session_id="s1", title=" ", markdown_or_text="x")
    with pytest.raises(ValueError):
        adapter.canvas_render_text(session_id="s1", title="t", markdown_or_text=1)  # type: ignore[arg-type]
    with pytest.raises(ValueError):
        adapter.canvas_render_image(session_id="s1", title="", path="x")
    with pytest.raises(ValueError):
        adapter.canvas_render_image(session_id="s1", title="img", path=" ")
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="", path="x.pdf", page=0)
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="doc", path="", page=0)
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="doc", path="x.pdf", page=-1)


def test_history_limit_must_be_positive(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_history(session_id="s1", limit=0)
