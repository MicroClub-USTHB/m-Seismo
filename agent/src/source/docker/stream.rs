//! Bounded decoder for Docker log stream chunks.

use chrono::{DateTime, Utc};

use crate::model::{LogStream, RawLogLine, ReadLimits};

const MAX_LINE_BYTES: usize = 16 * 1024;

#[derive(Debug)]
pub(super) struct DecodedLogs {
    pub lines: Vec<RawLogLine>,
    pub discarded_line_count: u64,
    pub truncated: bool,
}

pub(super) struct LineDecoder {
    lines: Vec<RawLogLine>,
    pending: Vec<u8>,
    pending_stream: Option<LogStream>,
    dropping_oversized_line: bool,
    bytes_seen: usize,
    discarded_line_count: u64,
    truncated: bool,
    limits: ReadLimits,
}

impl LineDecoder {
    pub fn new(limits: ReadLimits) -> Self {
        Self {
            lines: Vec::new(),
            pending: Vec::new(),
            pending_stream: None,
            dropping_oversized_line: false,
            bytes_seen: 0,
            discarded_line_count: 0,
            truncated: false,
            limits,
        }
    }

    pub fn push(&mut self, stream: LogStream, bytes: &[u8]) -> bool {
        for &byte in bytes {
            if self.bytes_seen >= self.limits.max_bytes || self.lines.len() >= self.limits.max_lines
            {
                self.truncated = true;
                if self.dropping_oversized_line {
                    self.dropping_oversized_line = false;
                } else if !self.pending.is_empty() {
                    self.discarded_line_count = self.discarded_line_count.saturating_add(1);
                }
                self.pending.clear();
                self.pending_stream = None;
                return false;
            }
            self.bytes_seen += 1;
            if byte == b'\n' {
                self.finish_line();
                if self.lines.len() >= self.limits.max_lines {
                    self.truncated = true;
                    return false;
                }
                continue;
            }

            if self.dropping_oversized_line {
                continue;
            }
            if self
                .pending_stream
                .is_some_and(|previous| previous != stream)
            {
                self.discard_current_line();
            }
            self.pending_stream = Some(stream);
            if self.pending.len() >= MAX_LINE_BYTES {
                self.discarded_line_count = self.discarded_line_count.saturating_add(1);
                self.pending.clear();
                self.pending_stream = None;
                self.dropping_oversized_line = true;
            } else {
                self.pending.push(byte);
            }
        }
        true
    }

    pub fn finish(mut self) -> DecodedLogs {
        if self.dropping_oversized_line {
            // The oversized line was already counted when it crossed its cap.
        } else if !self.pending.is_empty() {
            self.finish_line();
        }
        DecodedLogs {
            lines: self.lines,
            discarded_line_count: self.discarded_line_count,
            truncated: self.truncated,
        }
    }

    fn finish_line(&mut self) {
        if self.dropping_oversized_line {
            self.dropping_oversized_line = false;
            self.pending.clear();
            self.pending_stream = None;
            return;
        }
        let bytes = std::mem::take(&mut self.pending);
        let stream = self.pending_stream.take().unwrap_or(LogStream::Unknown);
        let bytes = bytes.strip_suffix(b"\r").unwrap_or(&bytes);
        match std::str::from_utf8(bytes) {
            Ok(line) => match parse_timestamped_line(line) {
                Some((timestamp, message)) => self.lines.push(RawLogLine {
                    timestamp: Some(timestamp),
                    stream,
                    message,
                }),
                None => {
                    self.discarded_line_count = self.discarded_line_count.saturating_add(1);
                }
            },
            Err(_) => {
                self.discarded_line_count = self.discarded_line_count.saturating_add(1);
            }
        }
    }

    fn discard_current_line(&mut self) {
        self.discarded_line_count = self.discarded_line_count.saturating_add(1);
        self.pending.clear();
        self.pending_stream = None;
    }
}

fn parse_timestamped_line(line: &str) -> Option<(DateTime<Utc>, String)> {
    let (timestamp, message) = line.split_once(' ')?;
    let timestamp = DateTime::parse_from_rfc3339(timestamp)
        .ok()?
        .with_timezone(&Utc);
    Some((timestamp, message.to_owned()))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn limits() -> ReadLimits {
        ReadLimits {
            max_lines: 10,
            max_bytes: 1024,
        }
    }

    #[test]
    fn decodes_timestamp_and_stdout_stderr_streams_from_chunks() {
        let mut decoder = LineDecoder::new(limits());
        assert!(decoder.push(LogStream::Stdout, b"2026-10-16T12:00:00Z first\n"));
        assert!(decoder.push(LogStream::Stderr, b"2026-10-16T12:00:01Z second\n"));
        let decoded = decoder.finish();
        assert_eq!(decoded.lines.len(), 2);
        assert_eq!(decoded.lines[0].message, "first");
        assert_eq!(decoded.lines[0].stream, LogStream::Stdout);
        assert_eq!(decoded.lines[1].message, "second");
        assert_eq!(decoded.lines[1].stream, LogStream::Stderr);
    }

    #[test]
    fn joins_chunks_and_drops_invalid_timestamp_or_utf8() {
        let mut decoder = LineDecoder::new(limits());
        assert!(decoder.push(LogStream::Stdout, b"2026-10-16T12:00:"));
        assert!(decoder.push(LogStream::Stdout, b"00Z split\nnot-a-time leak\n"));
        assert!(decoder.push(LogStream::Stdout, b"2026-10-16T12:00:01Z \xff\n"));
        let decoded = decoder.finish();
        assert_eq!(decoded.lines.len(), 1);
        assert_eq!(decoded.lines[0].message, "split");
        assert_eq!(decoded.discarded_line_count, 2);
    }

    #[test]
    fn bounds_oversized_lines_and_total_stream_bytes() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: 10,
        });
        assert!(!decoder.push(LogStream::Stdout, b"01234567890"));
        let decoded = decoder.finish();
        assert!(decoded.truncated);
        assert!(decoded.lines.is_empty());

        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: MAX_LINE_BYTES * 2,
        });
        let oversized = format!("2026-10-16T12:00:00Z {}\n", "x".repeat(MAX_LINE_BYTES));
        assert!(decoder.push(LogStream::Stdout, oversized.as_bytes()));
        let decoded = decoder.finish();
        assert!(decoded.lines.is_empty());
        assert_eq!(decoded.discarded_line_count, 1);
    }

    #[test]
    fn does_not_return_a_log_fragment_cut_off_by_the_total_byte_limit() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: 30,
        });
        assert!(!decoder.push(
            LogStream::Stdout,
            b"2026-10-16T12:00:00Z this line is cut off"
        ));
        let decoded = decoder.finish();
        assert!(decoded.truncated);
        assert!(decoded.lines.is_empty());
        assert_eq!(decoded.discarded_line_count, 1);
    }
}
