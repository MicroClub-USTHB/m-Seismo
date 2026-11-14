import XCTest
@testable import MSeismo

final class InstallationTests: XCTestCase {
    private func makeDefaults() -> UserDefaults {
        UserDefaults(suiteName: UUID().uuidString)!
    }

    func testIDIsAUUIDAndStaysTheSame() {
        let installation = Installation(defaults: makeDefaults())

        XCTAssertNotNil(UUID(uuidString: installation.id))
        XCTAssertEqual(installation.id, installation.id)
    }

    func testIDSurvivesANewInstanceOnTheSameStorage() {
        let defaults = makeDefaults()

        XCTAssertEqual(Installation(defaults: defaults).id, Installation(defaults: defaults).id)
    }

    func testDifferentInstallationsGetDifferentIDs() {
        XCTAssertNotEqual(Installation(defaults: makeDefaults()).id, Installation(defaults: makeDefaults()).id)
    }
}
