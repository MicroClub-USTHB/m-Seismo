//! Kubernetes source against a fake `kubectl`: the script records its args,
//! prints canned logs or pod JSON, and fails like kubectl for `missing`.

use chrono::{DateTime, SecondsFormat};
use prost_types::Timestamp;
use seismo_agent::{
    config,
    source::{Fetched, Params, kubernetes::Kubernetes},
};
use seismo_proto::{SourceError, SourceErrorKind};
use std::{collections::HashMap, fs, path::Path, time::Duration};

const BASE: i64 = 1_792_000_000;

fn at(secs: i64) -> String {
    DateTime::from_timestamp(BASE + secs, 123_456_789)
        .unwrap()
        .to_rfc3339_opts(SecondsFormat::Nanos, true)
}

fn ts(secs: i64) -> Timestamp {
    Timestamp {
        seconds: BASE + secs,
        nanos: 0,
    }
}

const POD: &str = r#"{
  "spec": {"containers": [{
    "name": "app", "image": "registry.local/api:1.4",
    "resources": {"limits": {"cpu": "500m", "memory": "256Mi"}}
  }]},
  "status": {"containerStatuses": [{
    "name": "app", "restartCount": 3,
    "state": {"running": {"startedAt": "2026-10-26T10:00:00Z"}},
    "lastState": {"terminated": {"reason": "OOMKilled"}}
  }]}
}"#;

/// Fake kubectl. `logs` prints `logs.txt`, `get` prints `pod.json`.
fn fake(dir: &Path, logs: &[String], pod: &str) -> config::Kubernetes {
    fs::write(dir.join("logs.txt"), logs.join("\n") + "\n").unwrap();
    fs::write(dir.join("pod.json"), pod).unwrap();
    let script = format!(
        "printf '%s\\n' \"$@\" > {d}/args\n\
         case \"$*\" in\n\
         *missing*) echo 'Error from server (NotFound): pods \"missing\" not found' >&2; exit 1;;\n\
         logs*) cat {d}/logs.txt;;\n\
         get*) cat {d}/pod.json;;\n\
         esac\n",
        d = dir.display()
    );
    fs::write(dir.join("fake.sh"), script).unwrap();
    config::Kubernetes {
        pods: HashMap::from([
            ("app".into(), "shop/api-7f9/app".into()),
            ("gone".into(), "shop/missing/app".into()),
        ]),
        command: vec!["sh".into(), dir.join("fake.sh").display().to_string()],
    }
}

async fn fetch(cfg: &config::Kubernetes, service: &str) -> Result<Fetched, SourceError> {
    Kubernetes::new(cfg)
        .unwrap()
        .fetch(Params {
            service,
            from: &ts(100),
            to: &ts(200),
            trace_id: None,
            limit: 10,
            context: 0,
            timeout: Duration::from_secs(5),
        })
        .await
}

#[tokio::test]
async fn fetch_asks_kubectl_for_the_container_and_keeps_the_window() {
    let dir = tempfile::tempdir().unwrap();
    let logs = [
        format!("{} before the window", at(50)),
        format!("{} inside the window", at(150)),
        format!("{} after the window", at(250)),
    ];
    let cfg = fake(dir.path(), &logs, POD);

    let f = fetch(&cfg, "app").await.unwrap();

    let args = fs::read_to_string(dir.path().join("args")).unwrap();
    let args: Vec<&str> = args.lines().collect();
    assert_eq!(args[0], "logs");
    assert!(args.contains(&"--timestamps"));
    assert!(args.windows(2).any(|w| w == ["-n", "shop"]));
    assert!(args.windows(2).any(|w| w == ["-c", "app"]));
    assert!(args.contains(&"api-7f9"));
    let msgs: Vec<&str> = f.entries.iter().map(|e| e.message.as_str()).collect();
    assert_eq!(msgs, ["inside the window"]);
    assert_eq!(f.scope_label, "shop");
}

#[tokio::test]
async fn a_service_without_a_pod_fails_before_running_kubectl() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], POD);

    let err = fetch(&cfg, "unmapped").await.unwrap_err();

    assert_eq!(err.kind, SourceErrorKind::WorkloadNotFound as i32);
    assert!(!dir.path().join("args").exists());
}

#[tokio::test]
async fn a_missing_pod_comes_back_as_workload_not_found() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], POD);

    let err = fetch(&cfg, "gone").await.unwrap_err();

    assert_eq!(err.kind, SourceErrorKind::WorkloadNotFound as i32);
}

#[tokio::test]
async fn status_reads_state_limits_and_oom_from_the_pod() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], POD);

    let info = Kubernetes::new(&cfg).unwrap().status("app").await.unwrap();

    assert_eq!(info.workload.id, "shop/api-7f9/app");
    assert_eq!(info.workload.name, "api-7f9/app");
    let d = info.diagnostics;
    assert_eq!(
        d.runtime_state.unwrap().value,
        Some(seismo_proto::string_diagnostic::Value::Available(
            "running".into()
        ))
    );
    assert_eq!(
        d.restart_count.unwrap().value,
        Some(seismo_proto::uint_diagnostic::Value::Available(3))
    );
    assert_eq!(
        d.oom_killed.unwrap().value,
        Some(seismo_proto::bool_diagnostic::Value::Available(true))
    );
    assert_eq!(
        d.configured_cpu_nanocpus.unwrap().value,
        Some(seismo_proto::uint_diagnostic::Value::Available(500_000_000))
    );
}

#[tokio::test]
async fn status_of_a_missing_pod_is_workload_not_found() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], POD);

    let err = Kubernetes::new(&cfg)
        .unwrap()
        .status("gone")
        .await
        .map(|_| ())
        .unwrap_err();

    assert_eq!(err.kind, SourceErrorKind::WorkloadNotFound as i32);
}

#[tokio::test]
async fn malformed_pod_mappings_are_rejected_up_front() {
    let cfg = config::Kubernetes {
        pods: HashMap::from([("app".into(), "shop/api-7f9".into())]),
        command: vec!["kubectl".into()],
    };
    assert!(Kubernetes::new(&cfg).is_err());
}
