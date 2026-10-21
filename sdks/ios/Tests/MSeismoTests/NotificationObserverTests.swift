import XCTest
@testable import MSeismo

final class NotificationObserverTests: XCTestCase {
    func testForwardsMatchingNotification() {
        let center = NotificationCenter()
        let name = Notification.Name("test.notification")
        var fired = 0
        let observer = NotificationObserver(name: name, center: center) { fired += 1 }

        observer.start()
        center.post(Notification(name: name))

        XCTAssertEqual(fired, 1)
    }

    func testStopPreventsFurtherNotifications() {
        let center = NotificationCenter()
        let name = Notification.Name("test.notification")
        var fired = 0
        let observer = NotificationObserver(name: name, center: center) { fired += 1 }

        observer.start()
        observer.stop()
        center.post(Notification(name: name))

        XCTAssertEqual(fired, 0)
    }

    func testIgnoresNonMatchingNotification() {
        let center = NotificationCenter()
        var fired = 0
        let observer = NotificationObserver(name: Notification.Name("a"), center: center) { fired += 1 }

        observer.start()
        center.post(Notification(name: Notification.Name("b")))

        XCTAssertEqual(fired, 0)
    }
}
