import Combine
import Foundation

#if os(iOS)
import UIKit
#endif

/// Entry point for the m-Seismo iOS SDK. Connects an app to a project
/// (FR-02) and opens the report sheet on any of its triggers: shake
/// (FR-05), screenshot or manual call (FR-07).
public final class MSeismo: ObservableObject {
    public static let shared = MSeismo()

    public private(set) var configuration: Configuration?

    /// One per app launch; carried on every report (FR-22, SDK-09).
    public let session = Session()

    public let shakeDetector = ShakeDetector()

    #if os(iOS)
    private let screenshotObserver = NotificationObserver(name: UIApplication.userDidTakeScreenshotNotification)
    #endif

    /// Drives presentation of `ReportFormView`. An app observes this
    /// (`@ObservedObject var mseismo = MSeismo.shared`) and binds it to a
    /// `.sheet(isPresented:)`.
    @Published public var isReportSheetPresented = false

    private init() {
        shakeDetector.onShake = { [weak self] in
            self?.report()
        }

        #if os(iOS)
        screenshotObserver.onNotify = { [weak self] in
            self?.report()
        }
        screenshotObserver.start()
        #endif
    }

    /// Connects the app to an m-Seismo project.
    public func configure(_ configuration: Configuration) {
        self.configuration = configuration
    }

    /// Opens the report sheet. Called automatically by the shake and
    /// screenshot triggers; also public so an app can call it directly,
    /// e.g. from a "Report a bug" button or a long-press gesture (FR-07).
    public func report() {
        isReportSheetPresented = true
    }
}
