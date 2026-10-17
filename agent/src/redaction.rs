use std::sync::OnceLock;

use regex::{Regex, RegexBuilder};
use serde_json::Value;

/// The outcome is explicit so callers can count dropped lines without ever
/// falling back to returning the original message.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum RedactionOutcome {
    Kept(String),
    Dropped(DropReason),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DropReason {
    LineTooLong,
    PrivateKeyBlock,
    OpaqueEncodedPayload,
    InvalidJson,
    PatternInitializationFailed,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BoundedRedactionResult {
    pub lines: Vec<String>,
    /// Input indices corresponding to `lines`, in input order.
    pub kept_indices: Vec<usize>,
    pub dropped_line_count: u64,
    pub truncated: bool,
}

const MAX_LINE_BYTES: usize = 16 * 1024;
pub const MAX_INPUT_BYTES: usize = 8 * 1024 * 1024;
const MASK: &str = "[REDACTED]";

struct Patterns {
    bearer: Regex,
    auth_header: Regex,
    secret_assignment: Regex,
    vendor_token: Regex,
    jwt: Regex,
    email: Regex,
    card_candidate: Regex,
    private_key_start: Regex,
}

fn patterns() -> Option<&'static Patterns> {
    static PATTERNS: OnceLock<Option<Patterns>> = OnceLock::new();
    PATTERNS
        .get_or_init(|| {
            Some(Patterns {
                bearer: Regex::new(r"(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*").ok()?,
                auth_header: Regex::new(
            r"(?i)(authorization|proxy-authorization|cookie|set-cookie)\s*[:=]\s*([^,;\s]+(?:\s+[^,;\s]+)?)",
        )
                    .ok()?,
                secret_assignment: Regex::new(
            r#"(?i)\b(password|passwd|pwd|token|access[_-]?token|refresh[_-]?token|client[_-]?secret|secret|api[_-]?key|credential|auth)\b\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;&]+)"#,
        )
                    .ok()?,
                vendor_token: Regex::new(
            r"\b(?:A[KS]IA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b",
        )
                    .ok()?,
                jwt: Regex::new(
                    r"\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b",
                )
                .ok()?,
                email: Regex::new(r"(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b").ok()?,
                card_candidate: Regex::new(r"\b(?:\d[ -]?){13,19}\b").ok()?,
                private_key_start: RegexBuilder::new(r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----")
                    .case_insensitive(true)
                    .build()
                    .ok()?,
            })
        })
        .as_ref()
}

/// Redact a single log line. A private-key marker is dropped because the key
/// may span lines and line-by-line masking cannot safely redact the whole block.
fn redact_line(line: &str) -> RedactionOutcome {
    if line.len() > MAX_LINE_BYTES {
        return RedactionOutcome::Dropped(DropReason::LineTooLong);
    }

    let Some(p) = patterns() else {
        return RedactionOutcome::Dropped(DropReason::PatternInitializationFailed);
    };
    if p.private_key_start.is_match(line) {
        return RedactionOutcome::Dropped(DropReason::PrivateKeyBlock);
    }

    let mut output = match find_json_payload(line) {
        Some((prefix, mut value)) => {
            if mask_sensitive_json_keys(&mut value) {
                match serde_json::to_string(&value) {
                    Ok(json) => format!("{prefix}{json}"),
                    Err(_) => return RedactionOutcome::Dropped(DropReason::InvalidJson),
                }
            } else {
                line.to_owned()
            }
        }
        None if looks_like_json(line) => {
            return RedactionOutcome::Dropped(DropReason::InvalidJson);
        }
        None => line.to_owned(),
    };

    output = p
        .bearer
        .replace_all(&output, format!("Bearer {MASK}"))
        .into_owned();
    output = p
        .auth_header
        .replace_all(&output, format!("$1: {MASK}"))
        .into_owned();
    output = p
        .secret_assignment
        .replace_all(&output, format!("$1={MASK}"))
        .into_owned();
    output = p.vendor_token.replace_all(&output, MASK).into_owned();
    output = p.jwt.replace_all(&output, MASK).into_owned();
    output = p.email.replace_all(&output, MASK).into_owned();
    output = p
        .card_candidate
        .replace_all(&output, |captures: &regex::Captures<'_>| {
            let digits: String = captures[0].chars().filter(char::is_ascii_digit).collect();
            if is_luhn_card(&digits) {
                MASK.to_owned()
            } else {
                captures[0].to_owned()
            }
        })
        .into_owned();

    RedactionOutcome::Kept(output)
}

