import Foundation

/// Debounces repeated shake triggers so one physical shake can't fire more
/// than one report (PRD FR-06). Platform-agnostic: the actual shake gesture
/// is detected by `ShakeWindow` on iOS and forwarded here.
public final class ShakeDetector {
    public var cooldown: TimeInterval
    public var onShake: (() -> Void)?

    /// Set from `Configuration.shakeEnabled`. True by default, so shake
    /// works even before `configure(_:)` is called.
    public var isEnabled = true

    private var lastTrigger: Date?
    private let now: () -> Date

    public init(cooldown: TimeInterval = 2.0, now: @escaping () -> Date = Date.init) {
        self.cooldown = cooldown
        self.now = now
    }

    /// Called whenever the underlying platform detects a shake gesture.
    /// Ignored if `isEnabled` is false, or if called again before
    /// `cooldown` has elapsed.
    public func shakeDetected() {
        guard isEnabled else { return }

        let current = now()

        if let last = lastTrigger, current.timeIntervalSince(last) < cooldown {
            return
        }

        lastTrigger = current
        onShake?()
    }
}
