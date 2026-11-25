import XCTest
@testable import MSeismo

final class MSeismoTests: XCTestCase {
    func testConfigureStoresConfiguration() {
        let config = Configuration(
            baseURL: URL(string: "https://seismo.example.com")!,
            sdkKey: "test-key",
            environment: "staging"
        )

        MSeismo.shared.configure(config)

        XCTAssertEqual(MSeismo.shared.configuration?.sdkKey, "test-key")
        XCTAssertEqual(MSeismo.shared.configuration?.environment, "staging")
    }

    func testEnrichAddsTheAppsFlagsExperimentsAndCrashlyticsID() {
        let mseismo = MSeismo.shared
        mseismo.featureFlags = ["new_checkout": true]
        mseismo.experiments = ["pay_button_color": "green"]
        mseismo.crashlyticsInstallationID = "fid-123"
        defer {
            mseismo.featureFlags = [:]
            mseismo.experiments = [:]
            mseismo.crashlyticsInstallationID = nil
        }

        let report = mseismo.enrich(Report())

        XCTAssertEqual(report.featureFlags, ["new_checkout": true])
        XCTAssertEqual(report.experiments, ["pay_button_color": "green"])
        XCTAssertEqual(report.crashlyticsInstallationID, "fid-123")
    }

    func testEnrichKeepsWhatTheReportAlreadyCarries() {
        let mseismo = MSeismo.shared
        mseismo.featureFlags = ["from_app": true]
        defer { mseismo.featureFlags = [:] }

        let report = mseismo.enrich(Report(featureFlags: ["own": false]))

        XCTAssertEqual(report.featureFlags, ["own": false])
    }

    func testEnrichLeavesTheFieldsEmptyWhenTheAppSetNone() {
        let report = MSeismo.shared.enrich(Report())

        XCTAssertNil(report.featureFlags)
        XCTAssertNil(report.experiments)
        XCTAssertNil(report.crashlyticsInstallationID)
    }
}
