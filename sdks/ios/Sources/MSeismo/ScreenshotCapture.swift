#if os(iOS)
import UIKit

/// Captures the actual current screen (FR-19), not a placeholder: renders
/// the key window's live view hierarchy into an image via the same public
/// API `UIView.drawHierarchy(in:afterScreenUpdates:)` screenshot tools use.
enum ScreenshotCapture {
    static func captureCurrentScreen() -> UIImage? {
        let keyWindow = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)

        guard let window = keyWindow else { return nil }

        let renderer = UIGraphicsImageRenderer(bounds: window.bounds)
        return renderer.image { _ in
            window.drawHierarchy(in: window.bounds, afterScreenUpdates: false)
        }
    }
}
#endif
