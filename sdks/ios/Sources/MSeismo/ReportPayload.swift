import Foundation

/// The `report` part of an ingestion request, following
/// `spec/report.schema.json` (version 1). `Report` is the SDK's own model;
/// this is the wire format built from it at send time, so the model can
/// change without changing what the backend receives.
struct ReportPayload: Encodable {
    struct SDK: Encodable {
        var name: String
        var version: String
    }

    struct Release: Encodable {
        var version: String
        var build: String
    }

    struct UserInput: Encodable {
        var title: String?
        var description: String?
        var category: String
        var severity: String
    }

    struct Device: Encodable {
        var osName: String
        var osVersion: String
        var model: String
        var locale: String?
        var batteryLevel: Double?
        var availableMemoryBytes: Int?
        var networkType: String
        var orientation: String

        enum CodingKeys: String, CodingKey {
            case osName = "os_name"
            case osVersion = "os_version"
            case model, locale
            case batteryLevel = "battery_level"
            case availableMemoryBytes = "available_memory_bytes"
            case networkType = "network_type"
            case orientation
        }
    }

    struct Log: Encodable {
        var timestamp: String
        var message: String
    }

    struct Breadcrumb: Encodable {
        var timestamp: String
        var kind: String
        var message: String
    }

    struct Crash: Encodable {
        var type: String
        var reason: String?
        var stackTrace: [String]

        enum CodingKeys: String, CodingKey {
            case type, reason
            case stackTrace = "stack_trace"
        }
    }

    struct NetworkRequest: Encodable {
        var timestamp: String
        var url: String
        var method: String
        var statusCode: Int?
        var durationMs: Int
        var requestBody: String?
        var responseBody: String?

        enum CodingKeys: String, CodingKey {
            case timestamp, url, method
            case statusCode = "status_code"
            case durationMs = "duration_ms"
            case requestBody = "request_body"
            case responseBody = "response_body"
        }
    }

