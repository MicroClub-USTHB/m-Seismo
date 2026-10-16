import XCTest
@testable import MSeismo

private final class FakeTransport: ReportTransport {
    var results: [Result<Void, Error>] = [.success(())]
    private(set) var sentReports: [Report] = []

    func send(_ report: Report, to configuration: Configuration, completion: @escaping (Result<Void, Error>) -> Void) {
        sentReports.append(report)
        let result = results.count > 1 ? results.removeFirst() : (results.first ?? .success(()))
        completion(result)
    }
}

private struct FakeError: Error {}

final class OfflineQueueTests: XCTestCase {
    private let configuration = Configuration(
        endpoint: URL(string: "https://seismo.example.com/reports")!,
        projectKey: "test-key"
    )

    private func makeStore() -> OfflineQueueStore {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return OfflineQueueStore(directory: directory)
    }

    func testEnqueueSendsImmediatelyWhenTransportSucceeds() {
        let transport = FakeTransport()
        let queue = OfflineQueue(transport: transport, store: makeStore())
        queue.configuration = configuration

        let expectation = expectation(description: "drained")
        queue.onDrained = { expectation.fulfill() }
        queue.enqueue(Report(title: "First"))

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(transport.sentReports.map(\.title), ["First"])
        XCTAssertEqual(queue.pendingCount, 0)
    }

    func testDoesNotSendWithoutConfiguration() {
        let transport = FakeTransport()
        let queue = OfflineQueue(transport: transport, store: makeStore())

        queue.enqueue(Report(title: "First"))

        XCTAssertTrue(transport.sentReports.isEmpty)
        XCTAssertEqual(queue.pendingCount, 1)
    }

    func testFailureSchedulesARetryRatherThanDroppingTheReport() {
        let transport = FakeTransport()
        transport.results = [.failure(FakeError())]
        let queue = OfflineQueue(transport: transport, store: makeStore())
        queue.configuration = configuration

        let expectation = expectation(description: "attempted")
        queue.onDrained = { expectation.fulfill() }
        queue.enqueue(Report(title: "First"))

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(queue.pendingCount, 1)
        XCTAssertEqual(transport.sentReports.count, 1)
    }

    func testPersistsAcrossInstances() {
        let store = makeStore()
        let transport = FakeTransport()
        // No configuration yet, so this just persists without sending.
        let firstQueue = OfflineQueue(transport: transport, store: store)
        firstQueue.enqueue(Report(title: "Still here after relaunch"))

        let secondQueue = OfflineQueue(transport: FakeTransport(), store: store)

        XCTAssertEqual(secondQueue.pendingCount, 1)
    }
}
