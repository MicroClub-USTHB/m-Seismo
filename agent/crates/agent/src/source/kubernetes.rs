//! Reads pod logs with `kubectl logs` and container state with
//! `kubectl get pod -o json`. A workload is one container of one pod.

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

pub struct Kubernetes {
    command: Vec<String>,
    pods: HashMap<String, Target>,
}

/// Where a service runs, parsed from `namespace/pod/container`.
#[derive(Clone)]
struct Target {
    namespace: String,
    pod: String,
    container: String,
}

impl Target {
    fn parse(value: &str) -> Result<Self, String> {
        let parts: Vec<&str> = value.split('/').collect();
        let &[namespace, pod, container] = parts.as_slice() else {
            return Err(format!("{value} is not namespace/pod/container"));
        };
        if parts.iter().any(|part| part.is_empty()) {
            return Err(format!("{value} has an empty part"));
        }
        Ok(Self {
            namespace: namespace.into(),
            pod: pod.into(),
            container: container.into(),
        })
    }

    fn workload(&self) -> Workload {
        Workload {
            id: format!("{}/{}/{}", self.namespace, self.pod, self.container),
            name: format!("{}/{}", self.pod, self.container),
            scope_label: self.namespace.clone(),
        }
    }
}

impl Kubernetes {
    pub fn new(cfg: &config::Kubernetes) -> Result<Self, String> {
        let pods = cfg
            .pods
            .iter()
            .map(|(service, value)| Ok((service.clone(), Target::parse(value)?)))
            .collect::<Result<_, String>>()?;
        Ok(Self {
            command: cfg.command.clone(),
            pods,
        })
    }

    fn command(&self) -> Command {
        let mut cmd = Command::new(&self.command[0]);
        cmd.args(&self.command[1..]);
        cmd
    }

    fn target(&self, service: &str) -> Result<&Target, SourceError> {
        self.pods.get(service).ok_or_else(|| {
            err(
                SourceErrorKind::WorkloadNotFound,
                format!("no pod is configured for service {service}"),
            )
        })
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        let target = self.target(p.service)?;
        let stamp = DateTime::from_timestamp(p.from.seconds, p.from.nanos as u32)
            .map(|d| d.format("%Y-%m-%dT%H:%M:%S%.9fZ").to_string())
            .unwrap_or_default();
        let mut cmd = self.command();
        cmd.args([
            "logs",
            "--timestamps",
            "--since-time",
            &stamp,
            "-n",
            &target.namespace,
            &target.pod,
            "-c",
            &target.container,
        ]);

        let filter = Filter {
            from: ts_to_ns(p.from),
            to: ts_to_ns(p.to),
            trace: p.trace_id,
            limit: p.limit as usize,
            context: p.context as usize,
        };
        // kubectl has no upper bound, the window filter in the reader drops the rest
        let (mut rows, hit_limit) =
            tokio::time::timeout(p.timeout, collect(cmd, filter, "kubectl", failure_kind))
                .await
                .map_err(|_| err(SourceErrorKind::Timeout, "kubectl logs timed out"))??;

        rows.sort_by_key(|r| r.ts);
        let entries = finish(rows, p.trace_id.is_some(), filter.context, filter.limit);
        Ok(Fetched {
            entries,
            scope_label: target.namespace.clone(),
            hit_limit,
        })
    }

    pub async fn status(&self, service: &str) -> Result<StatusInfo, SourceError> {
        let target = self.target(service)?;
        let mut cmd = self.command();
        cmd.args([
            "get",
            "pod",
            &target.pod,
            "-n",
            &target.namespace,
            "-o",
            "json",
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);
        let out = cmd.output().await.map_err(|e| spawn_error("kubectl", e))?;
        if !out.status.success() {
            let stderr = String::from_utf8_lossy(&out.stderr);
            return Err(err(
                failure_kind(&stderr),
                format!(
                    "kubectl get failed: {}",
                    stderr.trim().chars().take(200).collect::<String>()
                ),
            ));
        }

        let pod: Value = serde_json::from_slice(&out.stdout).map_err(|e| {
            err(
                SourceErrorKind::InvalidResponse,
                format!("kubectl get printed no JSON: {e}"),
            )
        })?;
        Ok(StatusInfo {
            workload: target.workload(),
            diagnostics: diagnostics(&pod, &target.container)?,
        })
    }
}

/// Maps `kubectl` complaints to the proto's error kinds.
fn failure_kind(stderr: &str) -> SourceErrorKind {
    let s = stderr.to_lowercase();
    if s.contains("not found") || s.contains("is not valid for pod") {
        SourceErrorKind::WorkloadNotFound
    } else if s.contains("forbidden") {
        SourceErrorKind::AccessDenied
    } else {
        SourceErrorKind::SourceUnreachable
    }
}

/// Reads the fields `kubectl get pod -o json` prints for one container.
/// Limits come from the spec, the rest from the status.
fn diagnostics(pod: &Value, container: &str) -> Result<WorkloadDiagnostics, SourceError> {
    let spec = find(&pod["spec"]["containers"], container).ok_or_else(|| {
        err(
            SourceErrorKind::WorkloadNotFound,
            "the container is not in the pod spec",
        )
    })?;
    let status = find(&pod["status"]["containerStatuses"], container);
    let limits = &spec["resources"]["limits"];

    Ok(WorkloadDiagnostics {
        runtime_state: Some(str_diag(status.and_then(|s| runtime_state(&s["state"])))),
        restart_count: Some(uint_diag(status.and_then(|s| s["restartCount"].as_u64()))),
        oom_killed: Some(bool_diag(status.map(oom_killed))),
        image: Some(str_diag(spec["image"].as_str().map(str::to_owned))),
        // configured limits, not usage, same as docker
        configured_cpu_nanocpus: Some(uint_diag(
            quantity(limits, "cpu").and_then(|cores| whole(cores * 1e9)),
        )),
        configured_memory_bytes: Some(uint_diag(quantity(limits, "memory").and_then(whole))),
    })
}

fn find<'a>(list: &'a Value, container: &str) -> Option<&'a Value> {
    list.as_array()?
        .iter()
        .find(|item| item["name"].as_str() == Some(container))
}