fn find_json_payload(line: &str) -> Option<(&str, Value)> {
    for (index, character) in line.char_indices() {
        if character != '{' && character != '[' {
            continue;
        }
        if character == '[' {
            let rest = &line[index + character.len_utf8()..];
            let Some(next) = rest.trim_start().chars().next() else {
                continue;
            };
            if !matches!(
                next,
                '{' | '[' | '"' | '-' | '0'..='9' | 't' | 'f' | 'n' | ']'
            ) {
                continue;
            }
        }
        if let Ok(value) = serde_json::from_str::<Value>(&line[index..]) {
            return Some((&line[..index], value));
        }
    }
    None
}

/// Redact every candidate before collecting it, then enforce response limits.
/// `max_bytes` applies to the JSON-encoded message strings; the response
/// envelope and per-entry fields must be accounted for by the caller.
pub fn redact_bounded<I>(lines: I, max_lines: usize, max_bytes: usize) -> BoundedRedactionResult
where
    I: IntoIterator<Item = String>,
{
    let mut result = BoundedRedactionResult {
        lines: Vec::new(),
        kept_indices: Vec::new(),
        dropped_line_count: 0,
        truncated: false,
    };
    let mut used_bytes = 0usize;
    let mut scanned_bytes = 0usize;
    let mut in_private_key_block = false;

    for (input_index, line) in lines.into_iter().enumerate() {
        if scanned_bytes.saturating_add(line.len()) > MAX_INPUT_BYTES {
            result.truncated = true;
            break;
        }
        scanned_bytes += line.len();
        match redact_stream_line(&line, &mut in_private_key_block) {
            RedactionOutcome::Dropped(_) => {
                result.dropped_line_count = result.dropped_line_count.saturating_add(1);
            }
            RedactionOutcome::Kept(safe_line) => {
                let encoded_len = match serde_json::to_vec(&safe_line) {
                    Ok(encoded) => encoded.len(),
                    Err(_) => {
                        result.dropped_line_count = result.dropped_line_count.saturating_add(1);
                        continue;
                    }
                };
                if result.lines.len() >= max_lines
                    || used_bytes.saturating_add(encoded_len) > max_bytes
                {
                    result.truncated = true;
                    continue;
                }
                used_bytes += encoded_len;
                result.lines.push(safe_line);
                result.kept_indices.push(input_index);
            }
        }
    }

    result
}

fn redact_stream_line(line: &str, in_private_key_block: &mut bool) -> RedactionOutcome {
    let Some(patterns) = patterns() else {
        return RedactionOutcome::Dropped(DropReason::PatternInitializationFailed);
    };
    let is_begin = patterns.private_key_start.is_match(line);
    let uppercase = line.to_ascii_uppercase();
    let is_end = uppercase.contains("-----END ") && uppercase.contains("PRIVATE KEY-----");
    if *in_private_key_block || is_begin {
        *in_private_key_block = !is_end;
        return RedactionOutcome::Dropped(DropReason::PrivateKeyBlock);
    }
    if looks_like_opaque_encoded_payload(line) {
        return RedactionOutcome::Dropped(DropReason::OpaqueEncodedPayload);
    }
    redact_line(line)
}

