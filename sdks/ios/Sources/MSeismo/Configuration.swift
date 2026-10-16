import Foundation

/// DSN-style configuration for connecting an app to an m-Seismo project
/// (PRD FR-02, SDK-01).
public struct Configuration {
    public var endpoint: URL
    public var projectKey: String
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
        endpoint: URL,
        projectKey: String,
        environment: String = "production",
        release: String? = nil,
        shakeEnabled: Bool = true,
        screenshotEnabled: Bool = false
    ) {
        self.endpoint = endpoint
        self.projectKey = projectKey
        self.environment = environment
        self.release = release
        self.shakeEnabled = shakeEnabled
        self.screenshotEnabled = screenshotEnabled
    }
}
