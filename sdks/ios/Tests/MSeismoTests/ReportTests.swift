import XCTest
@testable import MSeismo

final class ReportTests: XCTestCase {
    func testDefaultsAreAllOptionalFields() {
        let report = Report()

        XCTAssertEqual(report.title, "")
        XCTAssertEqual(report.description, "")
        XCTAssertEqual(report.category, .bug)
        XCTAssertEqual(report.severity, .medium)
        XCTAssertNil(report.screenshot)
    }
}