fn looks_like_opaque_encoded_payload(line: &str) -> bool {
    let payload = line.trim();
    if payload.len() < 32 {
        return false;
    }
    let mut has_alphabetic = false;
    for byte in payload.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' => has_alphabetic = true,
            b'0'..=b'9' => {}
            b'+' | b'/' | b'=' | b'-' | b'_' => {}
            _ => return false,
        }
    }
    has_alphabetic
}

fn looks_like_json(input: &str) -> bool {
    let trimmed = input.trim_start();
    if trimmed.starts_with('{') {
        return true;
    }
    if let Some(after_bracket) = trimmed.strip_prefix('[')
        && after_bracket.trim_start().starts_with(['{', '[', ']'])
    {
        return true;
    }

    // Runtime timestamps may precede a JSON object. Treat a malformed object
    // after a valid RFC 3339 timestamp as unsafe, without classifying ordinary
    // bracketed timestamps or thread labels as malformed JSON.
    input.find('{').is_some_and(|index| {
        input[..index]
            .split_whitespace()
            .last()
            .is_some_and(|prefix| {
                let timestamp = prefix.trim_matches(['[', ']']);
                chrono::DateTime::parse_from_rfc3339(timestamp).is_ok()
            })
    })
}

fn mask_sensitive_json_keys(value: &mut Value) -> bool {
    match value {
        Value::Object(object) => {
            let mut changed = false;
            for (key, child) in object.iter_mut() {
                if is_sensitive_key(key) {
                    *child = Value::String(MASK.to_owned());
                    changed = true;
                } else {
                    changed |= mask_sensitive_json_keys(child);
                }
            }
            changed
        }
        Value::Array(values) => {
            let mut changed = false;
            for value in values {
                changed |= mask_sensitive_json_keys(value);
            }
            changed
        }
        _ => false,
    }
}

fn is_sensitive_key(key: &str) -> bool {
    let normalized: String = key
        .chars()
        .filter(char::is_ascii_alphanumeric)
        .flat_map(char::to_lowercase)
        .collect();
    [
        "password",
        "passwd",
        "pwd",
        "token",
        "apitoken",
        "accesstoken",
        "refreshtoken",
        "clienttoken",
        "sessiontoken",
        "idtoken",
        "clientsecret",
        "secret",
        "authorization",
        "auth",
        "cookie",
        "setcookie",
        "apikey",
        "credential",
        "credentials",
        "privatekey",
        "secretkey",
    ]
    .iter()
    .any(|needle| normalized == *needle)
}

