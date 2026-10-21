import XCTest
@testable import MSeismo

final class BreadcrumbTrackerTests: XCTestCase {
    func testRecordsInOrder() {
        let tracker = BreadcrumbTracker(capacity: 10)

        tracker.record(.navigation, "Opened SettingsScreen")
        tracker.record(.tap, "Tapped SaveButton")

        XCTAssertEqual(tracker.recent.map(\.message), ["Opened SettingsScreen", "Tapped SaveButton"])
        XCTAssertEqual(tracker.recent.map(\.kind), [.navigation, .tap])
    }

    func testRespectsCapacity() {
        let tracker = BreadcrumbTracker(capacity: 2)

        tracker.record(.lifecycle, "opened")
        tracker.record(.lifecycle, "backgrounded")
        tracker.record(.lifecycle, "resumed")

        XCTAssertEqual(tracker.recent.map(\.message), ["backgrounded", "resumed"])
    }
}
