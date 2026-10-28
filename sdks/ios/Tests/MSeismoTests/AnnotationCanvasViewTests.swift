#if os(iOS)
import XCTest
@testable import MSeismo

final class AnnotationCanvasViewTests: XCTestCase {
    func testHasNoStrokesInitially() {
        let canvas = AnnotationCanvasView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))

        XCTAssertFalse(canvas.hasStrokes)
    }

    func testClearIsSafeWithNoStrokes() {
        let canvas = AnnotationCanvasView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))

        canvas.clear()

        XCTAssertFalse(canvas.hasStrokes)
    }

    func testUndoLastIsSafeWithNoStrokes() {
        let canvas = AnnotationCanvasView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))

        canvas.undoLast()

        XCTAssertFalse(canvas.hasStrokes)
    }

    func testRenderImageWithNoStrokesPreservesSize() {
        let canvas = AnnotationCanvasView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))
        let baseImage = UIGraphicsImageRenderer(size: CGSize(width: 50, height: 80)).image { context in
            UIColor.blue.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 50, height: 80))
        }

        let rendered = canvas.renderImage(over: baseImage)

        XCTAssertEqual(rendered.size, baseImage.size)
    }
}
#endif
