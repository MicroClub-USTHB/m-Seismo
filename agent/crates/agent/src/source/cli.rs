//! Reads logs from a CLI that prints `<rfc3339> <message>` on stdout and
//! stderr, like `docker logs --timestamps` and `kubectl logs --timestamps`.
//! Lines without a timestamp are the tool's own complaints, kept for errors.

use super::{Raw, err};
use chrono::DateTime;
use seismo_proto::{LogStream, SourceError, SourceErrorKind};
use std::{collections::VecDeque, process::Stdio};
use tokio::{
    io::{AsyncBufReadExt, AsyncRead, BufReader},
    process::Command,
};

/// What a line has to satisfy to be kept.
#[derive(Clone, Copy)]
pub(crate) struct Filter<'a> {
    pub from: i128,
    pub to: i128,
    pub trace: Option<&'a str>,
    pub limit: usize,
    pub context: usize,
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

/// Runs a log command and keeps the lines `f` asks for, `(rows, hit_limit)`.
/// `tool` names the program in errors, `classify` maps its stderr to a kind.
pub(crate) async fn collect(
    mut cmd: Command,
    f: Filter<'_>,
    tool: &str,
    classify: fn(&str) -> SourceErrorKind,
) -> Result<(Vec<Raw>, bool), SourceError> {
    cmd.stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);
    let mut child = cmd.spawn().map_err(|e| spawn_error(tool, e))?;
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
        // the tool's own errors come on stderr without a timestamp
        return Err(err(
            classify(&errs.junk),
            format!(
                "{tool} logs failed: {}",
                errs.junk.trim().chars().take(200).collect::<String>()
            ),
        ));
    }
    let overflowed = out.overflowed || errs.overflowed;
    let rows: Vec<Raw> = out.rows.into_iter().chain(errs.rows).collect();
    // more matches than we may return also means older ones get cut
    let hit_limit = overflowed || rows.iter().filter(|r| r.matched).count() > f.limit;
    Ok((rows, hit_limit))
}

/// The program itself is missing or not ours to run, not a failed command.
pub(crate) fn spawn_error(tool: &str, e: std::io::Error) -> SourceError {
    match e.kind() {
        std::io::ErrorKind::NotFound => err(
            SourceErrorKind::DriverUnsupported,
            format!("{tool} not found"),
        ),
        std::io::ErrorKind::PermissionDenied => err(SourceErrorKind::AccessDenied, e.to_string()),
        _ => err(SourceErrorKind::SourceUnreachable, e.to_string()),
    }
}

pub(crate) fn io_err(e: std::io::Error) -> SourceError {
    err(SourceErrorKind::SourceUnreachable, e.to_string())
}

#[derive(Default)]
struct Piped {
    /// Newest lines of this pipe, up to `Filter::cap`.
    rows: VecDeque<Raw>,
    /// A matching line fell off the front.
    overflowed: bool,
    /// Lines without a timestamp, only interesting when the tool failed.
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
