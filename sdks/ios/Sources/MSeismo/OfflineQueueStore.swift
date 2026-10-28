import Foundation

/// A report waiting to be delivered, with its retry state (SDK-10).
public struct QueuedReport: Codable, Equatable {
    public var id: UUID
    public var report: Report
    public var attempts: Int
    public var nextAttemptAt: Date

    public init(id: UUID = UUID(), report: Report, attempts: Int = 0, nextAttemptAt: Date = Date()) {
        self.id = id
        self.report = report
        self.attempts = attempts
        self.nextAttemptAt = nextAttemptAt
    }
}

/// Persists the pending-delivery queue to disk, so a report "still unsent
/// when the app closes is sent on the next launch" (FR-33) rather than
/// only living in memory.
public final class OfflineQueueStore {
    private let fileURL: URL

    public init(directory: URL = FileManager.default.temporaryDirectory) {
        fileURL = directory.appendingPathComponent("mseismo-offline-queue.json")
    }

    public func load() -> [QueuedReport] {
        guard let data = try? Data(contentsOf: fileURL) else { return [] }
        return (try? JSONDecoder().decode([QueuedReport].self, from: data)) ?? []
    }

    public func save(_ items: [QueuedReport]) {
        guard let data = try? JSONEncoder().encode(items) else { return }
        try? data.write(to: fileURL, options: .atomic)
    }
}
