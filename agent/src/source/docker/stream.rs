//! Bounded decoder for Docker log stream chunks.

use std::collections::VecDeque;

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
    lines: VecDeque<RawLogLine>,
    kept_bytes: usize,
    pending: Vec<u8>,
    pending_stream: Option<LogStream>,
    dropping_oversized_line: bool,
    discarded_line_count: u64,
    truncated: bool,
    limits: ReadLimits,
}

impl LineDecoder {
    pub fn new(limits: ReadLimits) -> Self {
        Self {
            lines: VecDeque::new(),
            kept_bytes: 0,
            pending: Vec::new(),
            pending_stream: None,
            dropping_oversized_line: false,
            discarded_line_count: 0,
            truncated: false,
            limits,
        }
    }

    // NOTE: Docker sends oldest-first, so over a limit we evict from the
    // front and keep the newest lines. Never stops early.
    pub fn push(&mut self, stream: LogStream, bytes: &[u8]) {
        for &byte in bytes {
            if byte == b'\n' {
                self.finish_line();
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
    }

    pub fn finish(mut self) -> DecodedLogs {
        if self.dropping_oversized_line {
            // The oversized line was already counted when it crossed its cap.
        } else if !self.pending.is_empty() {
            self.finish_line();
        }
        DecodedLogs {
            lines: self.lines.into(),
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
                Some((timestamp, message)) => self.keep(RawLogLine {
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

    fn keep(&mut self, line: RawLogLine) {
        self.kept_bytes = self.kept_bytes.saturating_add(line.message.len());
        self.lines.push_back(line);
        while self.lines.len() > self.limits.max_lines || self.kept_bytes > self.limits.max_bytes {
            let Some(oldest) = self.lines.pop_front() else {
                break;
            };
            self.kept_bytes -= oldest.message.len();
            self.truncated = true;
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
        decoder.push(LogStream::Stdout, b"2026-10-16T12:00:00Z first\n");
        decoder.push(LogStream::Stderr, b"2026-10-16T12:00:01Z second\n");
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
        decoder.push(LogStream::Stdout, b"2026-10-16T12:00:");
        decoder.push(LogStream::Stdout, b"00Z split\nnot-a-time leak\n");
        decoder.push(LogStream::Stdout, b"2026-10-16T12:00:01Z \xff\n");
        let decoded = decoder.finish();
        assert_eq!(decoded.lines.len(), 1);
        assert_eq!(decoded.lines[0].message, "split");
        assert_eq!(decoded.discarded_line_count, 2);
    }

    #[test]
    fn evicts_oldest_lines_when_the_line_limit_is_reached() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 2,
            max_bytes: 1024,
        });
        decoder.push(
            LogStream::Stdout,
            b"2026-10-16T12:00:00Z first\n2026-10-16T12:00:01Z second\n",
        );
        decoder.push(LogStream::Stdout, b"2026-10-16T12:00:02Z third\n");
        let decoded = decoder.finish();
        let messages: Vec<_> = decoded
            .lines
            .iter()
            .map(|line| line.message.as_str())
            .collect();
        assert_eq!(messages, ["second", "third"]);
        assert!(decoded.truncated);
        assert_eq!(decoded.discarded_line_count, 0);
    }

    #[test]
    fn evicts_oldest_lines_when_the_byte_budget_is_reached() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: 10,
        });
        decoder.push(
            LogStream::Stdout,
            b"2026-10-16T12:00:00Z aaaa\n2026-10-16T12:00:01Z bbbb\n2026-10-16T12:00:02Z cccc\n",
        );
        let decoded = decoder.finish();
        let messages: Vec<_> = decoded
            .lines
            .iter()
            .map(|line| line.message.as_str())
            .collect();
        assert_eq!(messages, ["bbbb", "cccc"]);
        assert!(decoded.truncated);
    }

    #[test]
    fn never_returns_a_fragment_of_a_line_larger_than_the_byte_budget() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: 10,
        });
        decoder.push(
            LogStream::Stdout,
            b"2026-10-16T12:00:00Z this line is cut off",
        );
        let decoded = decoder.finish();
        assert!(decoded.truncated);
        assert!(decoded.lines.is_empty());
        assert_eq!(decoded.discarded_line_count, 0);
    }

    #[test]
    fn discards_oversized_lines_without_storing_them() {
        let mut decoder = LineDecoder::new(ReadLimits {
            max_lines: 10,
            max_bytes: MAX_LINE_BYTES * 2,
        });
        let oversized = format!("2026-10-16T12:00:00Z {}\n", "x".repeat(MAX_LINE_BYTES));
        decoder.push(LogStream::Stdout, oversized.as_bytes());
        let decoded = decoder.finish();
        assert!(decoded.lines.is_empty());
        assert_eq!(decoded.discarded_line_count, 1);
    }
}
