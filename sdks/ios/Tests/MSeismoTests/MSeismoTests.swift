import XCTest
@testable import MSeismo

final class MSeismoTests: XCTestCase {
    func testConfigureStoresConfiguration() {
        let config = Configuration(
            endpoint: URL(string: "https://seismo.example.com")!,
            projectKey: "test-key",
            environment: "staging"
        )

        MSeismo.shared.configure(config)

        XCTAssertEqual(MSeismo.shared.configuration?.projectKey, "test-key")
        XCTAssertEqual(MSeismo.shared.configuration?.environment, "staging")
    }
}
