use super::{Fetched, Params, Raw, err, finish, ts_to_ns};
use crate::config;
use chrono::DateTime;
use seismo_proto::{LogStream, SourceError, SourceErrorKind};
use std::{
    collections::{HashMap, VecDeque},
    process::Stdio,
};
use tokio::{
    io::{AsyncBufReadExt, AsyncRead, BufReader},
    process::Command,
};

pub struct Docker {
    command: Vec<String>,
    containers: HashMap<String, String>,
    host: String,
}

/// What a line has to satisfy to be kept.
#[derive(Clone, Copy)]
struct Filter<'a> {
    from: i128,
    to: i128,
    trace: Option<&'a str>,
    limit: usize,
    context: usize,
}

impl Filter<'_> {
    fn want_ctx(&self) -> bool {
        self.trace.is_some() && self.context > 0
    }

    /// Lines to remember per pipe. Neighbours of a match can be anywhere
    /// near it, so with context we keep room for all of them.
    fn cap(&self) -> usize {
        if self.want_ctx() {
            self.limit.saturating_mul(2 * self.context + 1)
        } else {
            self.limit
        }
    }
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

        let filter = Filter {
            from: ts_to_ns(p.from),
            to: ts_to_ns(p.to),
            trace: p.trace_id,
            limit: p.limit as usize,
            context: p.context as usize,
        };
        let (mut rows, hit_limit) = tokio::time::timeout(p.timeout, collect(cmd, filter))
            .await
            .map_err(|_| err(SourceErrorKind::Timeout, "docker logs timed out"))??;

        // stdout and stderr came in separately, interleave them by time. stable,
        // so equal timestamps keep stdout before stderr
        rows.sort_by_key(|r| r.ts);
        let entries = finish(rows, p.trace_id.is_some(), filter.context, filter.limit);
        Ok(Fetched {
            entries,
            scope_label: self.host.clone(),
            hit_limit,
        })
    }
}

async fn collect(mut cmd: Command, f: Filter<'_>) -> Result<(Vec<Raw>, bool), SourceError> {
    let mut child = cmd.spawn().map_err(|e| match e.kind() {
        std::io::ErrorKind::NotFound => err(SourceErrorKind::DriverUnsupported, "docker not found"),
        std::io::ErrorKind::PermissionDenied => err(SourceErrorKind::AccessDenied, e.to_string()),
        _ => err(SourceErrorKind::SourceUnreachable, e.to_string()),
    })?;
    let (out, errs) = tokio::join!(
        read_pipe(
            child.stdout.take().expect("stdout is piped"),
            LogStream::Stdout,
            f
        ),
        read_pipe(
            child.stderr.take().expect("stderr is piped"),
            LogStream::Stderr,
            f
        ),
    );
    let (out, errs) = (out.map_err(io_err)?, errs.map_err(io_err)?);

    let status = child.wait().await.map_err(io_err)?;
    if !status.success() {
        // docker's own errors come on stderr without a timestamp
        return Err(failure(&errs.junk));
    }
    let overflowed = out.overflowed || errs.overflowed;
    let rows: Vec<Raw> = out.rows.into_iter().chain(errs.rows).collect();
    // more matches than we may return also means older ones get cut
    let hit_limit = overflowed || rows.iter().filter(|r| r.matched).count() > f.limit;
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
    /// Newest lines of this pipe, up to `Filter::cap`.
    rows: VecDeque<Raw>,
    /// A matching line fell off the front.
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
    f: Filter<'_>,
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
        let matched = f.trace.is_none_or(|t| msg.contains(t));
        if ns < f.from || ns > f.to || !(matched || f.want_ctx()) {
            continue;
        }
        if p.rows.len() == f.cap()
            && let Some(old) = p.rows.pop_front()
        {
            p.overflowed |= old.matched;
        }
        p.rows.push_back(Raw {
            ts: ns,
            stream,
            text: msg.to_owned(),
            matched,
        });
    }
    Ok(p)
}
