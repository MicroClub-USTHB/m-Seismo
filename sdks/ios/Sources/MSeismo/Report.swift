import Foundation

/// A reporter-submitted bug report (PRD FR-10). Every field is optional
/// (FR-12): defaults exist so a report can be sent with none of them filled
/// in, in auto-submit mode.
public struct Report {
    public enum Category: String, CaseIterable, Identifiable, Equatable {
        case bug, crash, ui, other

        public var id: String { rawValue }

        public var label: String {
            switch self {
            case .bug: return "Bug"
            case .crash: return "Crash"
            case .ui: return "UI"
            case .other: return "Other"
            }
        }
    }

    public enum Severity: String, CaseIterable, Identifiable, Equatable {
        case low, medium, high, critical

        public var id: String { rawValue }
        public var label: String { rawValue.capitalized }
    }

    public var title: String
    public var description: String
    public var category: Category
    public var severity: Severity

    /// A screenshot the reporter attached from their photo library (JPEG).
    /// `Data`, not `UIImage`, so this type stays usable outside iOS-only
    /// code (see `ReportFormView`, which populates it).
    public var screenshot: Data?

    public init(
        title: String = "",
        description: String = "",
        category: Category = .bug,
        severity: Severity = .medium,
        screenshot: Data? = nil
    ) {
        self.title = title
        self.description = description
        self.category = category
        self.severity = severity
        self.screenshot = screenshot
    }
}
