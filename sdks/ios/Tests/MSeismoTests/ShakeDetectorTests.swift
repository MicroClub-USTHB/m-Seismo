import XCTest
@testable import MSeismo

final class ShakeDetectorTests: XCTestCase {
    func testFirstShakeAlwaysFires() {
        var fired = 0
        let detector = ShakeDetector(cooldown: 2.0, now: { Date(timeIntervalSinceReferenceDate: 0) })
        detector.onShake = { fired += 1 }

        detector.shakeDetected()

        XCTAssertEqual(fired, 1)
    }

    func testShakeWithinCooldownIsIgnored() {
        var fired = 0
        var clock = Date(timeIntervalSinceReferenceDate: 0)
        let detector = ShakeDetector(cooldown: 2.0, now: { clock })
        detector.onShake = { fired += 1 }

        detector.shakeDetected()
        clock = clock.addingTimeInterval(1.0)
        detector.shakeDetected()

        XCTAssertEqual(fired, 1)
    }

    func testShakeAfterCooldownFiresAgain() {
        var fired = 0
        var clock = Date(timeIntervalSinceReferenceDate: 0)
        let detector = ShakeDetector(cooldown: 2.0, now: { clock })
        detector.onShake = { fired += 1 }

        detector.shakeDetected()
        clock = clock.addingTimeInterval(2.1)
        detector.shakeDetected()

        XCTAssertEqual(fired, 2)
    }
}
