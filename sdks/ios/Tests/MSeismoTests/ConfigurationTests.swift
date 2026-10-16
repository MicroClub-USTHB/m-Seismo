import XCTest
@testable import MSeismo

final class ConfigurationTests: XCTestCase {
    private let endpoint = URL(string: "https://seismo.example.com")!

    func testStoresEndpointAndProjectKey() {
        let config = Configuration(endpoint: endpoint, projectKey: "test-key")

        XCTAssertEqual(config.endpoint, endpoint)
        XCTAssertEqual(config.projectKey, "test-key")
    }

    func testDefaultsEnvironmentToProduction() {
        let config = Configuration(endpoint: endpoint, projectKey: "test-key")

        XCTAssertEqual(config.environment, "production")
    }

    func testEnvironmentCanBeOverridden() {
        let config = Configuration(endpoint: endpoint, projectKey: "test-key", environment: "staging")

        XCTAssertEqual(config.environment, "staging")
    }

    func testReleaseDefaultsToNilWhenNotGiven() {
        let config = Configuration(endpoint: endpoint, projectKey: "test-key")

        XCTAssertNil(config.release)
    }

    func testReleaseCanBeSetExplicitly() {
        let config = Configuration(endpoint: endpoint, projectKey: "test-key", release: "1.2.3 (45)")

        XCTAssertEqual(config.release, "1.2.3 (45)")
    }
}
