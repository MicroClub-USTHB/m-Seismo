#if os(iOS)
import SwiftUI
import UIKit

/// Becomes first responder so it receives `motionEnded`, without being
/// visible. This is how a pure SwiftUI app (no custom `UIWindow` subclass,
/// see `ShakeWindow`) observes the system shake gesture.
private final class ShakeDetectingViewController: UIViewController {
    var onShake: (() -> Void)?

    override var canBecomeFirstResponder: Bool { true }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        becomeFirstResponder()
    }

    override func motionEnded(_ motion: UIEvent.EventSubtype, with event: UIEvent?) {
        if motion == .motionShake {
            onShake?()
        }
        super.motionEnded(motion, with: event)
    }
}

private struct ShakeDetectingViewControllerRepresentable: UIViewControllerRepresentable {
    let onShake: () -> Void

    func makeUIViewController(context: Context) -> UIViewController {
        let controller = ShakeDetectingViewController()
        controller.onShake = onShake
        return controller
    }

    func updateUIViewController(_ uiViewController: UIViewController, context: Context) {}
}

public extension View {
    /// Calls `action` when the system shake gesture is detected while this
    /// view is visible (PRD FR-05). Pair with `ShakeDetector` to apply the
    /// shake cooldown (FR-06) before acting on it.
    func onShake(perform action: @escaping () -> Void) -> some View {
        background(
            ShakeDetectingViewControllerRepresentable(onShake: action)
                .frame(width: 0, height: 0)
        )
    }
}
#endif
