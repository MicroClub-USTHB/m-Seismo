import XCTest
@testable import MSeismo

/// Stubs every request so these tests never touch the real network.
private final class StubURLProtocol: URLProtocol {
    static var handler: ((URLRequest) -> (HTTPURLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = StubURLProtocol.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        let (response, data) = handler(request)
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

final class URLSessionTransportTests: XCTestCase {
    private func makeSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    private func makeInstallation() -> Installation {
        Installation(defaults: UserDefaults(suiteName: UUID().uuidString)!)
    }

    private let configuration = Configuration(
        baseURL: URL(string: "https://seismo.example.com")!,
        sdkKey: "msk_test"
    )

    func testPostsToTheReportsPathWithABearerKey() {
        let expectation = expectation(description: "request sent")
        var capturedRequest: URLRequest?

        StubURLProtocol.handler = { request in
            capturedRequest = request
            return (HTTPURLResponse(url: request.url!, statusCode: 202, httpVersion: nil, headerFields: nil)!, Data())
        }

        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())
        transport.send(Report(title: "Broken button"), to: configuration) { _ in
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(capturedRequest?.httpMethod, "POST")
        XCTAssertEqual(capturedRequest?.url?.absoluteString, "https://seismo.example.com/api/v1/reports")
        XCTAssertEqual(capturedRequest?.value(forHTTPHeaderField: "Authorization"), "Bearer msk_test")
        XCTAssertNil(capturedRequest?.value(forHTTPHeaderField: "X-Seismo-Project-Key"))
    }

    func testBodyIsMultipartWithAReportPartAndNoScreenshotWhenThereIsNone() throws {
        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())

        let request = try transport.makeRequest(for: Report(title: "Broken button", id: "ID-1"), configuration: configuration)
        let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
        let contentType = request.value(forHTTPHeaderField: "Content-Type") ?? ""

        XCTAssertTrue(contentType.hasPrefix("multipart/form-data; boundary="))
        XCTAssertTrue(body.contains("name=\"report\""))
        XCTAssertTrue(body.contains("Content-Type: application/json"))
        XCTAssertTrue(body.contains("\"title\":\"Broken button\""))
        XCTAssertFalse(body.contains("name=\"screenshot\""))
    }

    func testScreenshotIsSentAsItsOwnPart() throws {
        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())
        let jpeg = Data([0xFF, 0xD8, 0xFF, 0xE0])
        let png = Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A])

        let jpegBody = String(decoding: try transport.makeRequest(for: Report(screenshot: jpeg), configuration: configuration).httpBody ?? Data(), as: UTF8.self)
        let pngBody = String(decoding: try transport.makeRequest(for: Report(screenshot: png), configuration: configuration).httpBody ?? Data(), as: UTF8.self)

        XCTAssertTrue(jpegBody.contains("name=\"screenshot\"; filename=\"screenshot.jpg\""))
        XCTAssertTrue(jpegBody.contains("Content-Type: image/jpeg"))
        XCTAssertTrue(pngBody.contains("filename=\"screenshot.png\""))
        XCTAssertTrue(pngBody.contains("Content-Type: image/png"))
    }

    func testReportPartIsTheSameForEveryRetry() throws {
        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())
        var report = Report(title: "Same report")
        report.id = "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51"
        report.occurredAt = Date(timeIntervalSince1970: 1_000)

        let first = try transport.makeRequest(for: report, configuration: configuration).httpBody
        let second = try transport.makeRequest(for: report, configuration: configuration).httpBody

        // Only the multipart boundary differs between two requests.
        let strip: (Data?) -> String = {
            String(decoding: $0 ?? Data(), as: UTF8.self)
                .components(separatedBy: "\r\n")
                .filter { !$0.hasPrefix("--mseismo-") }
                .joined(separator: "\n")
        }
        XCTAssertEqual(strip(first), strip(second))
        XCTAssertTrue(strip(first).contains("\"id\":\"0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51\""))
    }

    func testSuccessOnA2xxStatus() {
        let expectation = expectation(description: "request sent")
        StubURLProtocol.handler = { request in
            (HTTPURLResponse(url: request.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, Data())
        }

        var result: Result<Void, Error>?
        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())
        transport.send(Report(), to: configuration) {
            result = $0
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2)

        switch result {
        case .success: break
        default: XCTFail("expected success, got \(String(describing: result))")
        }
    }

    func testFailureOnA5xxStatus() {
        let expectation = expectation(description: "request sent")
        StubURLProtocol.handler = { request in
            (HTTPURLResponse(url: request.url!, statusCode: 503, httpVersion: nil, headerFields: nil)!, Data())
        }

        var result: Result<Void, Error>?
        let transport = URLSessionTransport(session: makeSession(), installation: makeInstallation())
        transport.send(Report(), to: configuration) {
            result = $0
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2)

        switch result {
        case .failure: break
        default: XCTFail("expected failure, got \(String(describing: result))")
        }
    }

    private func result(forStatus status: Int, retryAfter: String? = nil) -> Result<Void, Error> {
        URLSessionTransport.result(forStatus: status, retryAfter: retryAfter)
    }

    func testAcceptedIsDelivered() {
        guard case .success = result(forStatus: 202) else { return XCTFail("202 should be delivered") }
    }

    func testClientErrorsAreFinalExceptRateLimiting() {
        for status in [400, 401, 403, 404, 413, 415] {
            guard case .failure(let error) = result(forStatus: status),
                  let transportError = error as? TransportError else { return XCTFail("\(status) should fail") }
            XCTAssertTrue(transportError.isPermanent, "\(status) should be dropped")
        }
    }

    func testRateLimitAndServerErrorsAreRetriedAndHonourRetryAfter() {
        for status in [429, 500, 502, 503] {
            guard case .failure(let error) = result(forStatus: status, retryAfter: "30"),
                  let transportError = error as? TransportError else { return XCTFail("\(status) should fail") }
            XCTAssertFalse(transportError.isPermanent, "\(status) should be retried")
            XCTAssertEqual(transportError.retryAfter, 30)
        }
    }

    func testRetryAfterThatIsNotSecondsIsIgnored() {
        guard case .failure(let error) = result(forStatus: 503, retryAfter: "Wed, 21 Oct 2026 07:28:00 GMT"),
              let transportError = error as? TransportError else { return XCTFail("503 should fail") }
        XCTAssertNil(transportError.retryAfter)
    }
}
