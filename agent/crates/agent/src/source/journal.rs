use super::{Fetched, Params, Raw, err, finish, ts_to_ns};
use crate::config;
use chrono::DateTime;
use seismo_proto::{LogStream, SourceError, SourceErrorKind};
use std::{collections::HashMap, process::Stdio};
use tokio::{
    io::{AsyncBufReadExt, AsyncReadExt, BufReader},
    process::Command,
};

// PERF: same idea as the file source, a huge window gets cut and reported
const MAX_SCAN_BYTES: u64 = 64 << 20;

pub struct Journal {
    command: Vec<String>,
    units: HashMap<String, String>,
}

impl Journal {
    pub fn new(cfg: &config::Journal) -> Self {
        Self {
            command: cfg.command.clone(),
            units: cfg.units.clone(),
        }
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        let unit = self
            .units
            .get(p.service)
            .cloned()
            .unwrap_or_else(|| format!("{}.service", p.service));
        // journalctl only takes whole seconds, so ask for a slightly wider
        // range and cut to the exact window ourselves
        let secs = |t: i64| {
            DateTime::from_timestamp(t, 0)
                .map(|d| d.format("%Y-%m-%d %H:%M:%S").to_string())
                .unwrap_or_default()
        };
        let mut cmd = Command::new(&self.command[0]);
        cmd.args(&self.command[1..])
            .args(["--no-pager", "--utc", "-o", "json", "-r", "-u", &unit])
            .args(["--since", &secs(p.from.seconds)])
            .args(["--until", &secs(p.to.seconds + 1)])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);

        let (from, to) = (ts_to_ns(p.from), ts_to_ns(p.to));
        let run = collect(
            cmd,
            from,
            to,
            p.trace_id,
            p.limit as usize,
            p.context as usize,
        );
        let (mut rows, hit_limit, host) = tokio::time::timeout(p.timeout, run)
            .await
            .map_err(|_| err(SourceErrorKind::Timeout, "journalctl timed out"))??;

        // journalctl -r gave us newest first
        rows.reverse();
        let entries = finish(
            rows,
            p.trace_id.is_some(),
            p.context as usize,
            p.limit as usize,
        );
        Ok(Fetched {
            entries,
            scope_label: host,
            hit_limit,
        })
    }
}

/// Newest first, like journalctl -r gave them.
type Rows = (Vec<Raw>, bool, String);

async fn collect(
    mut cmd: Command,
    from: i128,
    to: i128,
    trace: Option<&str>,
    limit: usize,
    context: usize,
) -> Result<Rows, SourceError> {
    let mut child = cmd.spawn().map_err(|e| match e.kind() {
        std::io::ErrorKind::NotFound => {
            err(SourceErrorKind::DriverUnsupported, "journalctl not found")
        }
        std::io::ErrorKind::PermissionDenied => err(SourceErrorKind::AccessDenied, e.to_string()),
        _ => err(SourceErrorKind::SourceUnreachable, e.to_string()),
    })?;
    let mut lines = BufReader::new(child.stdout.take().expect("stdout is piped")).lines();
    let (mut rows, mut host, mut scanned) = (Vec::new(), String::new(), 0u64);
    let (mut matches, mut extra, mut hit_limit) = (0usize, context, false);
    let want_ctx = trace.is_some() && context > 0;
    let io = |e: std::io::Error| err(SourceErrorKind::SourceUnreachable, e.to_string());

    while let Some(line) = lines.next_line().await.map_err(io)? {
        scanned += line.len() as u64 + 1;
        if scanned > MAX_SCAN_BYTES {
            return Ok((rows, true, host));
        }
        let Ok(v) = serde_json::from_str::<serde_json::Value>(&line) else {
            continue;
        };
        let Some(ns) = v["__REALTIME_TIMESTAMP"]
            .as_str()
            .and_then(|s| s.parse::<i128>().ok())
            .map(|us| us * 1000)
        else {
            continue;
        };
        if ns < from {
            break; // newest first, nothing older can match
        }
        if ns > to {
            continue;
        }
        let msg = match &v["MESSAGE"] {
            serde_json::Value::String(s) => s.clone(),
            // journald encodes non-utf8 messages as a byte array
            serde_json::Value::Array(a) => {
                let bytes: Vec<u8> = a
                    .iter()
                    .filter_map(|b| b.as_u64().map(|b| b as u8))
                    .collect();
                String::from_utf8_lossy(&bytes).into_owned()
            }
            _ => continue,
        };
        let matched = trace.is_none_or(|t| msg.contains(t));
        if host.is_empty()
            && let Some(h) = v["_HOSTNAME"].as_str()
        {
            host = h.to_owned();
        }
        let raw = |matched| Raw {
            ts: ns,
            stream: LogStream::Unknown,
            text: msg,
            matched,
        };
        if hit_limit {
            // all matches found, only the older neighbours are left
            if extra == 0 {
                return Ok((rows, true, host)); // dropping the child kills journalctl
            }
            extra -= 1;
            rows.push(raw(false));
            continue;
        }
        if matched || want_ctx {
            rows.push(raw(matched));
        }
        if matched {
            matches += 1;
            if matches >= limit {
                hit_limit = true;
                if !want_ctx {
                    return Ok((rows, true, host));
                }
            }
        }
    }

    let status = child.wait().await.map_err(io)?;
    if !status.success() {
        let mut stderr = String::new();
        if let Some(mut s) = child.stderr.take() {
            let _ = s.read_to_string(&mut stderr).await;
        }
        return Err(err(
            SourceErrorKind::SourceUnreachable,
            format!(
                "journalctl {status}: {}",
                stderr.trim().chars().take(200).collect::<String>()
            ),
        ));
    }
    Ok((rows, hit_limit, host))
}
