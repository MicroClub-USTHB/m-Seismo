use super::{
    Fetched, Params,
    cli::{Filter, collect},
    err, finish, ts_to_ns,
};
use crate::config;
use chrono::DateTime;
use seismo_proto::{SourceError, SourceErrorKind};
use std::collections::HashMap;
use tokio::process::Command;

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
        cmd.args(&self.command[1..]).args([
            "logs",
            "--timestamps",
            "--since",
            &stamp(p.from),
            "--until",
            &stamp(p.to),
            container,
        ]);

        let filter = Filter {
            from: ts_to_ns(p.from),
            to: ts_to_ns(p.to),
            trace: p.trace_id,
            limit: p.limit as usize,
            context: p.context as usize,
        };
        let (mut rows, hit_limit) =
            tokio::time::timeout(p.timeout, collect(cmd, filter, "docker", failure_kind))
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

/// Maps `docker logs` complaints to the proto's error kinds.
fn failure_kind(stderr: &str) -> SourceErrorKind {
    let s = stderr.to_lowercase();
    if s.contains("no such container") {
        SourceErrorKind::WorkloadNotFound
    } else if s.contains("does not support reading") {
        SourceErrorKind::DriverUnsupported
    } else if s.contains("permission denied") {
        SourceErrorKind::AccessDenied
    } else {
        // includes "Cannot connect to the Docker daemon"
        SourceErrorKind::SourceUnreachable
    }
}
