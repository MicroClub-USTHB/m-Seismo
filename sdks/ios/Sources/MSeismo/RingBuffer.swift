import Foundation

/// A fixed-capacity buffer that drops the oldest element once full.
/// Backs both `LogBuffer` (SDK-07) and `BreadcrumbTracker` (FR-18).
public struct RingBuffer<Element> {
    private var storage: [Element] = []
    public let capacity: Int

    public init(capacity: Int) {
        self.capacity = max(0, capacity)
    }

    public mutating func append(_ element: Element) {
        guard capacity > 0 else { return }
        storage.append(element)
        if storage.count > capacity {
            storage.removeFirst(storage.count - capacity)
        }
    }

    public var elements: [Element] { storage }
}
