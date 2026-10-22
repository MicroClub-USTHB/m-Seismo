use super::{Fetched, Params, err, ns_to_ts, ts_to_ns};
use crate::{config, redaction::redact};
use chrono::{DateTime, NaiveDateTime};
use seismo_proto::{self as pb, LogStream, MatchKind, SourceError, SourceErrorKind};
use std::{
    collections::HashMap,
    fs,
    io::{self, Read, Seek, SeekFrom},
    path::{Path, PathBuf},
};

const CHUNK: u64 = 64 << 10;
// PERF: hard stop for one fetch, a window deep in a 50GB log gets reported
// as truncated instead of pinning a thread for minutes
const MAX_SCAN_BYTES: u64 = 64 << 20;

pub struct File {
    files: HashMap<String, PathBuf>,
    rotated: u32,
    host: String,
}

impl File {
    pub fn new(cfg: &config::File) -> Self {
        Self {
            files: cfg.files.clone(),
            rotated: cfg.rotated,
            host: hostname::get()
                .ok()
                .and_then(|h| h.into_string().ok())
                .unwrap_or_default(),
        }
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        let base = self.files.get(p.service).ok_or_else(|| {
            err(
                SourceErrorKind::WorkloadNotFound,
                "no file configured for service",
            )
        })?;
        // current file first, then path.1, path.2... (older and older)
        let paths: Vec<PathBuf> = std::iter::once(base.clone())
            .chain((1..=self.rotated).map(|i| {
                let mut s = base.clone().into_os_string();
                s.push(format!(".{i}"));
                PathBuf::from(s)
            }))
            .collect();
        let (from, to) = (ts_to_ns(p.from), ts_to_ns(p.to));
        let (trace, limit) = (p.trace_id.map(str::to_owned), p.limit as usize);

        // NOTE: blocking io on purpose, the timeout only abandons the thread,
        // MAX_SCAN_BYTES is what actually bounds it
        let job =
            tokio::task::spawn_blocking(move || scan(&paths, from, to, trace.as_deref(), limit));
        let (rows, hit_limit) = tokio::time::timeout(p.timeout, job)
            .await
            .map_err(|_| err(SourceErrorKind::Timeout, "file scan timed out"))?
            .map_err(|e| err(SourceErrorKind::SourceUnreachable, e.to_string()))??;

        let match_kind = if p.trace_id.is_some() {
            MatchKind::Trace
        } else {
            MatchKind::Window
        };
        let entries = rows
            .into_iter()
            .map(|(ns, line)| pb::LogEntry {
                timestamp: Some(ns_to_ts(ns)),
                stream: LogStream::Unknown as i32,
                message: redact(&line),
                match_kind: match_kind as i32,
            })
            .collect();
        Ok(Fetched {
            entries,
            scope_label: self.host.clone(),
            hit_limit,
        })
    }
}

fn io_err(path: &Path, e: io::Error) -> SourceError {
    let kind = match e.kind() {
        io::ErrorKind::NotFound => SourceErrorKind::WorkloadNotFound,
        io::ErrorKind::PermissionDenied => SourceErrorKind::AccessDenied,
        _ => SourceErrorKind::SourceUnreachable,
    };
    err(kind, format!("{}: {e}", path.display()))
}

/// Newest lines inside [from, to], oldest first, plus whether it stopped at `limit`.
fn scan(
    paths: &[PathBuf],
    from: i128,
    to: i128,
    trace: Option<&str>,
    limit: usize,
) -> Result<(Vec<(i128, String)>, bool), SourceError> {
    // collected newest -> oldest, flipped at the end
    let mut rows: Vec<(i128, String)> = Vec::new();
    // lines without a timestamp (stack traces...) belong to the header line
    // *above* them, which we only meet later since we read backwards. they
    // can even spill into the next (older) file
    let mut pending: Vec<String> = Vec::new();
    let (mut scanned, mut hit_limit) = (0u64, false);

    'files: for (i, path) in paths.iter().enumerate() {
        let mut lines = match RevLines::open(path) {
            Ok(l) => l,
            // a missing rotation just means there is nothing older
            Err(e) if i > 0 && e.kind() == io::ErrorKind::NotFound => break,
            Err(e) => return Err(io_err(path, e)),
        };
        while let Some(line) = lines.next().map_err(|e| io_err(path, e))? {
            scanned += line.len() as u64 + 1;
            if scanned > MAX_SCAN_BYTES {
                hit_limit = true;
                break 'files;
            }
            let Some(ts) = parse_ts(&line) else {
                pending.push(line);
                continue;
            };
            // the group = this header + its continuation lines, same timestamp
            let group = pending.drain(..).chain(std::iter::once(line));
            if ts < from {
                break 'files; // everything above here is older still
            }
            if ts <= to {
                rows.extend(
                    group
                        .filter(|l| trace.is_none_or(|t| l.contains(t)))
                        .map(|l| (ts, l)),
                );
                if rows.len() >= limit {
                    rows.truncate(limit);
                    hit_limit = true;
                    break 'files;
                }
            }
        }
    }
    rows.reverse();
    Ok((rows, hit_limit))
}

