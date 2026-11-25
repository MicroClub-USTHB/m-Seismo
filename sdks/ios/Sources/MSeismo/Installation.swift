import Foundation

/// The anonymous ID of this app installation (schema `installation_id`):
/// generated on first use and kept on the device. It is random, never
/// derived from a person or a device identifier.
public final class Installation {
    public static let shared = Installation()

    private let defaults: UserDefaults
    private let key: String
    private let lock = NSLock()

    public init(defaults: UserDefaults = .standard, key: String = "mseismo.installation_id") {
        self.defaults = defaults
        self.key = key
    }

    public var id: String {
        lock.lock()
        defer { lock.unlock() }
        if let existing = defaults.string(forKey: key), !existing.isEmpty {
            return existing
        }
        let created = UUID().uuidString
        defaults.set(created, forKey: key)
        return created
    }
}
