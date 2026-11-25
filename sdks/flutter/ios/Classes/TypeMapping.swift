import CoreFoundation
import Foundation

enum TypeMapping {
    static func trigger(_ trigger: ReportTrigger) -> Report.Trigger {
        switch trigger {
        case .shake: return .shake
        case .screenshot: return .screenshot
        case .manual: return .manual
        case .crash: return .crash
        case .longPress: return .longPress
        case .nonFatal: return .nonFatal
        }
    }

    static func trigger(_ trigger: Report.Trigger) -> ReportTrigger {
        switch trigger {
        case .shake: return .shake
        case .screenshot: return .screenshot
        case .manual: return .manual
        case .crash: return .crash
        case .longPress: return .longPress
        case .nonFatal: return .nonFatal
        }
    }

    static func category(_ category: ReportCategory) -> Report.Category {
        switch category {
        case .bug: return .bug
        case .crash: return .crash
        case .ui: return .ui
        case .other: return .other
        }
    }

    static func category(_ category: Report.Category) -> ReportCategory {
        switch category {
        case .bug: return .bug
        case .crash: return .crash
        case .ui: return .ui
        case .other: return .other
        }
    }

    static func severity(_ severity: ReportSeverity) -> Report.Severity {
        switch severity {
        case .low: return .low
        case .medium: return .medium
        case .high: return .high
        case .critical: return .critical
        }
    }

    static func severity(_ severity: Report.Severity) -> ReportSeverity {
        switch severity {
        case .low: return .low
        case .medium: return .medium
        case .high: return .high
        case .critical: return .critical
        }
    }

    static func breadcrumbKind(_ kind: BreadcrumbKind) -> Breadcrumb.Kind {
        switch kind {
        case .navigation: return .navigation
        case .tap: return .tap
        case .network: return .network
        case .lifecycle: return .lifecycle
        }
    }

    static func report(from draft: ReportDraft) -> Report {
        Report(
            title: draft.title,
            description: draft.description,
            category: category(draft.category),
            severity: severity(draft.severity),
            screenshot: draft.screenshot?.data,
            trigger: trigger(draft.trigger),
            crash: draft.crash.map {
                CrashReport(
                    name: $0.type,
                    reason: $0.reason,
                    callStack: $0.stackTrace,
                    isFatal: $0.fatal
                )
            }
        )
    }

    static func autoReport(from report: Report) -> AutoReport {
        AutoReport(
            id: report.id ?? "",
            title: report.title,
            description: report.description,
            category: category(report.category),
            severity: severity(report.severity),
            trigger: trigger(report.trigger ?? .manual),
            occurredAtMillis: Int64(
                (report.occurredAt ?? Date()).timeIntervalSince1970 * 1000
            ),
            crash: report.crash.map {
                CrashDetails(
                    type: $0.name,
                    reason: $0.reason,
                    stackTrace: $0.callStack,
                    fatal: $0.isFatal
                )
            }
        )
    }

    /// Converts the Pigeon `Map<String, Object?>` into the native flag
    /// dictionary. Flutter's standard codec delivers numbers and booleans
    /// as `NSNumber`, so inspect the CoreFoundation type to distinguish a
    /// boolean from the integer `1`.
    static func flagValues(from values: [String: Any?]) -> [String: FlagValue] {
        var flags: [String: FlagValue] = [:]
        for (key, rawValue) in values {
            guard !key.isEmpty, let value = rawValue else { continue }
            if let string = value as? String {
                flags[key] = .string(string)
            } else if let number = value as? NSNumber {
                if CFGetTypeID(number) == CFBooleanGetTypeID() {
                    flags[key] = .bool(number.boolValue)
                } else {
                    flags[key] = .number(number.doubleValue)
                }
            }
        }
        return flags
    }
}