/// Reads a file line by line from the end.
struct RevLines {
    file: fs::File,
    pos: u64,
    /// Start of a line whose beginning we haven't read yet.
    carry: Vec<u8>,
    /// Complete lines of the last chunk, next one to hand out is the last.
    ready: Vec<Vec<u8>>,
}

impl RevLines {
    fn open(path: &Path) -> io::Result<Self> {
        let file = fs::File::open(path)?;
        let pos = file.metadata()?.len();
        Ok(Self {
            file,
            pos,
            carry: Vec::new(),
            ready: Vec::new(),
        })
    }

    fn next(&mut self) -> io::Result<Option<String>> {
        loop {
            if let Some(raw) = self.ready.pop() {
                let line = String::from_utf8_lossy(&raw);
                let line = line.trim_end_matches('\r');
                if line.trim().is_empty() {
                    continue;
                }
                return Ok(Some(line.to_owned()));
            }
            if self.pos == 0 {
                return Ok(match std::mem::take(&mut self.carry) {
                    c if c.is_empty() => None,
                    c => Some(
                        String::from_utf8_lossy(&c)
                            .trim_end_matches('\r')
                            .to_owned(),
                    ),
                });
            }
            let n = self.pos.min(CHUNK);
            self.pos -= n;
            let mut buf = vec![0; n as usize];
            self.file.seek(SeekFrom::Start(self.pos))?;
            self.file.read_exact(&mut buf)?;
            buf.append(&mut self.carry);
            let mut parts = buf.split(|b| *b == b'\n').map(<[u8]>::to_vec);
            // first part may be cut in the middle, wait for the chunk before
            self.carry = parts.next().unwrap_or_default();
            self.ready = parts.collect();
        }
    }
}

/// Timestamp of a log line in ns, if it starts with / carries a known format:
/// JSON with time|timestamp|ts|@timestamp|t, RFC3339, or `YYYY-MM-DD HH:MM:SS[.,fff]` (UTC).
fn parse_ts(line: &str) -> Option<i128> {
    let line = line.trim_start();
    if line.starts_with('{') {
        let v: serde_json::Value = serde_json::from_str(line).ok()?;
        return ["time", "timestamp", "ts", "@timestamp", "t"]
            .iter()
            .find_map(|k| v.get(k))
            .and_then(|v| match v {
                serde_json::Value::String(s) => parse_text_ts(s),
                serde_json::Value::Number(n) => epoch_to_ns(n.as_f64()?),
                _ => None,
            });
    }
    parse_text_ts(line)
}

fn parse_text_ts(s: &str) -> Option<i128> {
    let s = s.trim_start().trim_start_matches('[');
    let mut words = s.split_whitespace();
    let first = words.next()?.trim_end_matches([']', ',']);
    if let Ok(t) = DateTime::parse_from_rfc3339(first) {
        return t.timestamp_nanos_opt().map(i128::from);
    }
    let second = words.next()?.trim_end_matches(']').replace(',', ".");
    NaiveDateTime::parse_from_str(&format!("{first} {second}"), "%Y-%m-%d %H:%M:%S%.f")
        .ok()?
        .and_utc()
        .timestamp_nanos_opt()
        .map(i128::from)
}

/// Epoch number to ns, guessing the unit from its magnitude.
fn epoch_to_ns(n: f64) -> Option<i128> {
    let scale = match n {
        n if n <= 0.0 => return None,
        n if n < 1e11 => 1e9, // seconds
        n if n < 1e14 => 1e6, // millis
        n if n < 1e17 => 1e3, // micros
        _ => 1.0,
    };
    Some((n * scale) as i128)
}
