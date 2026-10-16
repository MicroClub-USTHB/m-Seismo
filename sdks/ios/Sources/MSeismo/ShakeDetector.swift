import Foundation

/// Debounces repeated shake triggers so one physical shake can't fire more
/// than one report (PRD FR-06). Platform-agnostic: the actual shake gesture
/// is detected by `ShakeWindow` on iOS and forwarded here.
public final class ShakeDetector {
    public var cooldown: TimeInterval
    public var onShake: (() -> Void)?

    private var lastTrigger: Date?
    private let now: () -> Date

    public init(cooldown: TimeInterval = 2.0, now: @escaping () -> Date = Date.init) {
        self.cooldown = cooldown
        self.now = now
    }

    /// Called whenever the underlying platform detects a shake gesture.
    /// Ignored if called again before `cooldown` has elapsed.
    public func shakeDetected() {
        let current = now()

        if let last = lastTrigger, current.timeIntervalSince(last) < cooldown {
            return
        }

        lastTrigger = current
        onShake?()
    }
}