    var schemaVersion = 1
    var id: String
    var installationID: String
    var sessionID: String
    var occurredAt: String
    var platform = "ios"
    var sdk: SDK
    var environment: String
    var release: Release
    var trigger: String
    var autoSubmitted: Bool
    var userInput: UserInput?
    var device: Device
    var logs: [Log]?
    var breadcrumbs: [Breadcrumb]?
    var crash: Crash?
    var networkRequests: [NetworkRequest]?
    var bodiesCaptured: Bool

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case id
        case installationID = "installation_id"
        case sessionID = "session_id"
        case occurredAt = "occurred_at"
        case platform, sdk, environment, release, trigger
        case autoSubmitted = "auto_submitted"
        case userInput = "user_input"
        case device, logs, breadcrumbs, crash
        case networkRequests = "network_requests"
        case bodiesCaptured = "bodies_captured"
    }

    static let sdkName = "mseismo-ios"
    static let sdkVersion = "0.1.0"

    /// Placeholder for a required string the device could not supply, so
    /// the report is still accepted (every required string needs at least
    /// one character).
    private static let unknown = "unknown"

    init(report: Report, configuration: Configuration, installationID: String, now: Date = Date()) {
        let trigger = report.trigger ?? .manual
        let isCrash = trigger == .crash || trigger == .nonFatal
        let context = report.deviceContext

        id = report.id ?? UUID().uuidString
        self.installationID = installationID
        sessionID = nonEmpty(report.sessionID) ?? UUID().uuidString
        occurredAt = Self.timestamp(report.occurredAt ?? now)
        sdk = SDK(name: Self.sdkName, version: Self.sdkVersion)
        environment = clamp(nonEmpty(configuration.environment) ?? Self.unknown, 64)
        release = Release(
            version: clamp(nonEmpty(context?.appVersion) ?? Self.unknown, 64),
            build: clamp(nonEmpty(context?.appBuild) ?? Self.unknown, 64)
        )
        self.trigger = trigger.rawValue
        autoSubmitted = isCrash

        // A crash report carries its details in `crash`; the title and
        // description the SDK makes up for it would only repeat them.
        if !isCrash {
            userInput = UserInput(
                title: nonEmpty(report.title).map { clamp($0, 200) },
                description: nonEmpty(report.description).map { clamp($0, 10_000) },
                category: report.category.rawValue,
                severity: report.severity.rawValue
            )
        }

        device = Device(
            osName: "iOS",
            osVersion: clamp(nonEmpty(context?.osVersion) ?? Self.unknown, 32),
            model: clamp(nonEmpty(context?.deviceModel) ?? Self.unknown, 64),
            locale: nonEmpty(context?.locale).map { clamp($0.replacingOccurrences(of: "_", with: "-"), 35) },
            batteryLevel: context?.batteryLevel.map { min(1, max(0, Double($0))) },
            availableMemoryBytes: context.map { Int(clamping: $0.availableMemoryBytes) },
            networkType: Self.networkType(context?.networkType),
            orientation: Self.orientation(context?.orientation)
        )

        logs = report.recentLogs.map { lines in
            lines.suffix(500).map {
                Log(timestamp: Self.timestamp($0.timestamp), message: clamp($0.message, 4000))
            }
        }
        breadcrumbs = report.breadcrumbs.map { crumbs in
            crumbs.suffix(200).map {
                Breadcrumb(timestamp: Self.timestamp($0.timestamp), kind: $0.kind.rawValue, message: clamp($0.message, 1000))
            }
        }

        if isCrash, let crash = report.crash {
            self.crash = Crash(
                type: clamp(nonEmpty(crash.name) ?? Self.unknown, 256),
                reason: nonEmpty(crash.reason).map { clamp($0, 4000) },
                // The schema requires at least one frame.
                stackTrace: crash.callStack.isEmpty
                    ? ["(no stack trace available)"]
                    : crash.callStack.prefix(512).map { clamp($0, 1000) }
            )
        }

        var capturedBodies = false
        networkRequests = report.networkRequests.map { requests in
            requests.suffix(100).map { entry in
                capturedBodies = capturedBodies || entry.requestBody != nil || entry.responseBody != nil
                return NetworkRequest(
                    timestamp: Self.timestamp(entry.timestamp),
                    url: clamp(entry.url, 2048),
                    method: clamp(nonEmpty(entry.method) ?? "GET", 16),
                    statusCode: entry.statusCode.flatMap { (100...599).contains($0) ? $0 : nil },
                    durationMs: max(0, Int((entry.duration * 1000).rounded())),
                    requestBody: entry.requestBody.map { clamp($0, 16_000) },
                    responseBody: entry.responseBody.map { clamp($0, 16_000) }
                )
            }
        }
        bodiesCaptured = capturedBodies
    }

    func encoded() throws -> Data {
        try JSONEncoder().encode(self)
    }

    /// RFC 3339 in UTC with milliseconds, e.g. `2026-10-18T14:03:12.123Z`.
    static func timestamp(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        formatter.timeZone = TimeZone(identifier: "UTC")
        return formatter.string(from: date)
    }

    /// The device captures a free-form string; the schema allows a fixed set.
    private static func networkType(_ value: String?) -> String {
        switch value {
        case "wifi", "cellular", "ethernet": return value!
        case "offline", "none": return "none"
        default: return "unknown"
        }
    }

    private static func orientation(_ value: String?) -> String {
        switch value {
        case "portrait", "portraitUpsideDown": return "portrait"
        case "landscapeLeft", "landscapeRight", "landscape": return "landscape"
        default: return "unknown"
        }
    }
}

private func nonEmpty(_ value: String?) -> String? {
    guard let value, !value.isEmpty else { return nil }
    return value
}

/// Cuts a string to at most `limit` characters as the schema counts them
/// (Unicode code points), so a long value is shortened instead of making
/// the backend reject the whole report.
private func clamp(_ value: String, _ limit: Int) -> String {
    guard value.unicodeScalars.count > limit else { return value }
    var scalars = String.UnicodeScalarView()
    scalars.append(contentsOf: value.unicodeScalars.prefix(limit))
    return String(scalars)
}
