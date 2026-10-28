import Foundation

/// One pattern-based redaction rule (FR-25, SDK-12).
public struct RedactionRule {
    public var name: String
    public var pattern: NSRegularExpression
    public var replacement: String

    public init(name: String, pattern: String, replacement: String = "[REDACTED]") {
        self.name = name
        // These patterns are static and known-valid; a throwing init here
        // would just push the same force-try onto every call site.
        self.pattern = try! NSRegularExpression(pattern: pattern)
        self.replacement = replacement
    }

    /// Emails, phone numbers, credit card numbers, and bearer tokens
    /// (FR-25's "emails, phone numbers, card numbers, tokens").
    public static let defaultRules: [RedactionRule] = [
        RedactionRule(name: "email", pattern: #"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"#),
        RedactionRule(name: "phone", pattern: #"\+?\d[\d\-.\s]{8,}\d"#),
        RedactionRule(name: "credit-card", pattern: #"\b(?:\d[ -]?){13,16}\b"#),
        RedactionRule(name: "bearer-token", pattern: #"Bearer\s+[A-Za-z0-9\-_.]+"#),
    ]
}
