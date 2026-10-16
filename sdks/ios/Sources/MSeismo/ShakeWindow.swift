#if os(iOS)
import UIKit

/// A `UIWindow` subclass that forwards the system shake gesture to a
/// `ShakeDetector`. Apps opt in by using this as their window class (see
/// the example app's `ExampleApp.swift`).
open class ShakeWindow: UIWindow {
    public var onShake: (() -> Void)?

    open override func motionEnded(_ motion: UIEvent.EventSubtype, with event: UIEvent?) {
        if motion == .motionShake {
            onShake?()
        }
        super.motionEnded(motion, with: event)
    }
}
#endif
