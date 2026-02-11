from __future__ import annotations

import queue
import sys
import threading
from pathlib import Path

from .events import CanvasEvent, EventValidationError, parse_event_line
from .state import CanvasState, reduce_state

from PySide6.QtCore import Qt, QTimer
from PySide6.QtGui import QPixmap
from PySide6.QtWidgets import (
    QApplication,
    QLabel,
    QMainWindow,
    QPlainTextEdit,
    QStackedWidget,
    QVBoxLayout,
    QWidget,
)

try:
    from PySide6.QtPdf import QPdfDocument
    from PySide6.QtPdfWidgets import QPdfView

    HAS_QTPDF = True
except Exception:  # pragma: no cover
    QPdfDocument = None
    QPdfView = None
    HAS_QTPDF = False


class CanvasWindow(QMainWindow):
    def __init__(self, *, poll_interval_ms: int = 250) -> None:
        super().__init__()
        self.setWindowTitle("Tabula Canvas")
        self.resize(1000, 700)

        self._state = CanvasState()
        self._incoming: queue.SimpleQueue[CanvasEvent] = queue.SimpleQueue()
        self._errors: queue.SimpleQueue[str] = queue.SimpleQueue()

        root = QWidget(self)
        layout = QVBoxLayout(root)

        self.mode_label = QLabel("mode: prompt")
        self.mode_label.setObjectName("modeLabel")
        layout.addWidget(self.mode_label)

        self.status_label = QLabel("status: waiting for MCP events")
        self.status_label.setObjectName("statusLabel")
        layout.addWidget(self.status_label)

        self.stack = QStackedWidget()
        layout.addWidget(self.stack, 1)

        self.blank_label = QLabel("Canvas inactive")
        self.blank_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.stack.addWidget(self.blank_label)

        self.text_view = QPlainTextEdit()
        self.text_view.setReadOnly(True)
        self.stack.addWidget(self.text_view)

        self.image_label = QLabel("image")
        self.image_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.image_label.setScaledContents(False)
        self.stack.addWidget(self.image_label)

        if HAS_QTPDF:
            self.pdf_document = QPdfDocument(self)
            self.pdf_view = QPdfView()
            self.pdf_view.setDocument(self.pdf_document)
            self.stack.addWidget(self.pdf_view)
        else:
            self.pdf_document = None
            self.pdf_view = QLabel("QtPdf unavailable")
            self.pdf_view.setAlignment(Qt.AlignmentFlag.AlignCenter)
            self.stack.addWidget(self.pdf_view)

        self.setCentralWidget(root)

        self._reader = threading.Thread(target=self._read_stdin_loop, daemon=True)
        self._reader.start()

        self._timer = QTimer(self)
        self._timer.timeout.connect(self.poll_once)
        self._timer.start(poll_interval_ms)

    def _read_stdin_loop(self) -> None:
        base_dir = Path.cwd()
        try:
            for raw in sys.stdin:
                line = raw.strip()
                if not line:
                    continue
                try:
                    event = parse_event_line(line, base_dir=base_dir)
                except EventValidationError as exc:
                    self._errors.put(str(exc))
                    continue
                self._incoming.put(event)
        except OSError:
            # Test harnesses may block stdin reads while output capture is active.
            return

    def apply_event(self, event: CanvasEvent) -> None:
        self._state = reduce_state(self._state, event)
        self.mode_label.setText(f"mode: {self._state.mode}")

        if event.kind == "clear_canvas":
            self.stack.setCurrentWidget(self.blank_label)
            self.status_label.setText("status: canvas cleared")
            return

        if event.kind == "text_artifact":
            self.text_view.setPlainText(event.text)
            self.stack.setCurrentWidget(self.text_view)
            self.status_label.setText(f"status: text artifact '{event.title}'")
            return

        if event.kind == "image_artifact":
            pixmap = QPixmap(event.path)
            if pixmap.isNull():
                self.status_label.setText(f"status: failed to load image {event.path}")
                return
            self.image_label.setPixmap(pixmap)
            self.stack.setCurrentWidget(self.image_label)
            self.status_label.setText(f"status: image artifact '{event.title}'")
            return

        if event.kind == "pdf_artifact":
            if HAS_QTPDF and self.pdf_document is not None:
                self.pdf_document.load(event.path)
                if self.pdf_document.status() == QPdfDocument.Status.Ready:
                    self.stack.setCurrentWidget(self.pdf_view)
                    self.status_label.setText(f"status: pdf artifact '{event.title}'")
                else:
                    self.status_label.setText(f"status: failed to load pdf {event.path}")
            else:
                self.stack.setCurrentWidget(self.pdf_view)
                self.status_label.setText("status: QtPdf unavailable")

    def poll_once(self) -> None:
        while True:
            try:
                event = self._incoming.get_nowait()
            except queue.Empty:
                break
            self.apply_event(event)

        last_error: str | None = None
        while True:
            try:
                last_error = self._errors.get_nowait()
            except queue.Empty:
                break
        if last_error is not None:
            self.status_label.setText("status: " + last_error)


def run_canvas(*, poll_interval_ms: int = 250) -> int:
    app = QApplication.instance() or QApplication([])
    window = CanvasWindow(poll_interval_ms=poll_interval_ms)
    window.show()
    return app.exec()
