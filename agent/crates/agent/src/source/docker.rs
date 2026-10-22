use super::{Fetched, Params, err, ns_to_ts, ts_to_ns};
use crate::config;
use chrono::DateTime;
use seismo_proto::{self as pb, LogStream, MatchKind, SourceError, SourceErrorKind};
use std::{
    collections::{HashMap, VecDeque},
    process::Stdio,
};
use tokio::{
    io::{AsyncBufReadExt, AsyncRead, BufReader},
    process::Command,
};

type Row = (i128, LogStream, String);

pub struct Docker {
    command: Vec<String>,
    containers: HashMap<String, String>,
    host: String,
}

impl Docker {
    pub fn new(cfg: &config::Docker) -> Self {
        Self {
            command: cfg.command.clone(),
            containers: cfg.containers.clone(),
            host: hostname::get()
                .ok()
                .and_then(|h| h.into_string().ok())
                .unwrap_or_default(),
        }
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        let container = self
            .containers
            .get(p.service)
            .map_or(p.service, String::as_str);
        let stamp = |t: &prost_types::Timestamp| {
            DateTime::from_timestamp(t.seconds, t.nanos as u32)
                .map(|d| d.format("%Y-%m-%dT%H:%M:%S%.9fZ").to_string())
                .unwrap_or_default()
        };
        let mut cmd = Command::new(&self.command[0]);
        cmd.args(&self.command[1..])
            .args([
                "logs",
                "--timestamps",
                "--since",
                &stamp(p.from),
                "--until",
                &stamp(p.to),
                container,
            ])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);

        let run = collect(
            cmd,
            ts_to_ns(p.from),
            ts_to_ns(p.to),
            p.trace_id,
            p.limit as usize,
        );
        let (mut rows, hit_limit) = tokio::time::timeout(p.timeout, run)
            .await
            .map_err(|_| err(SourceErrorKind::Timeout, "docker logs timed out"))??;

        // stdout and stderr came in separately, interleave them by time. stable,
        // so equal timestamps keep stdout before stderr
        rows.sort_by_key(|r| r.0);
        if rows.len() > p.limit as usize {
            rows.drain(..rows.len() - p.limit as usize);
        }
        let match_kind = if p.trace_id.is_some() {
            MatchKind::Trace
        } else {
            MatchKind::Window
        };
        let entries = rows
            .into_iter()
            .map(|(ns, stream, line)| pb::LogEntry {
                timestamp: Some(ns_to_ts(ns)),
                stream: stream as i32,
                message: line,
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

async fn collect(
    mut cmd: Command,
    from: i128,
    to: i128,
    trace: Option<&str>,
    limit: usize,
) -> Result<(Vec<Row>, bool), SourceError> {
    let mut child = cmd.spawn().map_err(|e| match e.kind() {
        std::io::ErrorKind::NotFound => err(SourceErrorKind::DriverUnsupported, "docker not found"),
        std::io::ErrorKind::PermissionDenied => err(SourceErrorKind::AccessDenied, e.to_string()),
        _ => err(SourceErrorKind::SourceUnreachable, e.to_string()),
    })?;
    let (out, errs) = tokio::join!(
        read_pipe(
            child.stdout.take().expect("stdout is piped"),
            LogStream::Stdout,
            from,
            to,
            trace,
            limit
        ),
        read_pipe(
            child.stderr.take().expect("stderr is piped"),
            LogStream::Stderr,
            from,
            to,
            trace,
            limit
        ),
    );
    let (out, errs) = (out.map_err(io_err)?, errs.map_err(io_err)?);

    let status = child.wait().await.map_err(io_err)?;
    if !status.success() {
        // docker's own errors come on stderr without a timestamp
        return Err(failure(&errs.junk));
    }
    let hit_limit = out.overflowed || errs.overflowed;
    let rows = out.rows.into_iter().chain(errs.rows).collect();
    Ok((rows, hit_limit))
}

fn io_err(e: std::io::Error) -> SourceError {
    err(SourceErrorKind::SourceUnreachable, e.to_string())
}

/// Maps `docker logs` complaints to the proto's error kinds.
fn failure(stderr: &str) -> SourceError {
    let s = stderr.to_lowercase();
    let kind = if s.contains("no such container") {
        SourceErrorKind::WorkloadNotFound
    } else if s.contains("does not support reading") {
        SourceErrorKind::DriverUnsupported
    } else if s.contains("permission denied") {
        SourceErrorKind::AccessDenied
    } else {
        // includes "Cannot connect to the Docker daemon"
        SourceErrorKind::SourceUnreachable
    };
    err(
        kind,
        format!(
            "docker logs failed: {}",
            stderr.trim().chars().take(200).collect::<String>()
        ),
    )
}

#[derive(Default)]
struct Piped {
    /// Newest `limit` matching lines of this pipe.
    rows: VecDeque<Row>,
    overflowed: bool,
    /// Lines without a docker timestamp, only interesting when docker failed.
    junk: String,
}

impl Piped {
    fn note_junk(&mut self, line: &str) {
        // only the first lines matter for the error message
        if self.junk.len() < 4096 {
            self.junk.push_str(line);
            self.junk.push('\n');
        }
    }
}

async fn read_pipe(
    r: impl AsyncRead + Unpin,
    stream: LogStream,
    from: i128,
    to: i128,
    trace: Option<&str>,
    limit: usize,
) -> std::io::Result<Piped> {
    let mut p = Piped::default();
    let mut lines = BufReader::new(r).lines();
    while let Some(line) = lines.next_line().await? {
        let Some((stamp, msg)) = line.split_once(' ') else {
            p.note_junk(&line);
            continue;
        };
        let Some(ns) = DateTime::parse_from_rfc3339(stamp)
            .ok()
            .and_then(|t| t.timestamp_nanos_opt())
            .map(i128::from)
        else {
            p.note_junk(&line);
            continue;
        };
        if ns < from || ns > to || trace.is_some_and(|t| !msg.contains(t)) {
            continue;
        }
        if p.rows.len() == limit {
            p.rows.pop_front();
            p.overflowed = true;
        }
        p.rows.push_back((ns, stream, msg.to_owned()));
    }
    Ok(p)
}
