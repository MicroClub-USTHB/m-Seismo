import XCTest
@testable import MSeismo

final class RingBufferTests: XCTestCase {
    func testKeepsElementsUpToCapacity() {
        var buffer = RingBuffer<Int>(capacity: 3)

        buffer.append(1)
        buffer.append(2)
        buffer.append(3)

        XCTAssertEqual(buffer.elements, [1, 2, 3])
    }

    func testDropsOldestWhenOverCapacity() {
        var buffer = RingBuffer<Int>(capacity: 3)

        buffer.append(1)
        buffer.append(2)
        buffer.append(3)
        buffer.append(4)

        XCTAssertEqual(buffer.elements, [2, 3, 4])
    }

    func testZeroCapacityKeepsNothing() {
        var buffer = RingBuffer<Int>(capacity: 0)

        buffer.append(1)

        XCTAssertEqual(buffer.elements, [])
    }
}
