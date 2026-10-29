import Foundation

/// A reporter-submitted bug report (PRD FR-10). Every field is optional
/// (FR-12): defaults exist so a report can be sent with none of them filled
/// in, in auto-submit mode.
public struct Report: Codable, Equatable {
    public enum Category: String, CaseIterable, Identifiable, Equatable, Codable {
        case bug, crash, ui, other

        public var id: String { rawValue }

        public var label: String {
            switch self {
            case .bug: return "Bug"
            case .crash: return "Crash"
            case .ui: return "UI"
            case .other: return "Other"
            }
        }
    }

    public enum Severity: String, CaseIterable, Identifiable, Equatable, Codable {
        case low, medium, high, critical

        public var id: String { rawValue }
        public var label: String { rawValue.capitalized }
    }

    /// What started the report (schema `trigger`).
    public enum Trigger: String, Equatable, Codable {
        case shake, screenshot, manual, crash
        case longPress = "long_press"
        case nonFatal = "non_fatal"
    }

    public var title: String
    public var description: String
    public var category: Category
    public var severity: Severity

    /// A screenshot the reporter attached from their photo library (JPEG).
    /// `Data`, not `UIImage`, so this type stays usable outside iOS-only
    /// code (see `ReportFormView`, which populates it).
    public var screenshot: Data?

    /// Device/app context captured automatically at submission time
    /// (FR-15), not filled in by the reporter.
    public var deviceContext: DeviceContext?

    /// The session this report was filed from (FR-22, SDK-09), so it can
    /// be matched against whatever the server logged for the same session.
    public var sessionID: String?

    /// Recent app log lines at the moment of submission (FR-17/SDK-07).
    public var recentLogs: [LogLine]?

    /// The user's recent actions leading up to this report (FR-18/SDK-07).
    public var breadcrumbs: [Breadcrumb]?

    /// Recent network requests (FR-23). Bodies are only present when the
    /// app opted in via `NetworkLogger.capturesBodies` (FR-28).
    public var networkRequests: [NetworkRequestLog]?

    /// Idempotency key: the backend stores the same ID once (ingestion
    /// §3), so it is assigned once, when the report is first submitted,
    /// and kept across every retry. Optional so reports queued by an
    /// older SDK build still decode.
    public var id: String?

    /// When the report was triggered on the device; for a crash, when the
    /// crash happened.
    public var occurredAt: Date?

    /// What started the report. `nil` is sent as `manual`.
    public var trigger: Trigger?

    /// Crash details for an automatic crash or non-fatal report. The
    /// backend requires them when `trigger` is `crash` or `non_fatal`.
    public var crash: CrashReport?

    public init(
        title: String = "",
        description: String = "",
        category: Category = .bug,
        severity: Severity = .medium,
        screenshot: Data? = nil,
        deviceContext: DeviceContext? = nil,
        sessionID: String? = nil,
        recentLogs: [LogLine]? = nil,
        breadcrumbs: [Breadcrumb]? = nil,
        networkRequests: [NetworkRequestLog]? = nil,
        id: String? = nil,
        occurredAt: Date? = nil,
        trigger: Trigger? = nil,
        crash: CrashReport? = nil
    ) {
        self.title = title
        self.description = description
        self.category = category
        self.severity = severity
        self.screenshot = screenshot
        self.deviceContext = deviceContext
        self.sessionID = sessionID
        self.recentLogs = recentLogs
        self.breadcrumbs = breadcrumbs
        self.networkRequests = networkRequests
        self.id = id
        self.occurredAt = occurredAt
        self.trigger = trigger
        self.crash = crash
    }
}
