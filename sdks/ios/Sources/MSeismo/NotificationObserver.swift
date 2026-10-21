import Foundation

/// Minimal `NotificationCenter` observation wrapper: starts/stops cleanly
/// and forwards matching notifications to a closure. Platform-agnostic
/// (Foundation only) so it's testable without UIKit; `MSeismo` wires one to
/// `UIApplication.userDidTakeScreenshotNotification` on iOS (FR-07).
public final class NotificationObserver {
    private let center: NotificationCenter
    private let name: Notification.Name
    private var token: NSObjectProtocol?

    public var onNotify: (() -> Void)?

    public init(name: Notification.Name, center: NotificationCenter = .default, onNotify: (() -> Void)? = nil) {
        self.name = name
        self.center = center
        self.onNotify = onNotify
    }

    public func start() {
        stop()
        token = center.addObserver(forName: name, object: nil, queue: nil) { [weak self] _ in
            self?.onNotify?()
        }
    }

    public func stop() {
        if let token {
            center.removeObserver(token)
        }
        token = nil
    }

    deinit {
        stop()
    }
}
