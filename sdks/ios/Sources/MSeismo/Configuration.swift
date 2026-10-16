import Foundation

/// DSN-style configuration for connecting an app to an m-Seismo project
/// (PRD FR-02, SDK-01).
public struct Configuration {
    public var endpoint: URL
    public var projectKey: String
    public var environment: String
    public var release: String?

    public init(
        endpoint: URL,
        projectKey: String,
        environment: String = "production",
        release: String? = nil
    ) {
        self.endpoint = endpoint
        self.projectKey = projectKey
        self.environment = environment
        self.release = release
    }
}
