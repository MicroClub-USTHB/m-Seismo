import Foundation

/// A user's recent action: screens visited, taps, network calls, and app
/// lifecycle events (FR-18, SDK-07).
public struct Breadcrumb: Codable, Equatable {
    public enum Kind: String, Codable, Equatable {
        case navigation, tap, network, lifecycle
    }

    public var kind: Kind
    public var message: String
    public var timestamp: Date

    public init(kind: Kind, message: String, timestamp: Date = Date()) {
        self.kind = kind
        self.message = message
        self.timestamp = timestamp
    }
}

/// Ring buffer of the user's most recent actions (FR-18).
public final class BreadcrumbTracker {
    private var buffer: RingBuffer<Breadcrumb>

    public init(capacity: Int = 50) {
        buffer = RingBuffer(capacity: capacity)
    }

    public func record(_ kind: Breadcrumb.Kind, _ message: String) {
        buffer.append(Breadcrumb(kind: kind, message: message))
    }

    public var recent: [Breadcrumb] { buffer.elements }
}
