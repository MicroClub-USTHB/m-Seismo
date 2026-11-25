import Flutter
import Foundation
import UIKit

/// Thin Flutter bridge over the native m-Seismo SDK. The native sources are
/// compiled into this pod's module, so there is no `import MSeismo`; both
/// layers share one SDK instance.
public final class MSeismoPlugin: NSObject, FlutterPlugin, MSeismoHostApi {
    private let sheetPresenter = ReportSheetPresenter()
    private let shakeMonitor = ShakeMonitor()
    private var flutterApi: MSeismoFlutterApi?

    public static func register(with registrar: FlutterPluginRegistrar) {
        let instance = MSeismoPlugin()
        MSeismoHostApiSetup.setUp(
            binaryMessenger: registrar.messenger(),
            api: instance
        )

        let flutterApi = MSeismoFlutterApi(binaryMessenger: registrar.messenger())
        instance.flutterApi = flutterApi
        MSeismo.shared.onAutoReport = { [weak flutterApi] report in
            guard let flutterApi = flutterApi else { return }
            flutterApi.onAutoReport(
                report: TypeMapping.autoReport(from: report)
            ) { _ in }
        }

        instance.sheetPresenter.start()
        registrar.addApplicationDelegate(instance)
    }

    public func detachFromEngine(for registrar: FlutterPluginRegistrar) {
        MSeismo.shared.onAutoReport = nil
        MSeismoHostApiSetup.setUp(
            binaryMessenger: registrar.messenger(),
            api: nil
        )
        sheetPresenter.stop()
        shakeMonitor.stop()
        flutterApi = nil
    }

    func configure(config: MSeismoConfig) throws {
        let rawBaseURL = config.baseUrl
        guard !rawBaseURL.isEmpty,
              rawBaseURL == rawBaseURL.trimmingCharacters(in: .whitespacesAndNewlines),
              !rawBaseURL.contains(where: { $0.isWhitespace }),
              let components = URLComponents(string: rawBaseURL),
              let scheme = components.scheme, !scheme.isEmpty,
              let host = components.host, !host.isEmpty,
              let baseURL = components.url
        else {
            throw PigeonError(
                code: "invalid-configuration",
                message: "Invalid m-Seismo base URL: \(config.baseUrl)",
                details: nil
            )
        }

        let configuration = Configuration(
            baseURL: baseURL,
            sdkKey: config.sdkKey,
            environment: config.environment,
            release: config.release,
            shakeEnabled: config.shakeEnabled,
            screenshotEnabled: config.screenshotEnabled
        )
        MSeismo.shared.configure(configuration)

        if config.shakeEnabled {
            shakeMonitor.start()
        } else {
            shakeMonitor.stop()
        }
    }

    func report(trigger: ReportTrigger) throws {
        MSeismo.shared.report(trigger: TypeMapping.trigger(trigger))
    }

    func submit(draft: ReportDraft) throws -> String {
        let report = TypeMapping.report(from: draft)
        return MSeismo.shared.submit(report).id ?? ""
    }

    func recordError(message: String, stackTrace: String?, fatal: Bool) throws {
        let frames = stackTrace?
            .split(separator: "\n")
            .map(String.init) ?? []
        CrashStore().save(
            CrashReport(
                name: "FlutterError",
                reason: message,
                callStack: frames,
                isFatal: fatal
            )
        )
    }

    func checkForPendingCrash() throws {
        MSeismo.shared.checkForPendingCrash()
    }

    func log(message: String) throws {
        MSeismo.shared.logs.log(message)
    }

    func breadcrumb(kind: BreadcrumbKind, message: String) throws {
        MSeismo.shared.breadcrumbs.record(
            TypeMapping.breadcrumbKind(kind),
            message
        )
    }

    func recordNetworkRequest(request: NetworkRequest) throws {
        MSeismo.shared.networkLog.record(
            url: request.url,
            method: request.method,
            statusCode: request.statusCode.map { Int($0) },
            duration: Double(request.durationMs) / 1000,
            requestBody: request.requestBody,
            responseBody: request.responseBody
        )
    }

    func setCapturesNetworkBodies(enabled: Bool) throws {
        MSeismo.shared.networkLog.capturesBodies = enabled
    }

    func setFeatureFlags(flags: [String: Any?]) throws {
        MSeismo.shared.featureFlags = TypeMapping.flagValues(from: flags)
    }

    func setExperiments(experiments: [String: String]) throws {
        MSeismo.shared.experiments = experiments
    }

    func setCrashlyticsInstallationId(id: String?) throws {
        MSeismo.shared.crashlyticsInstallationID = id
    }

    func setRedactionAllowList(values: [String]) throws {
        MSeismo.shared.redactor.allowList = values
    }

    func setRedactionDenyList(values: [String]) throws {
        MSeismo.shared.redactor.denyList = values
    }

    func pendingReportCount() throws -> Int64 {
        Int64(MSeismo.shared.queue.pendingCount)
    }

    func retryNow() throws {
        MSeismo.shared.queue.retryNow()
    }

    func getInstallationId() throws -> String {
        Installation.shared.id
    }

    func getSessionId() throws -> String {
        MSeismo.shared.session.id
    }

    func newTraceId() throws -> String {
        MSeismo.shared.session.newTraceID()
    }
}
