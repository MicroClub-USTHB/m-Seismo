import XCTest
@testable import MSeismo

final class LogBufferTests: XCTestCase {
    func testKeepsUpToMaxLines() {
        let buffer = LogBuffer(maxLines: 2)

        buffer.log("a")
        buffer.log("b")
        buffer.log("c")

        XCTAssertEqual(buffer.recent.map(\.message), ["b", "c"])
    }

    func testWithNoMaxAgeKeepsAllWithinLineLimit() {
        let buffer = LogBuffer(maxLines: 10)

        buffer.log("a")

        XCTAssertEqual(buffer.recent.map(\.message), ["a"])
    }

    func testMaxAgeDropsOlderLines() {
        var now = Date(timeIntervalSinceReferenceDate: 0)
        let buffer = LogBuffer(maxLines: 10, maxAge: 5, now: { now })

        buffer.log("old")
        now = now.addingTimeInterval(10)
        buffer.log("new")

        XCTAssertEqual(buffer.recent.map(\.message), ["new"])
    }
}
