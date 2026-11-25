import XCTest
@testable import MSeismo

final class ConfigurationTests: XCTestCase {
    private let endpoint = URL(string: "https://seismo.example.com")!

    func testStoresBaseURLAndSDKKey() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key")

        XCTAssertEqual(config.baseURL, endpoint)
        XCTAssertEqual(config.sdkKey, "test-key")
    }

    func testDefaultsEnvironmentToProduction() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key")

        XCTAssertEqual(config.environment, "production")
    }

    func testEnvironmentCanBeOverridden() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key", environment: "staging")

        XCTAssertEqual(config.environment, "staging")
    }

    func testReleaseDefaultsToNilWhenNotGiven() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key")

        XCTAssertNil(config.release)
    }

    func testReleaseCanBeSetExplicitly() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key", release: "1.2.3 (45)")

        XCTAssertEqual(config.release, "1.2.3 (45)")
    }

    func testShakeEnabledDefaultsToTrue() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key")

        XCTAssertTrue(config.shakeEnabled)
    }

    func testScreenshotEnabledDefaultsToFalse() {
        let config = Configuration(baseURL: endpoint, sdkKey: "test-key")

        XCTAssertFalse(config.screenshotEnabled)
    }

    func testTriggerFlagsCanBeOverridden() {
        let config = Configuration(
            baseURL: endpoint,
            sdkKey: "test-key",
            shakeEnabled: false,
            screenshotEnabled: true
        )

        XCTAssertFalse(config.shakeEnabled)
        XCTAssertTrue(config.screenshotEnabled)
    }

    func testReportsURLAddsTheIngestionPath() {
        let config = Configuration(baseURL: endpoint, sdkKey: "msk_test")

        XCTAssertEqual(config.reportsURL.absoluteString, "https://seismo.example.com/api/v1/reports")
    }

    func testReportsURLIgnoresATrailingSlash() {
        let config = Configuration(baseURL: URL(string: "https://seismo.example.com/")!, sdkKey: "msk_test")

        XCTAssertEqual(config.reportsURL.absoluteString, "https://seismo.example.com/api/v1/reports")
    }
}
