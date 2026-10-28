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

    private let configuration = Configuration(
        endpoint: URL(string: "https://seismo.example.com/reports")!,
        projectKey: "test-project-key"
    )

    func testSendsProjectKeyHeaderAndReportBody() {
        let expectation = expectation(description: "request sent")
        var capturedRequest: URLRequest?

        StubURLProtocol.handler = { request in
            capturedRequest = request
            return (HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!, Data())
        }

        let transport = URLSessionTransport(session: makeSession())
        transport.send(Report(title: "Broken button"), to: configuration) { _ in
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2)

        XCTAssertEqual(capturedRequest?.httpMethod, "POST")
        XCTAssertEqual(capturedRequest?.value(forHTTPHeaderField: "X-Seismo-Project-Key"), "test-project-key")
    }

    func testSuccessOnA2xxStatus() {
        let expectation = expectation(description: "request sent")
        StubURLProtocol.handler = { request in
            (HTTPURLResponse(url: request.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, Data())
        }

        var result: Result<Void, Error>?
        let transport = URLSessionTransport(session: makeSession())
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
        let transport = URLSessionTransport(session: makeSession())
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
}
