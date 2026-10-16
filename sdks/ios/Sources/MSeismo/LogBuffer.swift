import Foundation

/// One recorded log line (SDK-07).
public struct LogLine: Codable, Equatable {
    public var message: String
    public var timestamp: Date

    public init(message: String, timestamp: Date = Date()) {
        self.message = message
        self.timestamp = timestamp
    }
}

/// The app's most recent log lines, limited to the last N lines, or the
/// last N seconds when `maxAge` is set (FR-17/SDK-07).
public final class LogBuffer {
    private var buffer: RingBuffer<LogLine>
    private let maxAge: TimeInterval?
    private let now: () -> Date

    public init(maxLines: Int = 200, maxAge: TimeInterval? = nil, now: @escaping () -> Date = Date.init) {
        buffer = RingBuffer(capacity: maxLines)
        self.maxAge = maxAge
        self.now = now
    }

    public func log(_ message: String) {
        buffer.append(LogLine(message: message, timestamp: now()))
    }

    public var recent: [LogLine] {
        guard let maxAge else { return buffer.elements }
        let cutoff = now().addingTimeInterval(-maxAge)
        return buffer.elements.filter { $0.timestamp >= cutoff }
    }
}
