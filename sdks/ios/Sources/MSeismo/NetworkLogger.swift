import Foundation

/// One logged network request (FR-23, SDK-13).
public struct NetworkRequestLog: Codable, Equatable {
    public var url: String
    public var method: String
    public var statusCode: Int?
    public var duration: TimeInterval
    public var requestBody: String?
    public var responseBody: String?
    public var timestamp: Date

    public init(
        url: String,
        method: String,
        statusCode: Int?,
        duration: TimeInterval,
        requestBody: String? = nil,
        responseBody: String? = nil,
        timestamp: Date = Date()
    ) {
        self.url = url
        self.method = method
        self.statusCode = statusCode
        self.duration = duration
        self.requestBody = requestBody
        self.responseBody = responseBody
        self.timestamp = timestamp
    }
}

/// Ring buffer of the app's recent network requests (FR-23). An app calls
/// `record(...)` from wherever it already has access to its own requests
/// and responses (a `URLSession` delegate, an interceptor, ...); this SDK
/// doesn't instrument networking automatically, since swizzling global
/// `URLSession` behavior is the kind of invasive trick NFR-02 ("must never
/// crash the app") argues against.
///
/// Request/response bodies are excluded unless `capturesBodies` is turned
/// on explicitly (FR-28: default deny). Even then, `MSeismo.redacted(_:)`
/// still runs them through `redactor` before they'd leave the phone.
public final class NetworkLogger {
    private var buffer: RingBuffer<NetworkRequestLog>

    public var capturesBodies = false

    public init(capacity: Int = 50) {
        buffer = RingBuffer(capacity: capacity)
    }

    public func record(
        url: String,
        method: String,
        statusCode: Int?,
        duration: TimeInterval,
        requestBody: String? = nil,
        responseBody: String? = nil
    ) {
        buffer.append(
            NetworkRequestLog(
                url: url,
                method: method,
                statusCode: statusCode,
                duration: duration,
                requestBody: capturesBodies ? requestBody : nil,
                responseBody: capturesBodies ? responseBody : nil
            )
        )
    }

    public var recent: [NetworkRequestLog] { buffer.elements }
}
