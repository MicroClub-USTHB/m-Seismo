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
        baseURL: URL(string: "https://seismo.example.com")!,
        sdkKey: "test-key"
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

    func testRejectedReportIsDroppedAndDoesNotBlockTheNextOne() {
        let transport = FakeTransport()
        transport.results = [.failure(TransportError.rejected(400)), .success(())]
        let queue = OfflineQueue(transport: transport, store: makeStore())
        queue.configuration = configuration

        let expectation = expectation(description: "drained")
        expectation.expectedFulfillmentCount = 2
        queue.onDrained = { expectation.fulfill() }
        queue.enqueue(Report(title: "Bad"))
        queue.enqueue(Report(title: "Good"))

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(transport.sentReports.map(\.title), ["Bad", "Good"])
        XCTAssertEqual(queue.pendingCount, 0)
    }

    func testRetriableStatusKeepsTheReportQueued() {
        let transport = FakeTransport()
        transport.results = [.failure(TransportError.badStatus(503))]
        let queue = OfflineQueue(transport: transport, store: makeStore())
        queue.configuration = configuration

        let expectation = expectation(description: "attempted")
        queue.onDrained = { expectation.fulfill() }
        queue.enqueue(Report(title: "First"))

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(queue.pendingCount, 1)
    }

    func testRetryAfterDelaysTheNextAttemptBeyondTheBackoff() {
        let transport = FakeTransport()
        transport.results = [.failure(TransportError.badStatus(429, retryAfter: 120)), .success(())]
        var clock = Date(timeIntervalSince1970: 1_000)
        let queue = OfflineQueue(transport: transport, store: makeStore(), now: { clock }, jitter: { _ in 0 })
        queue.configuration = configuration

        let first = expectation(description: "first attempt")
        queue.onDrained = { first.fulfill() }
        queue.enqueue(Report(title: "Limited"))
        wait(for: [first], timeout: 2)

        // Backoff alone would be 2 s; the server asked for 120 s.
        clock = clock.addingTimeInterval(60)
        queue.retryNow()
        let barrier = expectation(description: "queue idle")
        DispatchQueue.global().asyncAfter(deadline: .now() + 0.2) { barrier.fulfill() }
        wait(for: [barrier], timeout: 2)
        XCTAssertEqual(transport.sentReports.count, 1)

        clock = clock.addingTimeInterval(61)
        let second = expectation(description: "second attempt")
        queue.onDrained = { second.fulfill() }
        queue.retryNow()
        wait(for: [second], timeout: 2)
        XCTAssertEqual(transport.sentReports.count, 2)
        XCTAssertEqual(queue.pendingCount, 0)
    }

    func testReportKeepsTheSameIDAcrossRetries() {
        let transport = FakeTransport()
        transport.results = [.failure(TransportError.badStatus(503)), .success(())]
        var clock = Date(timeIntervalSince1970: 1_000)
        let queue = OfflineQueue(transport: transport, store: makeStore(), now: { clock }, jitter: { _ in 0 })
        queue.configuration = configuration

        let first = expectation(description: "first attempt")
        queue.onDrained = { first.fulfill() }
        queue.enqueue(Report(title: "Flaky"))
        wait(for: [first], timeout: 2)

        clock = clock.addingTimeInterval(600)
        let second = expectation(description: "second attempt")
        queue.onDrained = { second.fulfill() }
        queue.retryNow()
        wait(for: [second], timeout: 2)

        XCTAssertEqual(transport.sentReports.count, 2)
        XCTAssertNotNil(transport.sentReports[0].id)
        XCTAssertEqual(transport.sentReports[0].id, transport.sentReports[1].id)
    }
}
