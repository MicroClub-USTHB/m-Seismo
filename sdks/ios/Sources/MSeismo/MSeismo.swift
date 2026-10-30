import Combine
import Foundation

#if os(iOS)
import Network
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

    /// Recent network requests (FR-23). Call `networkLog.record(...)` from
    /// wherever the app already has access to its own requests/responses.
    public let networkLog = NetworkLogger()

    /// Strips PII from report text before it leaves the phone (FR-25).
    /// Configure `rules`, `allowList`, `denyList` directly on this.
    public let redactor = Redactor()

    /// Persistent delivery queue (FR-33/FR-34, SDK-10). `submit(_:)` is the
    /// normal way reports get here; exposed directly so an app can call
    /// `queue.retryNow()` on reconnect or `queue.pendingCount` for a
    /// "N reports waiting to send" indicator.
    public let queue = OfflineQueue(transport: URLSessionTransport())

    /// The app's active feature flags, sent with every report (FR-16).
    /// Set it when flags load or change; the SDK reads it when a report
    /// is made.
    public var featureFlags: [String: FlagValue] = [:]

    /// The experiments the user is in, as experiment name to variant,
    /// sent with every report (FR-16).
    public var experiments: [String: String] = [:]

    /// Crashlytics' installation ID, e.g. from
    /// `Installations.installations().installationID`. Sent with every
    /// report so the backend can link it to the Crashlytics crash. The
    /// SDK doesn't depend on Firebase: the app passes the ID in, and
    /// Crashlytics keeps receiving every crash as before.
    public var crashlyticsInstallationID: String?

    private let crashReporter = CrashReporter.shared

    /// Called with a crash/error report that was captured automatically
    /// (FR-08/FR-09) — no sheet is shown for these. Set this, then call
    /// `checkForPendingCrash()` once at launch.
    public var onAutoReport: ((Report) -> Void)?

    public let shakeDetector = ShakeDetector()

    #if os(iOS)
    private let screenshotObserver = NotificationObserver(name: UIApplication.userDidTakeScreenshotNotification)
    private var lifecycleObservers: [NotificationObserver] = []
    private let reachabilityMonitor = NWPathMonitor()
    #endif

    /// Retries the offline queue at a fixed interval, independent of
    /// reachability changes, so a retry eventually happens even without a
    /// clean "offline → online" transition to key off (FR-33: "retrying
    /// at growing intervals" — the queue's own per-item backoff already
    /// grows; this timer is just what re-checks it).
    private var retryTimer: Timer?

    /// Drives presentation of `ReportFormView`. An app observes this
    /// (`@ObservedObject var mseismo = MSeismo.shared`) and binds it to a
    /// `.sheet(isPresented:)`.
    @Published public var isReportSheetPresented = false

    /// What opened the report sheet and when, carried onto the report the
    /// sheet produces (schema `trigger` and `occurred_at`).
    private var sheetTrigger: Report.Trigger?
    private var sheetOpenedAt: Date?

    /// The real screen, captured at the moment of the trigger, before the
    /// report sheet appears (FR-19). An app reads this when constructing
    /// `ReportFormView`'s initial `Report`.
    @Published public private(set) var capturedScreenshot: Data?

    private init() {
        crashReporter.start()

        shakeDetector.onShake = { [weak self] in
            self?.report(trigger: .shake)
        }

        #if os(iOS)
        screenshotObserver.onNotify = { [weak self] in
            self?.report(trigger: .screenshot)
        }
        // Not started here: off by default until configure(_:) says
        // otherwise (Configuration.screenshotEnabled).

        observeLifecycle(UIApplication.didBecomeActiveNotification, message: "resumed")
        observeLifecycle(UIApplication.didEnterBackgroundNotification, message: "backgrounded")

        // "sent automatically when the connection returns" (FR-33).
        reachabilityMonitor.pathUpdateHandler = { [weak self] path in
            if path.status == .satisfied {
                self?.queue.retryNow()
            }
        }
        reachabilityMonitor.start(queue: DispatchQueue(label: "mseismo.reachability"))
        #endif

        retryTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            self?.queue.retryNow()
        }
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
        queue.configuration = configuration

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
    public func report(trigger: Report.Trigger = .manual) {
        sheetTrigger = trigger
        sheetOpenedAt = Date()
        #if os(iOS)
        capturedScreenshot = ScreenshotCapture.captureCurrentScreen()?.jpegData(compressionQuality: 0.8)
        #endif
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
        onAutoReport?(submit(crash.asReport()))
    }

    /// Attaches device context, session, logs, breadcrumbs, and network
    /// requests — everything a report gets regardless of how it was
    /// triggered. Shared by the manual-submit path (`ReportFormView`) and
    /// the automatic crash path (`checkForPendingCrash`).
    public func enrich(_ report: Report) -> Report {
        var report = report
        report.id = report.id ?? UUID().uuidString
        report.trigger = report.trigger ?? sheetTrigger ?? .manual
        report.occurredAt = report.occurredAt ?? sheetOpenedAt ?? Date()
        sheetTrigger = nil
        sheetOpenedAt = nil
        report.sessionID = session.id
        report.recentLogs = logs.recent
        report.breadcrumbs = breadcrumbs.recent
        report.networkRequests = networkLog.recent
        if report.featureFlags == nil, !featureFlags.isEmpty { report.featureFlags = featureFlags }
        if report.experiments == nil, !experiments.isEmpty { report.experiments = experiments }
        if report.crashlyticsInstallationID == nil { report.crashlyticsInstallationID = crashlyticsInstallationID }
        #if os(iOS)
        if report.deviceContext == nil {
            report.deviceContext = DeviceContext.current()
        }
        #endif
        return report
    }

    /// Queues a redacted, context-enriched report for delivery and
    /// returns the report as queued (FR-32: this call itself returns
    /// immediately — the actual network send happens in the background
    /// via `queue`, with offline retry per FR-33).
    @discardableResult
    public func submit(_ report: Report) -> Report {
        let final = redacted(enrich(report))
        queue.enqueue(final)
        return final
    }

    /// Applies `redactor` to every free-text field of a report (FR-25):
    /// title, description, each recorded log line/breadcrumb message, and
    /// any captured network request/response body.
    public func redacted(_ report: Report) -> Report {
        var report = report
        report.title = redactor.redact(report.title)
        report.description = redactor.redact(report.description)
        report.recentLogs = report.recentLogs?.map {
            LogLine(message: redactor.redact($0.message), timestamp: $0.timestamp)
        }
        report.breadcrumbs = report.breadcrumbs?.map {
            Breadcrumb(kind: $0.kind, message: redactor.redact($0.message), timestamp: $0.timestamp)
        }
        report.networkRequests = report.networkRequests?.map { entry in
            var entry = entry
            entry.requestBody = entry.requestBody.map { redactor.redact($0) }
            entry.responseBody = entry.responseBody.map { redactor.redact($0) }
            return entry
        }
        return report
    }
}
