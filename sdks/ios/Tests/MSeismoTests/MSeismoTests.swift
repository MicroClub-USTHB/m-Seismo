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
}
