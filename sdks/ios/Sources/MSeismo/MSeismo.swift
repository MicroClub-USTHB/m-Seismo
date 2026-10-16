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

    /// Recent app log lines (FR-17/SDK-07). Call `logs.log(_:)` wherever
    /// the app already logs.
    public let logs = LogBuffer()

    /// The user's recent actions (FR-18/SDK-07). Lifecycle events are
    /// recorded automatically; call `breadcrumbs.record(_:_:)` for
    /// navigation, taps, and network calls, which this SDK can't observe
    /// on its own.
    public let breadcrumbs = BreadcrumbTracker()

    private let crashReporter = CrashReporter.shared

    /// Called with a crash/error report that was captured automatically
    /// (FR-08/FR-09) — no sheet is shown for these. Set this, then call
    /// `checkForPendingCrash()` once at launch.
    public var onAutoReport: ((Report) -> Void)?

    public let shakeDetector = ShakeDetector()

    #if os(iOS)
    private let screenshotObserver = NotificationObserver(name: UIApplication.userDidTakeScreenshotNotification)
    private var lifecycleObservers: [NotificationObserver] = []
    #endif

    /// Drives presentation of `ReportFormView`. An app observes this
    /// (`@ObservedObject var mseismo = MSeismo.shared`) and binds it to a
    /// `.sheet(isPresented:)`.
    @Published public var isReportSheetPresented = false

    private init() {
        crashReporter.start()

        shakeDetector.onShake = { [weak self] in
            self?.report()
        }

        #if os(iOS)
        screenshotObserver.onNotify = { [weak self] in
            self?.report()
        }
        // Not started here: off by default until configure(_:) says
        // otherwise (Configuration.screenshotEnabled).

        observeLifecycle(UIApplication.didBecomeActiveNotification, message: "resumed")
        observeLifecycle(UIApplication.didEnterBackgroundNotification, message: "backgrounded")
        #endif
    }

    #if os(iOS)
    private func observeLifecycle(_ name: Notification.Name, message: String) {
        let observer = NotificationObserver(name: name) { [weak self] in
            self?.breadcrumbs.record(.lifecycle, message)
        }
        observer.start()
        lifecycleObservers.append(observer)
    }
    #endif

    /// Connects the app to an m-Seismo project, and applies its trigger
    /// settings (`shakeEnabled`, `screenshotEnabled`).
    public func configure(_ configuration: Configuration) {
        self.configuration = configuration

        shakeDetector.isEnabled = configuration.shakeEnabled

        #if os(iOS)
        if configuration.screenshotEnabled {
            screenshotObserver.start()
        } else {
            screenshotObserver.stop()
        }
        #endif
    }

    /// Opens the report sheet. Called automatically by the shake and
    /// screenshot triggers; also public so an app can call it directly,
    /// e.g. from a "Report a bug" button or a long-press gesture (FR-07).
    public func report() {
        isReportSheetPresented = true
    }

    /// Records an error the app already caught, to be auto-reported on
    /// the next launch (FR-09). Pass `fatal: true` to flag it the same
    /// way an uncaught crash would be.
    public func recordError(_ error: Error, fatal: Bool = false) {
        crashReporter.recordError(error, fatal: fatal)
    }

    /// Call once at launch, after setting `onAutoReport`. If the previous
    /// launch crashed (or recorded an error), builds its report with the
    /// same context a manual report gets and delivers it — no sheet shown.
    public func checkForPendingCrash() {
        guard let crash = crashReporter.pendingCrash() else { return }

        var report = crash.asReport()
        report.sessionID = session.id
        report.recentLogs = logs.recent
        report.breadcrumbs = breadcrumbs.recent
        #if os(iOS)
        report.deviceContext = DeviceContext.current()
        #endif

        onAutoReport?(report)
    }
}