fn is_luhn_card(digits: &str) -> bool {
    if !(13..=19).contains(&digits.len()) {
        return false;
    }
    let sum: u32 = digits
        .bytes()
        .rev()
        .enumerate()
        .map(|(index, byte)| {
            let mut digit = u32::from(byte - b'0');
            if index % 2 == 1 {
                digit *= 2;
                if digit > 9 {
                    digit -= 9;
                }
            }
            digit
        })
             git diff --cached -- agent/src/model.rs   .sum();
    sum.is_multiple_of(10)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn kept(line: &str) -> String {
        match redact_line(line) {
            RedactionOutcome::Kept(value) => value,
            RedactionOutcome::Dropped(reason) => panic!("unexpected drop: {reason:?}"),
        }
    }

    #[test]
    fn masks_secrets_and_personal_data_in_plain_text() {
        let output = kept(
            "Authorization: Bearer abc.def email alice@example.com jwt eyJhbGciOi.test_payload.signature",
        );
        assert!(!output.contains("abc.def"));
        assert!(!output.contains("alice@example.com"));
        assert!(!output.contains("eyJhbGciOi.test_payload.signature"));
        assert!(output.contains(MASK));
    }

    #[test]
    fn masks_nested_sensitive_json_fields_and_preserves_benign_fields() {
        let output = kept(r#"{"user":"alice","metadata":{"api_token":"abc123"}}"#);
        assert!(output.contains("alice"));
        assert!(!output.contains("abc123"));
        assert!(output.contains(MASK));
    }

    #[test]
    fn masks_common_short_sensitive_json_key() {
        let output = kept(r#"{"pwd":"hunter2"}"#);
        assert!(!output.contains("hunter2"));
    }

    #[test]
    fn keeps_non_secret_token_count_field() {
        let output = kept(r#"{"max_tokens":4096}"#);
        assert!(output.contains("4096"));
    }

    #[test]
    fn auth_header_redaction_keeps_following_log_context() {
        let output = kept("Authorization: Bearer abc more context, request completed");
        assert!(!output.contains("abc"));
        assert!(output.contains("more context"));
        assert!(output.contains("request completed"));
    }

    #[test]
    fn masks_valid_card_number_but_keeps_non_card_number() {
        let output = kept("payment 4111 1111 1111 1111 ref 1234 5678 9012 3456");
        assert!(!output.contains("4111 1111 1111 1111"));
        assert!(output.contains("1234 5678 9012 3456"));
    }

    #[test]
    fn drops_private_key_markers_and_oversized_lines() {
        assert_eq!(
            redact_line("-----BEGIN PRIVATE KEY-----"),
            RedactionOutcome::Dropped(DropReason::PrivateKeyBlock)
        );
        assert_eq!(
            redact_line(&"x".repeat(MAX_LINE_BYTES + 1)),
            RedactionOutcome::Dropped(DropReason::LineTooLong)
        );
    }

    #[test]
    fn drops_every_line_inside_a_multiline_private_key_block() {
        let result = redact_bounded(
            [
                "-----BEGIN PRIVATE KEY-----".to_owned(),
                "MIIEvQIBADANBgkqhkiG9w0BAQEFAASC".to_owned(),
                "-----END PRIVATE KEY-----".to_owned(),
                "service recovered".to_owned(),
            ],
            10,
            1024,
        );
        assert_eq!(result.dropped_line_count, 3);
        assert_eq!(result.lines, ["service recovered"]);
    }

    #[test]
    fn preserves_bracketed_plain_text_logs() {
        assert_eq!(kept("[WARN] service is slow"), "[WARN] service is slow");
    }

    #[test]
    fn drops_malformed_json_that_could_hide_nested_secrets() {
        assert_eq!(
            redact_line(r#"{"password":"secret""#),
            RedactionOutcome::Dropped(DropReason::InvalidJson)
        );
    }

    #[test]
    fn redacts_before_enforcing_line_and_encoded_byte_limits() {
        let result = redact_bounded(
            [
                "Bearer secret-token".to_owned(),
                "second safe line".to_owned(),
            ],
            1,
            100,
        );
        assert_eq!(result.lines.len(), 1);
        assert!(!result.lines[0].contains("secret-token"));
        assert!(result.truncated);
        assert_eq!(result.dropped_line_count, 0);
    }

    #[test]
    fn counts_lines_dropped_by_redaction_separately_from_truncation() {
        let result = redact_bounded(["-----BEGIN PRIVATE KEY-----".to_owned()], 10, 100);
        assert!(result.lines.is_empty());
        assert!(!result.truncated);
        assert_eq!(result.dropped_line_count, 1);
    }

    #[test]
    fn stops_scanning_an_unbounded_input_at_the_input_byte_cap() {
        let line = "x ".repeat(MAX_LINE_BYTES / 2);
        let result = redact_bounded(std::iter::repeat(line), 10, MAX_INPUT_BYTES);
        assert!(result.truncated);
        assert_eq!(result.lines.len(), 10);
    }

    #[test]
    fn masks_sensitive_json_after_timestamp_prefix() {
        let output = kept("2026-10-16T12:00:00Z {\"password\":\"hunter2\"}");
        assert!(!output.contains("hunter2"));
    }

    #[test]
    fn masks_plain_text_password_assignment() {
        let output = kept("login failed password=hunter2");
        assert!(!output.contains("hunter2"));
    }

    #[test]
    fn masks_plain_text_api_key_query_parameter() {
        let output = kept("GET /login?api_key=secret-value&mode=fast");
        assert!(!output.contains("secret-value"));
        assert!(output.contains("mode=fast"));
    }

    #[test]
    fn masks_aws_access_key_shape() {
        let output = kept("aws key AKIAIOSFODNN7EXAMPLE used");
        assert!(!output.contains("AKIAIOSFODNN7EXAMPLE"));
    }

    #[test]
    fn masks_github_token_shape() {
        let output = kept("github token ghp_12345678901234567890ABCDEF");
        assert!(!output.contains("ghp_12345678901234567890ABCDEF"));
    }

    #[test]
    fn property_masks_json_secret_with_varied_prefixes_and_whitespace() {
        let alphabet = b"abcdefghijklmnopqrstuvwxyz0123456789-_: ";
        let mut seed = 0x5eed_u32;
        for case in 0..64 {
            let mut prefix = String::new();
            for _ in 0..(case % 17) {
                seed = seed.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
                prefix.push(alphabet[(seed as usize) % alphabet.len()] as char);
            }
            let line = format!("{prefix} {{\"password\"  :  \"hunter2\", \"id\":{case}}}");
            let output = kept(&line);
            assert!(
                !output.contains("hunter2"),
                "secret escaped for generated prefix {prefix:?}: {output}"
            );
        }
    }

    #[test]
    fn keeps_bracketed_timestamp_log() {
        assert_eq!(
            kept("[2026-10-16 12:00:00] user logged in"),
            "[2026-10-16 12:00:00] user logged in"
        );
    }

    #[test]
    fn keeps_bracketed_thread_log() {
        assert_eq!(kept("[thread-1] started"), "[thread-1] started");
    }

    #[test]
    fn drops_private_key_body_even_without_begin_marker_in_query() {
        let result = redact_bounded(["MIIEvQIBADANBgkqhkiG9w0BAQEFAASC".to_owned()], 10, 1024);
        assert_eq!(result.dropped_line_count, 1);
        assert!(result.lines.is_empty());
    }

    #[test]
    fn drops_opaque_base64_body_without_digits_or_begin_marker() {
        let line = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh";
        assert!(line.len() >= 32);
        assert_eq!(
            redact_stream_line(line, &mut false),
            RedactionOutcome::Dropped(DropReason::OpaqueEncodedPayload)
        );
    }

    #[test]
    fn preserves_large_integer_in_json() {
        let input = r#"{"id":99999999999999999999}"#;
        let output = kept(input);
        assert!(output.contains("99999999999999999999"));
    }

    #[test]
    fn preserves_json_key_order_when_masking_sensitive_fields() {
        let output = kept(r#"{"z":"last","password":"hidden","a":"first"}"#);
        assert!(output.find("\"z\"").unwrap() < output.find("\"password\"").unwrap());
        assert!(output.find("\"password\"").unwrap() < output.find("\"a\"").unwrap());
        assert!(!output.contains("hidden"));
    }

    #[test]
    fn drops_malformed_json_after_an_rfc3339_prefix() {
        assert_eq!(
            redact_line("2026-10-16T12:00:00Z {\"password\":\"unclosed-secret"),
            RedactionOutcome::Dropped(DropReason::InvalidJson)
        );
    }

    #[test]
    fn property_masks_secrets_in_plain_and_json_wrappers() {
        let mut seed = 0x0005_1ec7_u32;
        for case in 0..64 {
            seed = seed.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
            let prefix = format!("request-{seed:08x} ");
            let line = if case % 2 == 0 {
                format!("{prefix}password=known-secret-{case}")
            } else {
                format!("{prefix}{{\"message\":\"token: known-secret-{case}\"}}")
            };
            let output = kept(&line);
            assert!(
                !output.contains(&format!("known-secret-{case}")),
                "secret escaped for input {line:?}: {output}"
            );
        }
    }
}
