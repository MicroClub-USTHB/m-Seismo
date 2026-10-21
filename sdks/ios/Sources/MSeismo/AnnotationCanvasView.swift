#if os(iOS)
import UIKit

/// What a drawn stroke does: point at the bug, or hide something (FR-11).
public enum AnnotationTool {
    case pen
    case blackout

    var color: UIColor {
        switch self {
        case .pen: return .systemRed
        case .blackout: return .black
        }
    }

    var lineWidth: CGFloat {
        switch self {
        case .pen: return 4
        case .blackout: return 28
        }
    }
}

private struct Stroke {
    let path: UIBezierPath
    let tool: AnnotationTool
}

/// A touch-driven drawing surface overlaid on the screenshot. Freehand
/// only (no shape recognition): the "blackout" tool is a thick solid
/// stroke painted over the sensitive area, not a true Gaussian blur --
/// simpler to implement correctly, and FR-11 explicitly allows "black out"
/// as an alternative to blurring.
final class AnnotationCanvasView: UIView {
    var tool: AnnotationTool = .pen

    private var strokes: [Stroke] = []
    private var currentPath: UIBezierPath?

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .clear
        isOpaque = false
    }

    required init?(coder: NSCoder) {
        super.init(coder: coder)
        backgroundColor = .clear
        isOpaque = false
    }

    var hasStrokes: Bool { !strokes.isEmpty }

    func clear() {
        strokes.removeAll()
        currentPath = nil
        setNeedsDisplay()
    }

    func undoLast() {
        _ = strokes.popLast()
        setNeedsDisplay()
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let point = touches.first?.location(in: self) else { return }
        let path = UIBezierPath()
        path.lineCapStyle = .round
        path.lineJoinStyle = .round
        path.move(to: point)
        currentPath = path
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let point = touches.first?.location(in: self), let path = currentPath else { return }
        path.addLine(to: point)
        setNeedsDisplay()
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let path = currentPath else { return }
        strokes.append(Stroke(path: path, tool: tool))
        currentPath = nil
        setNeedsDisplay()
    }

    override func draw(_ rect: CGRect) {
        for stroke in strokes {
            stroke.tool.color.setStroke()
            stroke.path.lineWidth = stroke.tool.lineWidth
            stroke.path.stroke()
        }
        if let currentPath {
            tool.color.setStroke()
            currentPath.lineWidth = tool.lineWidth
            currentPath.stroke()
        }
    }

    /// Flattens `baseImage` with every stroke drawn on top, scaled from
    /// this view's point size to the image's pixel size.
    func renderImage(over baseImage: UIImage) -> UIImage {
        let format = UIGraphicsImageRendererFormat()
        format.scale = baseImage.scale
        let renderer = UIGraphicsImageRenderer(size: baseImage.size, format: format)

        let scaleX = baseImage.size.width / max(bounds.width, 1)
        let scaleY = baseImage.size.height / max(bounds.height, 1)

        return renderer.image { context in
            baseImage.draw(in: CGRect(origin: .zero, size: baseImage.size))

            context.cgContext.scaleBy(x: scaleX, y: scaleY)
            for stroke in strokes {
                stroke.tool.color.setStroke()
                stroke.path.lineWidth = stroke.tool.lineWidth
                stroke.path.stroke()
            }
        }
    }
}
#endif
