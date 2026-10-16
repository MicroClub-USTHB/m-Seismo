import Combine
import Foundation

/// Entry point for the m-Seismo iOS SDK. Connects an app to a project
/// (FR-02) and opens the report sheet when a shake is detected (FR-05).
public final class MSeismo: ObservableObject {
    public static let shared = MSeismo()

    public private(set) var projectKey: String?
    public private(set) var environment: String?

    public let shakeDetector = ShakeDetector()

    /// Drives presentation of `ReportFormView`. An app observes this
    /// (`@ObservedObject var mseismo = MSeismo.shared`) and binds it to a
    /// `.sheet(isPresented:)`.
    @Published public var isReportSheetPresented = false

    private init() {
        shakeDetector.onShake = { [weak self] in
            self?.isReportSheetPresented = true
        }
    }

    /// Connects the app to an m-Seismo project.
    public func configure(projectKey: String, environment: String = "production") {
        self.projectKey = projectKey
        self.environment = environment
    }
}
