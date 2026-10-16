import Foundation

/// Entry point for the m-Seismo iOS SDK. Skeleton: connects an app to a
/// project, nothing else is wired up yet (see PRD FR-02).
public final class MSeismo {
    public static let shared = MSeismo()

    public private(set) var projectKey: String?
    public private(set) var environment: String?

    private init() {}

    /// Connects the app to an m-Seismo project.
    public func configure(projectKey: String, environment: String = "production") {
        self.projectKey = projectKey
        self.environment = environment
    }
}
