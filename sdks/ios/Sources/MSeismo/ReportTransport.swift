import Foundation

/// Delivers a single report to the backend. `OfflineQueue` is the only
/// caller in this SDK; a test can substitute a fake to avoid the network.
public protocol ReportTransport {
    func send(_ report: Report, to configuration: Configuration, completion: @escaping (Result<Void, Error>) -> Void)
}

public enum TransportError: Error {
    case badStatus(Int?)
}

/// `URLSession`-based `ReportTransport`. POSTs the report as JSON to
/// `configuration.endpoint`, authenticated with the project key (FR-01:
/// a project key can only send reports).
public final class URLSessionTransport: ReportTransport {
    private let session: URLSession

    public init(session: URLSession = .shared) {
        self.session = session
    }

    public func send(_ report: Report, to configuration: Configuration, completion: @escaping (Result<Void, Error>) -> Void) {
        var request = URLRequest(url: configuration.endpoint)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue(configuration.projectKey, forHTTPHeaderField: "X-Seismo-Project-Key")

        do {
            request.httpBody = try JSONEncoder().encode(report)
        } catch {
            completion(.failure(error))
            return
        }

        let task = session.dataTask(with: request) { _, response, error in
            if let error {
                completion(.failure(error))
                return
            }
            guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
                completion(.failure(TransportError.badStatus((response as? HTTPURLResponse)?.statusCode)))
                return
            }
            completion(.success(()))
        }
        task.resume()
    }
}
