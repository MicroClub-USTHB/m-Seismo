import Foundation

/// Persists a pending `CrashReport` to disk, since nothing async can run
/// reliably at the moment of a crash — it's written synchronously by
/// `CrashReporter` and read back (and cleared) on the next launch (FR-08:
/// "a crash is reported on the next launch").
public final class CrashStore {
    private let fileURL: URL

    public init(directory: URL = FileManager.default.temporaryDirectory) {
        fileURL = directory.appendingPathComponent("mseismo-pending-crash.json")
    }

    public func save(_ crash: CrashReport) {
        guard let data = try? JSONEncoder().encode(crash) else { return }
        try? data.write(to: fileURL, options: .atomic)
    }

    /// Returns the pending crash, if any, and deletes it so it's only
    /// reported once.
    public func loadAndClear() -> CrashReport? {
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        try? FileManager.default.removeItem(at: fileURL)
        return try? JSONDecoder().decode(CrashReport.self, from: data)
    }
}
