use regex::{Captures, Regex};
use std::borrow::Cow;
use std::sync::LazyLock;

// NOTE: (pattern, replacement), applied in order. regex crate is linear time so
// a hostile log line can't blow us up (no catastrophic backtracking)
static RULES: LazyLock<Vec<(Regex, &str)>> = LazyLock::new(|| {
    [
        // Authorization: Bearer xxx / bare "Bearer xxx"
        (r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+", "Bearer [REDACTED]"),
        (r"(?i)\bbasic\s+[A-Za-z0-9+/=]{8,}", "Basic [REDACTED]"),
        // jwt, even outside of a header
        (
            r"\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*",
            "[REDACTED]",
        ),
        // password=foo, "api_key": "foo", token: foo ...
        (
            r#"(?i)\b(pass(?:word|wd)?|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)(["']?\s*[:=]\s*["']?)[^\s"',;&]+"#,
            "$1$2[REDACTED]",
        ),
        // scheme://user:pass@host
        (r"://[^/\s:@]+:[^/\s@]+@", "://[REDACTED]@"),
        (r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b", "[REDACTED]"),
        // email addresses
        (r"(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", "[REDACTED]"),
        // phone numbers: E.164-ish (+1415...), or a 3-3-4 group with a
        // separator (415-555-0132, (415) 555-0132); a bare run of digits
        // is left alone, too many false positives (ids, ports, durations).
        (r"\+[1-9]\d{6,14}\b", "[REDACTED]"),
        (r"\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}\b", "[REDACTED]"),
        // XXX: no ip scrubbing, too many false positives for debugging logs
    ]
    .into_iter()
    .map(|(re, to)| (Regex::new(re).expect("static redaction regex"), to))
    .collect()
});

// Card numbers: 13-19 digits, optionally grouped with spaces or dashes.
// Scrubbed only when they pass the Luhn check, so a plain id or trace
// number of the same length (common in debugging logs) is left alone.
static CARD_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b(?:\d[ -]?){12,18}\d\b").expect("static redaction regex"));

pub fn redact(line: &str) -> String {
    let mut out = line.to_owned();
    for (re, to) in RULES.iter() {
        // PERF: replace_all returns Cow, only allocates when something matched
        if let Cow::Owned(s) = re.replace_all(&out, *to) {
            out = s;
        }
    }
    if let Cow::Owned(s) = CARD_RE.replace_all(&out, redact_card) {
        out = s;
    }
    out
}

fn redact_card(caps: &Captures) -> String {
    let m = &caps[0];
    let digits: String = m.chars().filter(char::is_ascii_digit).collect();
    if luhn_valid(&digits) {
        "[REDACTED]".to_string()
    } else {
        m.to_string()
    }
}

// PERF: no allocation, no catastrophic anything: one pass over the digits.
fn luhn_valid(digits: &str) -> bool {
    if digits.len() < 13 || digits.len() > 19 {
        return false;
    }
    let sum: u32 = digits
        .chars()
        .rev()
        .enumerate()
        .map(|(i, c)| {
            let d = c.to_digit(10).expect("digits only");
            if i % 2 == 1 {
                let d2 = d * 2;
                if d2 > 9 { d2 - 9 } else { d2 }
            } else {
                d
            }
        })
        .sum();
    sum.is_multiple_of(10)
}

#[cfg(test)]
mod tests {
    use super::redact;

    #[test]
    fn scrubs_secrets() {
        for (input, gone) in [
            ("Authorization: Bearer abc.def-123", "abc.def-123"),
            ("auth Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA"),
            ("jwt eyJhbGciOi.eyJzdWIiOiIx.sig_nature end", "eyJhbGciOi"),
            ("connect password=hunter2 ok", "hunter2"),
            (r#"{"api_key": "sk-live-42", "a": 1}"#, "sk-live-42"),
            ("token: abcdef", "abcdef"),
            ("dial postgres://app:s3cret@db:5432/x", "s3cret"),
            ("key AKIAIOSFODNN7EXAMPLE used", "AKIAIOSFODNN7EXAMPLE"),
            ("contact user@example.com for help", "user@example.com"),
            ("call (415) 555-0132 now", "(415) 555-0132"),
            ("call 415-555-0132 now", "415-555-0132"),
            ("reach +14155550132 support", "+14155550132"),
            ("card 4111 1111 1111 1111 charged", "4111 1111 1111 1111"),
            ("card 4111111111111111 charged", "4111111111111111"),
        ] {
            let out = redact(input);
            assert!(!out.contains(gone), "{input} -> {out}");
            assert!(out.contains("[REDACTED]"), "{input} -> {out}");
        }
    }

    #[test]
    fn keeps_numbers_that_are_not_card_numbers() {
        // 16 digits, but fails the Luhn check: not a real card number, and a
        // trace/order id like this is common in debugging logs.
        let l = "order id 1234567890123456 created";
        assert_eq!(redact(l), l);
    }

    #[test]
    fn keeps_the_rest_of_the_line() {
        assert_eq!(
            redact("req id=7 password=x took 3ms"),
            "req id=7 password=[REDACTED] took 3ms"
        );
        assert_eq!(
            redact("postgres://app:pw@db/x"),
            "postgres://[REDACTED]@db/x"
        );
    }

    #[test]
    fn leaves_clean_lines_alone() {
        let l = "GET /healthz 200 3ms trace_id=4bf92f3577b34da6";
        assert_eq!(redact(l), l);
    }
}
