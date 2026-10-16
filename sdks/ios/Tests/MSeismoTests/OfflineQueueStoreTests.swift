import XCTest
@testable import MSeismo

final class OfflineQueueStoreTests: XCTestCase {
    private func makeStore() -> OfflineQueueStore {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return OfflineQueueStore(directory: directory)
    }

    func testLoadReturnsEmptyWhenNothingSaved() {
        let store = makeStore()

        XCTAssertEqual(store.load(), [])
    }

    func testSaveThenLoadRoundTrips() {
        let store = makeStore()
        let item = QueuedReport(id: UUID(), report: Report(title: "Broken"), attempts: 1, nextAttemptAt: Date())

        store.save([item])

        XCTAssertEqual(store.load(), [item])
    }
}
