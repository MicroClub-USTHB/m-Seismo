use super::{
    Fetched, Params, StatusInfo, bool_diag,
    cli::{Filter, collect, spawn_error},
    err, finish, str_diag, ts_to_ns, uint_diag,
};
use crate::config;
use chrono::DateTime;
use seismo_proto::{SourceError, SourceErrorKind, Workload, WorkloadDiagnostics};
use serde_json::Value;
use std::{collections::HashMap, process::Stdio};
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

    fn command(&self) -> Command {
        let mut cmd = Command::new(&self.command[0]);
        cmd.args(&self.command[1..]);
        cmd
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
        let mut cmd = self.command();
        cmd.args([
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

    pub async fn status(&self, service: &str) -> Result<StatusInfo, SourceError> {
        let target = self.containers.get(service).map_or(service, String::as_str);
        let mut cmd = self.command();
        cmd.args(["inspect", "--type", "container", target])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);
        let out = cmd.output().await.map_err(|e| spawn_error("docker", e))?;
        if !out.status.success() {
            return Err(inspect_failure(&String::from_utf8_lossy(&out.stderr)));
        }

        let inspected: Vec<Value> = serde_json::from_slice(&out.stdout).map_err(|e| {
            err(
                SourceErrorKind::InvalidResponse,
                format!("docker inspect printed no JSON: {e}"),
            )
        })?;
        let Some(container) = inspected.first() else {
            return Err(err(
                SourceErrorKind::WorkloadNotFound,
                "docker inspect returned nothing",
            ));
        };
        // inspect names the container with a leading slash
        let name = container["Name"]
            .as_str()
            .unwrap_or(target)
            .trim_start_matches('/');
        Ok(StatusInfo {
            workload: Workload {
                id: name.to_owned(),
                name: name.to_owned(),
                scope_label: self.host.clone(),
            },
            diagnostics: diagnostics(container),
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

/// Maps `docker inspect` complaints, which name the object instead of the container.
fn inspect_failure(stderr: &str) -> SourceError {
    let s = stderr.to_lowercase();
    let kind = if s.contains("no such object") {
        SourceErrorKind::WorkloadNotFound
    } else if s.contains("permission denied") {
        SourceErrorKind::AccessDenied
    } else {
        SourceErrorKind::SourceUnreachable
    };
    err(
        kind,
        format!(
            "docker inspect failed: {}",
            stderr.trim().chars().take(200).collect::<String>()
        ),
    )
}

/// Reads the fields `docker inspect` prints for a container. Absent or zero
/// means "not reported", and zero limits mean no limit was set.
fn diagnostics(c: &Value) -> WorkloadDiagnostics {
    let state = &c["State"];
    WorkloadDiagnostics {
        runtime_state: Some(str_diag(state["Status"].as_str().map(str::to_owned))),
        restart_count: Some(uint_diag(c["RestartCount"].as_u64())),
        oom_killed: Some(bool_diag(state["OOMKilled"].as_bool())),
        image: Some(str_diag(c["Config"]["Image"].as_str().map(str::to_owned))),
        configured_cpu_nanocpus: Some(uint_diag(
            c["HostConfig"]["NanoCpus"].as_u64().filter(|n| *n > 0),
        )),
        configured_memory_bytes: Some(uint_diag(
            c["HostConfig"]["Memory"].as_u64().filter(|m| *m > 0),
        )),
    }
}
