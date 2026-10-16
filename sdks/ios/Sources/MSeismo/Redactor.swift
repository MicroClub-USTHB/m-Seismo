import Foundation

/// Strips personal data from text using pattern rules, plus allow and deny
/// lists the app integrator sets (FR-25). Applied client-side, before
/// anything leaves the phone.
public final class Redactor {
    public var rules: [RedactionRule]

    /// Strings that must always be redacted in addition to `rules`
    /// (e.g. an internal project codename that isn't PII but still
    /// shouldn't leave the device).
    public var denyList: [String] = []

    /// Strings that must never be redacted, even if a rule would
    /// otherwise match them (e.g. a public support email address).
    public var allowList: [String] = []

    public init(rules: [RedactionRule] = RedactionRule.defaultRules) {
        self.rules = rules
    }

    public func redact(_ text: String) -> String {
        guard !text.isEmpty else { return text }

        var working = text
        var placeholders: [(placeholder: String, original: String)] = []

        for (index, allowed) in allowList.enumerated() where !allowed.isEmpty && working.contains(allowed) {
            let placeholder = "\u{E000}ALLOW\(index)\u{E001}"
            working = working.replacingOccurrences(of: allowed, with: placeholder)
            placeholders.append((placeholder, allowed))
        }

        for rule in rules {
            let range = NSRange(working.startIndex..., in: working)
            working = rule.pattern.stringByReplacingMatches(in: working, range: range, withTemplate: rule.replacement)
        }

        for denied in denyList where !denied.isEmpty {
            working = working.replacingOccurrences(of: denied, with: "[REDACTED]")
        }

        for (placeholder, original) in placeholders {
            working = working.replacingOccurrences(of: placeholder, with: original)
        }

        return working
    }
}