fn runtime_state(state: &Value) -> Option<String> {
    let name = if state["running"].is_object() {
        "running"
    } else if state["waiting"].is_object() {
        "waiting"
    } else if state["terminated"].is_object() {
        "terminated"
    } else {
        return None;
    };
    Some(name.into())
}

/// True when the current or the previous run ended with an OOM kill.
fn oom_killed(status: &Value) -> bool {
    [
        &status["state"]["terminated"],
        &status["lastState"]["terminated"],
    ]
    .iter()
    .any(|terminated| terminated["reason"].as_str() == Some("OOMKilled"))
}

fn quantity(limits: &Value, resource: &str) -> Option<f64> {
    parse_quantity(limits[resource].as_str()?)
}

/// Parses the decimal and binary suffixes Kubernetes uses for cpu and memory.
/// Exponent notation is not handled, it comes back as unavailable.
fn parse_quantity(raw: &str) -> Option<f64> {
    let split = raw
        .find(|character: char| !(character.is_ascii_digit() || character == '.'))
        .unwrap_or(raw.len());
    let (number, suffix) = raw.split_at(split);
    let number: f64 = number.parse().ok()?;
    let factor = match suffix {
        "" => 1.0,
        "m" => 1e-3,
        "k" => 1e3,
        "M" => 1e6,
        "G" => 1e9,
        "T" => 1e12,
        "P" => 1e15,
        "E" => 1e18,
        "Ki" => 1024.0,
        "Mi" => 1024f64.powi(2),
        "Gi" => 1024f64.powi(3),
        "Ti" => 1024f64.powi(4),
        "Pi" => 1024f64.powi(5),
        "Ei" => 1024f64.powi(6),
        _ => return None,
    };
    Some(number * factor)
}

fn whole(value: f64) -> Option<u64> {
    (value.is_finite() && value >= 0.0).then(|| value.round() as u64)
}

#[cfg(test)]
mod tests {
    use super::*;
    use seismo_proto::{StringDiagnostic, string_diagnostic};
    use serde_json::json;

    #[test]
    fn parses_cpu_and_memory_quantities() {
        assert_eq!(parse_quantity("500m"), Some(0.5));
        assert_eq!(parse_quantity("2"), Some(2.0));
        assert_eq!(parse_quantity("256Mi"), Some(268_435_456.0));
        assert_eq!(parse_quantity("1G"), Some(1e9));
        assert_eq!(parse_quantity("1e3"), None);
        assert_eq!(parse_quantity("lots"), None);
    }

    #[test]
    fn targets_need_namespace_pod_and_container() {
        let t = Target::parse("shop/api-7f9/app").unwrap();
        assert_eq!(t.workload().id, "shop/api-7f9/app");
        assert_eq!(t.workload().name, "api-7f9/app");
        assert_eq!(t.workload().scope_label, "shop");
        assert!(Target::parse("shop/api-7f9").is_err());
        assert!(Target::parse("shop//app").is_err());
    }

    #[test]
    fn reads_state_restarts_oom_and_limits_for_the_container() {
        let pod = json!({
            "spec": {"containers": [{
                "name": "app", "image": "registry.local/api:1.4",
                "resources": {"limits": {"cpu": "500m", "memory": "256Mi"}}
            }]},
            "status": {"containerStatuses": [{
                "name": "app",
                "restartCount": 3,
                "state": {"running": {"startedAt": "2026-10-26T10:00:00Z"}},
                "lastState": {"terminated": {"reason": "OOMKilled"}}
            }]}
        });

        let d = diagnostics(&pod, "app").unwrap();

        assert_eq!(
            d.runtime_state.unwrap().value,
            Some(string_diagnostic::Value::Available("running".into()))
        );
        assert_eq!(
            d.oom_killed.unwrap().value,
            Some(seismo_proto::bool_diagnostic::Value::Available(true))
        );
        assert_eq!(
            d.configured_cpu_nanocpus.unwrap().value,
            Some(seismo_proto::uint_diagnostic::Value::Available(500_000_000))
        );
        assert_eq!(
            d.configured_memory_bytes.unwrap().value,
            Some(seismo_proto::uint_diagnostic::Value::Available(268_435_456))
        );
        let _: StringDiagnostic = d.image.unwrap();
    }

    #[test]
    fn a_container_missing_from_the_spec_is_not_found() {
        let pod = json!({"spec": {"containers": [{"name": "app"}]}});
        let err = diagnostics(&pod, "sidecar").unwrap_err();
        assert_eq!(err.kind, SourceErrorKind::WorkloadNotFound as i32);
    }

    #[test]
    fn classifies_kubectl_complaints() {
        assert_eq!(
            failure_kind("Error from server (NotFound): pods \"x\" not found"),
            SourceErrorKind::WorkloadNotFound
        );
        assert_eq!(
            failure_kind("pods \"x\" is forbidden: User cannot get pods"),
            SourceErrorKind::AccessDenied
        );
        assert_eq!(
            failure_kind("The connection to the server was refused"),
            SourceErrorKind::SourceUnreachable
        );
    }
}
