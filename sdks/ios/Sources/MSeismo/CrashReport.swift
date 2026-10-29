import Foundation

/// A captured uncaught exception or recorded error (FR-08/FR-09, SDK-08).
/// Platform-agnostic so it can be persisted and decoded without UIKit.
public struct CrashReport: Codable, Equatable {
    public var name: String
    public var reason: String?
    public var callStack: [String]
    public var isFatal: Bool
    public var timestamp: Date

    public init(name: String, reason: String?, callStack: [String], isFatal: Bool, timestamp: Date = Date()) {
        self.name = name
        self.reason = reason
        self.callStack = callStack
        self.isFatal = isFatal
        self.timestamp = timestamp
    }

    /// Builds the `Report` m-Seismo files automatically for this crash
    /// (FR-08: "without any action from the user"), flagged non-fatal
    /// per FR-09 when it isn't a crash.
    public func asReport() -> Report {
        var description = reason ?? ""
        if !callStack.isEmpty {
            if !description.isEmpty { description += "\n\n" }
            description += callStack.joined(separator: "\n")
        }

        return Report(
            title: name,
            description: description,
            category: .crash,
            severity: isFatal ? .critical : .high,
            occurredAt: timestamp,
            trigger: isFatal ? .crash : .nonFatal,
            crash: self
        )
    }
}
