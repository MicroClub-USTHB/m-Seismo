import Foundation

/// Delivers a single report to the backend. `OfflineQueue` is the only
/// caller in this SDK; a test can substitute a fake to avoid the network.
public protocol ReportTransport {
    func send(_ report: Report, to configuration: Configuration, completion: @escaping (Result<Void, Error>) -> Void)
}

/// Why a send failed, in the terms of the ingestion API's retry rules
/// (spec/ingestion.md §5). Any other error, such as no network, is
/// retried like `badStatus`.
public enum TransportError: Error {
    /// Worth retrying later: `429`, `5xx`, anything unexpected.
    /// `retryAfter` is the server's `Retry-After`, in seconds.
    case badStatus(Int?, retryAfter: TimeInterval? = nil)

    /// The backend refused this report and always will (a `4xx` other than
    /// `429`), or it could not be built: drop it so it can't block the queue.
    case rejected(Int?)

    var isPermanent: Bool {
        if case .rejected = self { return true }
        return false
    }

    var retryAfter: TimeInterval? {
        if case .badStatus(_, let retryAfter) = self { return retryAfter }
        return nil
    }
}

/// `URLSession`-based `ReportTransport`. POSTs the report as
/// `multipart/form-data` to `configuration.reportsURL`: a `report` JSON
/// part and, when there is one, a `screenshot` part. The SDK key goes in
/// `Authorization: Bearer` (FR-01: a key can only send reports).
public final class URLSessionTransport: ReportTransport {
    private let session: URLSession
    private let installationID: () -> String

    public init(session: URLSession = .shared, installation: Installation = .shared) {
        self.session = session
        installationID = { installation.id }
    }

    /// Builds the request without sending it.
    func makeRequest(for report: Report, configuration: Configuration) throws -> URLRequest {
        let payload = ReportPayload(
            report: report,
            configuration: configuration,
            installationID: installationID()
        )

        var body = MultipartBody()
        body.addPart(name: "report", contentType: "application/json", data: try payload.encoded())
        if let screenshot = report.screenshot, !screenshot.isEmpty {
            let isPNG = screenshot.starts(with: [0x89, 0x50, 0x4E, 0x47])
            body.addPart(
                name: "screenshot",
                filename: isPNG ? "screenshot.png" : "screenshot.jpg",
                contentType: isPNG ? "image/png" : "image/jpeg",
                data: screenshot
            )
        }

        var request = URLRequest(url: configuration.reportsURL)
        request.httpMethod = "POST"
        request.setValue("Bearer \(configuration.sdkKey)", forHTTPHeaderField: "Authorization")
        request.setValue(body.contentType, forHTTPHeaderField: "Content-Type")
        request.httpBody = body.finalized()
        return request
    }

    public func send(_ report: Report, to configuration: Configuration, completion: @escaping (Result<Void, Error>) -> Void) {
        let request: URLRequest
        do {
            request = try makeRequest(for: report, configuration: configuration)
        } catch {
            completion(.failure(TransportError.rejected(nil)))
            return
        }

        let task = session.dataTask(with: request) { _, response, error in
            if let error {
                completion(.failure(error))
                return
            }
            guard let http = response as? HTTPURLResponse else {
                completion(.failure(TransportError.badStatus(nil)))
                return
            }
            completion(Self.result(forStatus: http.statusCode, retryAfter: http.value(forHTTPHeaderField: "Retry-After")))
        }
        task.resume()
    }

    /// `2xx` is delivered (the API answers `202`); a `4xx` other than `429`
    /// is final; everything else is worth trying again.
    static func result(forStatus status: Int, retryAfter: String?) -> Result<Void, Error> {
        switch status {
        case 200..<300:
            return .success(())
        case 429:
            return .failure(TransportError.badStatus(status, retryAfter: retryAfter.flatMap { TimeInterval($0) }))
        case 400..<500:
            return .failure(TransportError.rejected(status))
        default:
            return .failure(TransportError.badStatus(status, retryAfter: retryAfter.flatMap { TimeInterval($0) }))
        }
    }
}

/// A `multipart/form-data` body, built in memory (a report plus one
/// screenshot is at most a few MB).
struct MultipartBody {
    let boundary = "mseismo-\(UUID().uuidString)"
    private var data = Data()

    var contentType: String { "multipart/form-data; boundary=\(boundary)" }

    mutating func addPart(name: String, filename: String? = nil, contentType: String, data partData: Data) {
        var disposition = "Content-Disposition: form-data; name=\"\(name)\""
        if let filename { disposition += "; filename=\"\(filename)\"" }
        data.append(Data("--\(boundary)\r\n\(disposition)\r\nContent-Type: \(contentType)\r\n\r\n".utf8))
        data.append(partData)
        data.append(Data("\r\n".utf8))
    }

    func finalized() -> Data {
        var result = data
        result.append(Data("--\(boundary)--\r\n".utf8))
        return result
    }
}
