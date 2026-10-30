import XCTest
@testable import MSeismo

final class ReportPayloadTests: XCTestCase {
    private let configuration = Configuration(
        baseURL: URL(string: "https://seismo.example.com")!,
        sdkKey: "msk_test",
        environment: "staging"
    )
    private let installationID = "5B8F2C1E-9A4D-4E7B-8C3F-1D2E6A9B0C47"

    private func json(_ report: Report) throws -> [String: Any] {
        let payload = ReportPayload(report: report, configuration: configuration, installationID: installationID)
        let object = try JSONSerialization.jsonObject(with: payload.encoded())
        return try XCTUnwrap(object as? [String: Any])
    }

    private var context: DeviceContext {
        DeviceContext(
            osVersion: "18.1",
            deviceModel: "iPhone15,2",
            locale: "fr_DZ",
            batteryLevel: 0.62,
            availableMemoryBytes: 1_073_741_824,
            networkType: "wifi",
            orientation: "portrait",
            appVersion: "2.4.0",
            appBuild: "412"
        )
    }

    func testRequiredFieldsAreAllPresent() throws {
        var report = Report(title: "Checkout button does nothing", deviceContext: context, sessionID: "session-1")
        report.id = "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51"
        report.trigger = .shake
        report.occurredAt = Date(timeIntervalSince1970: 1_792_332_192.123)

        let object = try json(report)

        for key in ["schema_version", "id", "installation_id", "session_id", "occurred_at",
                    "platform", "sdk", "environment", "release", "trigger", "device"] {
            XCTAssertNotNil(object[key], "missing required field \(key)")
        }
        XCTAssertEqual(object["schema_version"] as? Int, 1)
        XCTAssertEqual(object["platform"] as? String, "ios")
        XCTAssertEqual(object["environment"] as? String, "staging")
        XCTAssertEqual(object["trigger"] as? String, "shake")
        XCTAssertEqual(object["installation_id"] as? String, installationID)
        XCTAssertEqual((object["sdk"] as? [String: Any])?["name"] as? String, "mseismo-ios")
        XCTAssertEqual((object["release"] as? [String: Any])?["version"] as? String, "2.4.0")
        XCTAssertEqual((object["release"] as? [String: Any])?["build"] as? String, "412")
    }

    func testTimestampsAreUTCWithMilliseconds() {
        let date = Date(timeIntervalSince1970: 1_000.5)

        XCTAssertEqual(ReportPayload.timestamp(date), "1970-01-01T00:16:40.500Z")
    }

    func testDeviceUsesSchemaNamesAndValues() throws {
        let object = try json(Report(deviceContext: context))
        let device = try XCTUnwrap(object["device"] as? [String: Any])

        XCTAssertEqual(device["os_name"] as? String, "iOS")
        XCTAssertEqual(device["os_version"] as? String, "18.1")
        XCTAssertEqual(device["model"] as? String, "iPhone15,2")
        XCTAssertEqual(device["locale"] as? String, "fr-DZ")
        XCTAssertEqual(device["network_type"] as? String, "wifi")
        XCTAssertEqual(device["orientation"] as? String, "portrait")
        XCTAssertEqual(device["available_memory_bytes"] as? Int, 1_073_741_824)
    }

    func testDeviceValuesOutsideTheSchemaAreMapped() throws {
        var other = context
        other.networkType = "other"
        other.orientation = "faceUp"
        var offline = context
        offline.networkType = "offline"
        offline.orientation = "landscapeLeft"

        let otherDevice = try XCTUnwrap(try json(Report(deviceContext: other))["device"] as? [String: Any])
        let offlineDevice = try XCTUnwrap(try json(Report(deviceContext: offline))["device"] as? [String: Any])

        XCTAssertEqual(otherDevice["network_type"] as? String, "unknown")
        XCTAssertEqual(otherDevice["orientation"] as? String, "unknown")
        XCTAssertEqual(offlineDevice["network_type"] as? String, "none")
        XCTAssertEqual(offlineDevice["orientation"] as? String, "landscape")
    }

