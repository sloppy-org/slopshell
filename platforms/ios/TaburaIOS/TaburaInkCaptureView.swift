import PencilKit
import SwiftUI

struct TaburaInkCaptureView: UIViewRepresentable {
    let onCommit: ([TaburaInkStroke]) -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(onCommit: onCommit)
    }

    func makeUIView(context: Context) -> PKCanvasView {
        let canvas = PKCanvasView()
        canvas.backgroundColor = .clear
        canvas.isOpaque = false
        canvas.drawingPolicy = .pencilOnly
        canvas.delegate = context.coordinator
        canvas.tool = PKInkingTool(.pen, color: .black, width: 2.4)
        return canvas
    }

    func updateUIView(_ uiView: PKCanvasView, context: Context) {
        uiView.delegate = context.coordinator
    }

    final class Coordinator: NSObject, PKCanvasViewDelegate {
        private let onCommit: ([TaburaInkStroke]) -> Void
        private var lastStrokeCount = 0

        init(onCommit: @escaping ([TaburaInkStroke]) -> Void) {
            self.onCommit = onCommit
        }

        func canvasViewDrawingDidChange(_ canvasView: PKCanvasView) {
            let drawing = canvasView.drawing
            guard drawing.strokes.count > lastStrokeCount else {
                return
            }
            let newStrokes = Array(drawing.strokes[lastStrokeCount...]).map { stroke in
                TaburaInkStroke(
                    pointerType: "pencil",
                    width: Double(stroke.ink.width),
                    points: stroke.path.map { point in
                        TaburaInkPoint(
                            x: point.location.x,
                            y: point.location.y,
                            pressure: Double(point.force),
                            tiltX: Double(point.azimuth),
                            tiltY: Double(point.altitude),
                            roll: 0,
                            timestampMS: point.timeOffset * 1000
                        )
                    }
                )
            }.filter { !$0.points.isEmpty }
            lastStrokeCount = drawing.strokes.count
            guard !newStrokes.isEmpty else {
                return
            }
            onCommit(newStrokes)
        }
    }
}
