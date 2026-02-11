from __future__ import annotations

import importlib
import sys
import types

from tabula.events import ClearCanvasEvent, ImageArtifactEvent, PdfArtifactEvent, TextArtifactEvent


def _import_window_with_fake_pyside(monkeypatch, *, has_qtpdf: bool):
    class _Signal:
        def __init__(self) -> None:
            self._callback = None

        def connect(self, callback) -> None:
            self._callback = callback

        def emit(self) -> None:
            if self._callback is not None:
                self._callback()

    class FakeQTimer:
        def __init__(self, _parent=None) -> None:
            self.timeout = _Signal()
            self.started_ms = None

        def start(self, ms: int) -> None:
            self.started_ms = ms

    class FakeQPixmap:
        null_paths: set[str] = set()

        def __init__(self, path: str) -> None:
            self.path = str(path)

        def isNull(self) -> bool:
            return self.path in type(self).null_paths

    class FakeQApplication:
        _instance = None
        exec_return = 42

        def __init__(self, _argv) -> None:
            type(self)._instance = self
            self.exec_calls = 0

        @classmethod
        def instance(cls):
            return cls._instance

        def exec(self) -> int:
            self.exec_calls += 1
            return type(self).exec_return

    class FakeWidget:
        def __init__(self, *args, **kwargs) -> None:
            self.object_name = None
            self.alignment = None
            self.read_only = False
            self._text = ""
            self.current_document = None
            self.pixmap = None

        def setObjectName(self, name: str) -> None:
            self.object_name = name

        def setAlignment(self, alignment) -> None:
            self.alignment = alignment

        def setScaledContents(self, _enabled: bool) -> None:
            return None

        def setText(self, text: str) -> None:
            self._text = text

        def text(self) -> str:
            return self._text

        def setReadOnly(self, enabled: bool) -> None:
            self.read_only = enabled

        def setPlainText(self, text: str) -> None:
            self._text = text

        def setPixmap(self, pixmap) -> None:
            self.pixmap = pixmap

        def setDocument(self, document) -> None:
            self.current_document = document

    class FakeQLabel(FakeWidget):
        def __init__(self, text: str = "") -> None:
            super().__init__()
            self._text = text

    class FakeQPlainTextEdit(FakeWidget):
        pass

    class FakeQStackedWidget(FakeWidget):
        def __init__(self) -> None:
            super().__init__()
            self.widgets: list[object] = []
            self.current_widget = None

        def addWidget(self, widget) -> None:
            self.widgets.append(widget)

        def setCurrentWidget(self, widget) -> None:
            self.current_widget = widget

    class FakeQVBoxLayout:
        def __init__(self, _root) -> None:
            self.widgets: list[object] = []

        def addWidget(self, widget, *_args) -> None:
            self.widgets.append(widget)

    class FakeQMainWindow(FakeWidget):
        show_calls = 0

        def __init__(self) -> None:
            super().__init__()
            self.title = ""
            self.size = (0, 0)
            self.central_widget = None

        def setWindowTitle(self, title: str) -> None:
            self.title = title

        def resize(self, width: int, height: int) -> None:
            self.size = (width, height)

        def setCentralWidget(self, widget) -> None:
            self.central_widget = widget

        def show(self) -> None:
            FakeQMainWindow.show_calls += 1

    class FakeQPdfDocument:
        error_paths: set[str] = set()

        class Status:
            Ready = 1
            Error = 2

        def __init__(self, _parent=None) -> None:
            self.loaded_path = None
            self._status = type(self).Status.Ready

        def load(self, path: str) -> None:
            self.loaded_path = str(path)
            if self.loaded_path in type(self).error_paths:
                self._status = type(self).Status.Error
            else:
                self._status = type(self).Status.Ready

        def status(self):
            return self._status

    class FakeQPdfView(FakeWidget):
        def setPageMode(self, _mode=None) -> None:
            return None

    qtcore = types.ModuleType("PySide6.QtCore")
    qtcore.Qt = types.SimpleNamespace(AlignmentFlag=types.SimpleNamespace(AlignCenter=1))
    qtcore.QTimer = FakeQTimer

    qtgui = types.ModuleType("PySide6.QtGui")
    qtgui.QPixmap = FakeQPixmap

    qtwidgets = types.ModuleType("PySide6.QtWidgets")
    qtwidgets.QApplication = FakeQApplication
    qtwidgets.QLabel = FakeQLabel
    qtwidgets.QMainWindow = FakeQMainWindow
    qtwidgets.QPlainTextEdit = FakeQPlainTextEdit
    qtwidgets.QStackedWidget = FakeQStackedWidget
    qtwidgets.QVBoxLayout = FakeQVBoxLayout
    qtwidgets.QWidget = FakeWidget

    monkeypatch.setitem(sys.modules, "PySide6", types.ModuleType("PySide6"))
    monkeypatch.setitem(sys.modules, "PySide6.QtCore", qtcore)
    monkeypatch.setitem(sys.modules, "PySide6.QtGui", qtgui)
    monkeypatch.setitem(sys.modules, "PySide6.QtWidgets", qtwidgets)

    if has_qtpdf:
        qtpdf = types.ModuleType("PySide6.QtPdf")
        qtpdf.QPdfDocument = FakeQPdfDocument
        qtpdfwidgets = types.ModuleType("PySide6.QtPdfWidgets")
        qtpdfwidgets.QPdfView = FakeQPdfView
        monkeypatch.setitem(sys.modules, "PySide6.QtPdf", qtpdf)
        monkeypatch.setitem(sys.modules, "PySide6.QtPdfWidgets", qtpdfwidgets)
    else:
        monkeypatch.delitem(sys.modules, "PySide6.QtPdf", raising=False)
        monkeypatch.delitem(sys.modules, "PySide6.QtPdfWidgets", raising=False)

    monkeypatch.delitem(sys.modules, "tabula.window", raising=False)
    module = importlib.import_module("tabula.window")
    module = importlib.reload(module)
    return module, {
        "QApplication": FakeQApplication,
        "QMainWindow": FakeQMainWindow,
        "QPixmap": FakeQPixmap,
        "QPdfDocument": FakeQPdfDocument,
    }


