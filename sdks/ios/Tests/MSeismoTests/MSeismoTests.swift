import XCTest
@testable import MSeismo

final class MSeismoTests: XCTestCase {
    func testConfigureStoresProjectKeyAndEnvironment() {
        MSeismo.shared.configure(projectKey: "test-key", environment: "staging")

        XCTAssertEqual(MSeismo.shared.projectKey, "test-key")
        XCTAssertEqual(MSeismo.shared.environment, "staging")
    }

    func testConfigureDefaultsEnvironmentToProduction() {
        MSeismo.shared.configure(projectKey: "test-key")

        XCTAssertEqual(MSeismo.shared.environment, "production")
    }
}
