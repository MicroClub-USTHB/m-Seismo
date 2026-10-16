import Foundation

/// Correlates a report, and any request made during the same app launch,
/// with what the server saw at that moment (PRD FR-22, SDK-09).
public final class Session {
    /// Stable for the lifetime of this `Session` (normally one per app
    /// launch). Carried on every report.
    public let id: String

    public init() {
        id = UUID().uuidString
    }

    /// A fresh identifier for one request, meant to be sent as a
    /// per-request trace header (e.g. `X-Seismo-Trace-Id`) alongside the
    /// stable `id`, so server logs for that one call can be matched too.
    public func newTraceID() -> String {
        UUID().uuidString
    }
}