def test_window_apply_event_and_poll_paths_with_mocked_qtpdf(monkeypatch) -> None:
    window_module, fake = _import_window_with_fake_pyside(monkeypatch, has_qtpdf=True)
    window = window_module.CanvasWindow(poll_interval_ms=123)

    window.apply_event(
        TextArtifactEvent(
            event_id="e1",
            ts="2026-02-11T12:00:00Z",
            kind="text_artifact",
            title="draft",
            text="hello",
        )
    )
    assert window.stack.current_widget is window.text_view
    assert "text artifact 'draft'" in window.status_label.text()

    window.apply_event(
        ImageArtifactEvent(
            event_id="e2",
            ts="2026-02-11T12:00:01Z",
            kind="image_artifact",
            title="img",
            path="ok.png",
        )
    )
    assert window.stack.current_widget is window.image_label
    assert "image artifact 'img'" in window.status_label.text()

    fake["QPixmap"].null_paths.add("bad.png")
    window.apply_event(
        ImageArtifactEvent(
            event_id="e3",
            ts="2026-02-11T12:00:02Z",
            kind="image_artifact",
            title="img-bad",
            path="bad.png",
        )
    )
    assert "failed to load image bad.png" in window.status_label.text()

    window.apply_event(
        PdfArtifactEvent(
            event_id="e4",
            ts="2026-02-11T12:00:03Z",
            kind="pdf_artifact",
            title="doc",
            path="ok.pdf",
            page=0,
        )
    )
    assert window.stack.current_widget is window.pdf_view
    assert "pdf artifact 'doc'" in window.status_label.text()

    fake["QPdfDocument"].error_paths.add("bad.pdf")
    window.apply_event(
        PdfArtifactEvent(
            event_id="e5",
            ts="2026-02-11T12:00:04Z",
            kind="pdf_artifact",
            title="doc-bad",
            path="bad.pdf",
            page=0,
        )
    )
    assert "failed to load pdf bad.pdf" in window.status_label.text()

    window._incoming.put(
        ClearCanvasEvent(
            event_id="e6",
            ts="2026-02-11T12:00:05Z",
            kind="clear_canvas",
            reason="done",
        )
    )
    window._errors.put("line 9: bad payload")
    window.poll_once()
    assert window.stack.current_widget is window.blank_label
    assert "line 9: bad payload" in window.status_label.text()


def test_window_pdf_unavailable_branch_with_mocked_imports(monkeypatch) -> None:
    window_module, _ = _import_window_with_fake_pyside(monkeypatch, has_qtpdf=False)
    window = window_module.CanvasWindow(poll_interval_ms=50)
    window.apply_event(
        PdfArtifactEvent(
            event_id="e1",
            ts="2026-02-11T12:00:00Z",
            kind="pdf_artifact",
            title="doc",
            path="doc.pdf",
            page=0,
        )
    )
    assert window.stack.current_widget is window.pdf_view
    assert "QtPdf unavailable" in window.status_label.text()


def test_run_canvas_reuses_existing_qapplication_instance(monkeypatch) -> None:
    window_module, fake = _import_window_with_fake_pyside(monkeypatch, has_qtpdf=False)
    fake["QApplication"]._instance = None

    rc1 = window_module.run_canvas(poll_interval_ms=10)
    app = fake["QApplication"].instance()
    assert rc1 == 42
    assert app is not None
    assert app.exec_calls == 1

    rc2 = window_module.run_canvas(poll_interval_ms=20)
    assert rc2 == 42
    assert fake["QApplication"].instance() is app
    assert app.exec_calls == 2
    assert fake["QMainWindow"].show_calls >= 2
