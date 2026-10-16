#if os(iOS)
import SwiftUI
import UIKit

private struct AnnotationCanvasRepresentable: UIViewRepresentable {
    @Binding var tool: AnnotationTool
    let canvas: AnnotationCanvasView

    func makeUIView(context: Context) -> AnnotationCanvasView {
        canvas
    }

    func updateUIView(_ uiView: AnnotationCanvasView, context: Context) {
        uiView.tool = tool
    }
}

/// Lets the reporter draw on the screenshot to point at the bug, and
/// black out sensitive areas, before it's attached to the report (FR-11).
public struct ScreenshotAnnotationView: View {
    let originalImage: UIImage
    let onDone: (UIImage) -> Void
    let onCancel: () -> Void

    @State private var tool: AnnotationTool = .pen
    private let canvas = AnnotationCanvasView()

    public init(image: UIImage, onDone: @escaping (UIImage) -> Void, onCancel: @escaping () -> Void) {
        originalImage = image
        self.onDone = onDone
        self.onCancel = onCancel
    }

    public var body: some View {
        NavigationView {
            VStack(spacing: 0) {
                GeometryReader { _ in
                    ZStack {
                        Image(uiImage: originalImage)
                            .resizable()
                            .scaledToFit()
                        AnnotationCanvasRepresentable(tool: $tool, canvas: canvas)
                    }
                }

                Picker("Tool", selection: $tool) {
                    Text("Draw").tag(AnnotationTool.pen)
                    Text("Black Out").tag(AnnotationTool.blackout)
                }
                .pickerStyle(SegmentedPickerStyle())
                .padding()
            }
            .navigationBarTitle("Annotate Screenshot", displayMode: .inline)
            .navigationBarItems(
                leading: Button("Cancel", action: onCancel)
                    .foregroundColor(.accentColor),
                trailing: HStack(spacing: 16) {
                    Button("Undo") { canvas.undoLast() }
                    Button(action: { onDone(canvas.renderImage(over: originalImage)) }) {
                        Text("Done").fontWeight(.semibold)
                    }
                }
                .foregroundColor(.accentColor)
            )
        }
    }
}

extension AnnotationTool: Hashable {
    public static func == (lhs: AnnotationTool, rhs: AnnotationTool) -> Bool {
        switch (lhs, rhs) {
        case (.pen, .pen), (.blackout, .blackout): return true
        default: return false
        }
    }

    public func hash(into hasher: inout Hasher) {
        switch self {
        case .pen: hasher.combine(0)
        case .blackout: hasher.combine(1)
        }
    }
}
#endif
