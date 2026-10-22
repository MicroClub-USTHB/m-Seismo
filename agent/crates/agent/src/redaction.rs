use regex::Regex;
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
        // XXX: no email / ip scrubbing, too many false positives for debugging logs
    ]
    .into_iter()
    .map(|(re, to)| (Regex::new(re).expect("static redaction regex"), to))
    .collect()
});

pub fn redact(line: &str) -> String {
    let mut out = line.to_owned();
    for (re, to) in RULES.iter() {
        // PERF: replace_all returns Cow, only allocates when something matched
        if let std::borrow::Cow::Owned(s) = re.replace_all(&out, *to) {
            out = s;
        }
    }
    out
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
        ] {
            let out = redact(input);
            assert!(!out.contains(gone), "{input} -> {out}");
            assert!(out.contains("[REDACTED]"), "{input} -> {out}");
        }
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