    func testMissingDeviceContextStillSatisfiesTheRequiredDeviceFields() throws {
        let object = try json(Report())
        let device = try XCTUnwrap(object["device"] as? [String: Any])

        XCTAssertEqual(device["os_name"] as? String, "iOS")
        XCTAssertEqual(device["os_version"] as? String, "unknown")
        XCTAssertEqual(device["model"] as? String, "unknown")
        XCTAssertEqual((object["release"] as? [String: Any])?["version"] as? String, "unknown")
    }

    func testUserInputCarriesWhatTheReporterTyped() throws {
        let report = Report(title: "Broken", description: "Tapped twice", category: .ui, severity: .high)

        let input = try XCTUnwrap(try json(report)["user_input"] as? [String: Any])

        XCTAssertEqual(input["title"] as? String, "Broken")
        XCTAssertEqual(input["description"] as? String, "Tapped twice")
        XCTAssertEqual(input["category"] as? String, "ui")
        XCTAssertEqual(input["severity"] as? String, "high")
    }

    func testEmptyTitleAndDescriptionAreLeftOut() throws {
        let input = try XCTUnwrap(try json(Report())["user_input"] as? [String: Any])

        XCTAssertNil(input["title"])
        XCTAssertNil(input["description"])
    }

    func testLongTextIsShortenedToTheSchemaLimits() throws {
        let report = Report(title: String(repeating: "t", count: 500), description: String(repeating: "d", count: 20_000))

        let input = try XCTUnwrap(try json(report)["user_input"] as? [String: Any])

        XCTAssertEqual((input["title"] as? String)?.count, 200)
        XCTAssertEqual((input["description"] as? String)?.count, 10_000)
    }

    func testCrashReportCarriesCrashDetailsAndIsAutoSubmitted() throws {
        let crash = CrashReport(name: "NSRangeException", reason: "boom", callStack: ["frame 1", "frame 2"], isFatal: true)

        let object = try json(crash.asReport())
        let details = try XCTUnwrap(object["crash"] as? [String: Any])

        XCTAssertEqual(object["trigger"] as? String, "crash")
        XCTAssertEqual(object["auto_submitted"] as? Bool, true)
        XCTAssertNil(object["user_input"])
        XCTAssertEqual(details["type"] as? String, "NSRangeException")
        XCTAssertEqual(details["reason"] as? String, "boom")
        XCTAssertEqual(details["stack_trace"] as? [String], ["frame 1", "frame 2"])
    }

    func testCrashWithoutAStackStillHasTheOneFrameTheSchemaRequires() throws {
        let crash = CrashReport(name: "SIGSEGV", reason: nil, callStack: [], isFatal: true)

        let details = try XCTUnwrap(try json(crash.asReport())["crash"] as? [String: Any])

        XCTAssertEqual((details["stack_trace"] as? [String])?.count, 1)
    }

    func testLogsBreadcrumbsAndNetworkRequestsUseSchemaShapes() throws {
        let when = Date(timeIntervalSince1970: 1_000)
        var report = Report()
        report.recentLogs = [LogLine(message: "Cart loaded", timestamp: when)]
        report.breadcrumbs = [Breadcrumb(kind: .tap, message: "Pay button", timestamp: when)]
        report.networkRequests = [NetworkRequestLog(url: "https://api.example.com/pay", method: "POST", statusCode: 502, duration: 0.25, timestamp: when)]

        let object = try json(report)
        let log = try XCTUnwrap((object["logs"] as? [[String: Any]])?.first)
        let crumb = try XCTUnwrap((object["breadcrumbs"] as? [[String: Any]])?.first)
        let request = try XCTUnwrap((object["network_requests"] as? [[String: Any]])?.first)

        XCTAssertEqual(log["message"] as? String, "Cart loaded")
        XCTAssertEqual(log["timestamp"] as? String, "1970-01-01T00:16:40.000Z")
        XCTAssertEqual(crumb["kind"] as? String, "tap")
        XCTAssertEqual(request["duration_ms"] as? Int, 250)
        XCTAssertEqual(request["status_code"] as? Int, 502)
        XCTAssertEqual(object["bodies_captured"] as? Bool, false)
    }

