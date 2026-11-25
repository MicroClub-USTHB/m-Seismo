import Foundation

/// DSN-style configuration for connecting an app to an m-Seismo project
/// (PRD FR-02, SDK-01).
public struct Configuration {
    /// The server's base address, e.g. `https://seismo.example.com`. The
    /// SDK adds the API path itself (`reportsURL`).
    public var baseURL: URL

    /// The app's SDK key (`msk_…`). It can only send reports.
    public var sdkKey: String
    public var environment: String
    public var release: String?

    /// Whether shake opens the report sheet (FR-05, FR-07: "each trigger
    /// can be turned on or off"). On by default.
    public var shakeEnabled: Bool

    /// Whether taking a screenshot opens the report sheet (FR-07). Off by
    /// default: unlike shake, a screenshot is often just... a screenshot,
    /// not a bug report, so this needs an explicit opt-in.
    public var screenshotEnabled: Bool

    public init(
        baseURL: URL,
        sdkKey: String,
        environment: String = "production",
        release: String? = nil,
        shakeEnabled: Bool = true,
        screenshotEnabled: Bool = false
    ) {
        self.baseURL = baseURL
        self.sdkKey = sdkKey
        self.environment = environment
        self.release = release
        self.shakeEnabled = shakeEnabled
        self.screenshotEnabled = screenshotEnabled
    }

    /// Where reports are posted: `POST {baseURL}/api/v1/reports`.
    public var reportsURL: URL {
        baseURL.appendingPathComponent("api/v1/reports")
    }
}
