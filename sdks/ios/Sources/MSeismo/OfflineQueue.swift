import Foundation

/// Persistent delivery queue with retry (FR-33, FR-34, SDK-10): a report
/// made offline, or that fails to send, stays queued and is retried at
/// growing intervals; a report still unsent when the app closes is sent
/// on the next launch, since `OfflineQueueStore` persists it to disk.
public final class OfflineQueue {
    private let transport: ReportTransport
    private let store: OfflineQueueStore
    private let now: () -> Date
    private let maxBackoff: TimeInterval
    private let jitter: (TimeInterval) -> TimeInterval
    private let queue = DispatchQueue(label: "mseismo.offline-queue")

    private var items: [QueuedReport]
    private var isSending = false
    private var _configuration: Configuration?

    /// Nothing sends until this is set — `MSeismo.configure(_:)` does
    /// that. A plain settable property (not a constructor parameter)
    /// since it's genuinely unknown yet when `MSeismo.shared` is first
    /// created: `configure(_:)` runs later, at app launch.
    public var configuration: Configuration? {
        get { queue.sync { _configuration } }
        set { queue.sync { _configuration = newValue } }
    }

    /// Fired after each send attempt settles, success or failure. Not
    /// part of the stable public contract — mainly so tests (and an app,
    /// if it wants a "queue drained" signal) know async work finished.
    public var onDrained: (() -> Void)?

    public init(
        transport: ReportTransport,
        store: OfflineQueueStore = OfflineQueueStore(),
        now: @escaping () -> Date = Date.init,
        maxBackoff: TimeInterval = 300,
        jitter: @escaping (TimeInterval) -> TimeInterval = { Double.random(in: 0...($0 * 0.2)) }
    ) {
        self.transport = transport
        self.store = store
        self.now = now
        self.maxBackoff = maxBackoff
        self.jitter = jitter
        items = store.load()
    }

    /// Queues a report for delivery. Persisted synchronously before this
    /// returns — the report survives even if the app is killed right
    /// after. The actual network send happens asynchronously.
    public func enqueue(_ report: Report) {
        // The ID is the backend's idempotency key: fixed here, once, so
        // every retry of this report carries the same one.
        var report = report
        if report.id == nil { report.id = UUID().uuidString }

        queue.sync {
            items.append(QueuedReport(report: report, nextAttemptAt: now()))
            store.save(items)
        }
        queue.async { [weak self] in
            self?.drainLocked()
        }
    }

    /// Call when connectivity returns, or periodically, to retry anything
    /// still pending (FR-33).
    public func retryNow() {
        queue.async { [weak self] in
            self?.drainLocked()
        }
    }

    public var pendingCount: Int {
        queue.sync { items.count }
    }

    /// Must only be called while already on `queue`.
    private func drainLocked() {
        guard !isSending else { return }
        guard let configuration = _configuration else { return }
        guard let index = items.firstIndex(where: { $0.nextAttemptAt <= now() }) else { return }

        isSending = true
        let item = items[index]

        transport.send(item.report, to: configuration) { [weak self] result in
            guard let self else { return }
            self.queue.async {
                self.isSending = false

                switch result {
                case .success:
                    self.items.removeAll { $0.id == item.id }
                case .failure(let error):
                    if let transportError = error as? TransportError, transportError.isPermanent {
                        // The backend will never accept this report; keep
                        // it from blocking the ones behind it.
                        NSLog("[MSeismo] dropped report %@: %@", item.report.id ?? "?", String(describing: transportError))
                        self.items.removeAll { $0.id == item.id }
                    } else if let currentIndex = self.items.firstIndex(where: { $0.id == item.id }) {
                        self.items[currentIndex].attempts += 1
                        let backoff = min(self.maxBackoff, pow(2, Double(self.items[currentIndex].attempts)))
                        let serverDelay = (error as? TransportError)?.retryAfter ?? 0
                        let delay = max(backoff + self.jitter(backoff), serverDelay)
                        self.items[currentIndex].nextAttemptAt = self.now().addingTimeInterval(delay)
                    }
                }

                self.store.save(self.items)
                self.onDrained?()
                self.drainLocked()
            }
        }
    }
}
