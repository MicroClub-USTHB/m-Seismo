import XCTest
@testable import MSeismo

final class NetworkLoggerTests: XCTestCase {
    func testRecordsRequestMetadata() {
        let logger = NetworkLogger()

        logger.record(url: "https://api.example.com/reports", method: "POST", statusCode: 201, duration: 0.42)

        XCTAssertEqual(logger.recent.count, 1)
        let entry = logger.recent[0]
        XCTAssertEqual(entry.url, "https://api.example.com/reports")
        XCTAssertEqual(entry.method, "POST")
        XCTAssertEqual(entry.statusCode, 201)
        XCTAssertEqual(entry.duration, 0.42)
    }

    func testBodiesAreExcludedByDefault() {
        let logger = NetworkLogger()

        logger.record(
            url: "https://api.example.com/login",
            method: "POST",
            statusCode: 200,
            duration: 0.1,
            requestBody: "{\"password\":\"secret\"}",
            responseBody: "{\"token\":\"abc\"}"
        )

        let entry = logger.recent[0]
        XCTAssertNil(entry.requestBody)
        XCTAssertNil(entry.responseBody)
    }

    func testBodiesAreIncludedWhenExplicitlyEnabled() {
        let logger = NetworkLogger()
        logger.capturesBodies = true

        logger.record(
            url: "https://api.example.com/login",
            method: "POST",
            statusCode: 200,
            duration: 0.1,
            requestBody: "{\"password\":\"secret\"}",
            responseBody: "{\"token\":\"abc\"}"
        )

        let entry = logger.recent[0]
        XCTAssertEqual(entry.requestBody, "{\"password\":\"secret\"}")
        XCTAssertEqual(entry.responseBody, "{\"token\":\"abc\"}")
    }

    func testRespectsCapacity() {
        let logger = NetworkLogger(capacity: 2)

        logger.record(url: "a", method: "GET", statusCode: 200, duration: 0.1)
        logger.record(url: "b", method: "GET", statusCode: 200, duration: 0.1)
        logger.record(url: "c", method: "GET", statusCode: 200, duration: 0.1)

        XCTAssertEqual(logger.recent.map(\.url), ["b", "c"])
    }
}
