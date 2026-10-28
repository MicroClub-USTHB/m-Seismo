import Foundation

/// Captures uncaught exceptions and manually-recorded errors (FR-08/FR-09,
/// SDK-08), persists them, and hands the pending one back on the next
/// launch so `MSeismo` can auto-report it with no user action.
///
/// Scope note: this installs `NSSetUncaughtExceptionHandler`, which
/// catches Objective-C-style/bridged uncaught exceptions. It does **not**
/// catch native traps (`fatalError()`, force-unwrap, SIGSEGV, SIGABRT,
/// ...): doing that safely requires an async-signal-safe handler (no
/// allocation, no JSON encoding, no normal Swift runtime calls), which is
/// real native-crash-reporter territory — the reason Crashlytics exists,
/// and why m-Seismo runs alongside it rather than replacing it (PRD 1.3).
/// A handler that does unsafe work mid-crash risks violating NFR-02 ("must
/// never crash the app"), so that's deliberately left out here.
public final class CrashReporter {
    public static let shared = CrashReporter()

    private let store: CrashStore
    private var isStarted = false

    init(store: CrashStore = CrashStore()) {
        self.store = store
    }

    /// Installs the uncaught exception handler. Call once, at launch.
    public func start() {
        guard !isStarted else { return }
        isStarted = true
        NSSetUncaughtExceptionHandler { exception in
            CrashReporter.shared.persist(exception: exception)
        }
    }

    /// Records an error the app already caught (FR-09), e.g. from a
    /// `catch` block it considers worth reporting.
    public func recordError(_ error: Error, fatal: Bool = false) {
        let nsError = error as NSError
        let crash = CrashReport(
            name: nsError.domain,
            reason: nsError.localizedDescription,
            callStack: Thread.callStackSymbols,
            isFatal: fatal
        )
        store.save(crash)
    }

    private func persist(exception: NSException) {
        let crash = CrashReport(
            name: exception.name.rawValue,
            reason: exception.reason,
            callStack: exception.callStackSymbols,
            isFatal: true
        )
        store.save(crash)
    }

    /// The crash or recorded error left over from the previous launch, if
    /// any. Consumes it, so it's only reported once.
    public func pendingCrash() -> CrashReport? {
        store.loadAndClear()
    }
}
