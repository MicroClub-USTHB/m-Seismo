import XCTest
@testable import MSeismo

final class SessionTests: XCTestCase {
    func testGeneratesAUUIDFormattedID() {
        let session = Session()

        XCTAssertNotNil(UUID(uuidString: session.id))
    }

    func testEachSessionGetsADifferentID() {
        let first = Session()
        let second = Session()

        XCTAssertNotEqual(first.id, second.id)
    }

    func testTraceIDIsAlsoUUIDFormatted() {
        let session = Session()

        XCTAssertNotNil(UUID(uuidString: session.newTraceID()))
    }

    func testEachCallToNewTraceIDIsDifferent() {
        let session = Session()

        XCTAssertNotEqual(session.newTraceID(), session.newTraceID())
    }
}