    func testBodiesCapturedIsTrueOnlyWhenABodyIsPresent() throws {
        var report = Report()
        report.networkRequests = [NetworkRequestLog(url: "https://api.example.com", method: "POST", statusCode: 200, duration: 0.1, requestBody: "{}")]

        XCTAssertEqual(try json(report)["bodies_captured"] as? Bool, true)
    }

    func testReportWithoutATriggerIsManual() throws {
        XCTAssertEqual(try json(Report())["trigger"] as? String, "manual")
        XCTAssertEqual(try json(Report())["auto_submitted"] as? Bool, false)
    }

    func testFeatureFlagsKeepTheirTypes() throws {
        let report = Report(featureFlags: ["new_checkout": true, "max_items": 20, "theme": "dark"])

        let flags = try XCTUnwrap(try json(report)["feature_flags"] as? [String: Any])

        XCTAssertEqual(flags["new_checkout"] as? Bool, true)
        XCTAssertEqual(flags["max_items"] as? Int, 20)
        XCTAssertEqual(flags["theme"] as? String, "dark")
    }

    func testExperimentsAreSentAsNameToVariant() throws {
        let report = Report(experiments: ["pay_button_color": "green"])

        let experiments = try XCTUnwrap(try json(report)["experiments"] as? [String: String])

        XCTAssertEqual(experiments, ["pay_button_color": "green"])
    }

    func testNoFlagsOrExperimentsLeavesTheFieldsOut() throws {
        let object = try json(Report(featureFlags: [:], experiments: [:]))

        XCTAssertNil(object["feature_flags"])
        XCTAssertNil(object["experiments"])
        XCTAssertNil(object["crashlytics"])
    }

    func testFlagsAndExperimentsAreCappedAtTheSchemaLimits() throws {
        var flags: [String: FlagValue] = [:]
        var experiments: [String: String] = [:]
        for index in 0..<300 {
            flags[String(format: "flag_%03d", index)] = true
            experiments[String(format: "exp_%03d", index)] = String(repeating: "v", count: 200)
        }

        let object = try json(Report(featureFlags: flags, experiments: experiments))
        let sentFlags = try XCTUnwrap(object["feature_flags"] as? [String: Any])
        let sentExperiments = try XCTUnwrap(object["experiments"] as? [String: String])

        XCTAssertEqual(sentFlags.count, 200)
        XCTAssertEqual(sentExperiments.count, 100)
        XCTAssertEqual(sentExperiments.values.first?.count, 128)
        // The cut is by name, so it is the same on every retry.
        XCTAssertNotNil(sentFlags["flag_000"])
        XCTAssertNil(sentFlags["flag_299"])
    }

    func testNumbersTheJSONCannotHoldAreLeftOut() throws {
        let report = Report(featureFlags: ["ok": 1.5, "bad": .number(.nan), "inf": .number(.infinity)])

        let flags = try XCTUnwrap(try json(report)["feature_flags"] as? [String: Any])

        XCTAssertEqual(Array(flags.keys), ["ok"])
    }

    func testCrashlyticsInstallationIDIsSentWhenPresent() throws {
        let crashlytics = try XCTUnwrap(try json(Report(crashlyticsInstallationID: "fid-123"))["crashlytics"] as? [String: Any])

        XCTAssertEqual(crashlytics["installation_id"] as? String, "fid-123")
    }

    func testEmptyCrashlyticsInstallationIDIsLeftOut() throws {
        XCTAssertNil(try json(Report(crashlyticsInstallationID: ""))["crashlytics"])
    }
}
