import PencilKit
import SwiftUI

struct TaburaInkCaptureView: UIViewRepresentable {
    private static let penWidth = 2.4
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
        canvas.tool = PKInkingTool(.pen, color: .black, width: Self.penWidth)
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
            let strokeSlice = drawing.strokes[lastStrokeCount...]
            let newStrokes = strokeSlice.compactMap(makeStroke)
            lastStrokeCount = drawing.strokes.count
            guard !newStrokes.isEmpty else {
                return
            }
            onCommit(newStrokes)
        }

        private func makeStroke(from stroke: PKStroke) -> TaburaInkStroke? {
            let points = stroke.path.map(makePoint)
            guard !points.isEmpty else {
                return nil
            }
            return TaburaInkStroke(
                pointerType: "pencil",
                width: TaburaInkCaptureView.penWidth,
                points: points
            )
        }

        private func makePoint(from point: PKStrokePoint) -> TaburaInkPoint {
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
    }
}
