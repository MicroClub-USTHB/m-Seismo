//! Container status through the Docker source and the engine, with a fake
//! `docker` script in place of the daemon.

use prost_types::Timestamp;
use seismo_agent::{
    config,
    engine::Engine,
    source::{Source, docker::Docker},
};
use seismo_proto::{
    BoolDiagnostic, SourceErrorKind, StatusQuery, StringDiagnostic, UintDiagnostic, WorkloadStatus,
    bool_diagnostic, string_diagnostic, uint_diagnostic,
};
use std::{
    collections::HashMap,
    fs,
    path::Path,
    time::{Duration, SystemTime},
};

const INSPECT: &str = r#"[{"Name":"/app","RestartCount":2,"State":{"Status":"running","OOMKilled":false},"Config":{"Image":"postgres:16"},"HostConfig":{"NanoCpus":500000000,"Memory":268435456}}]"#;

/// Fake docker: `inspect` prints `out`, and a container called `missing`
/// fails the way the daemon does for an unknown object.
fn fake_docker(dir: &Path, out: &str) -> config::Docker {
    fs::write(dir.join("out"), out).unwrap();
    let script = format!(
        "case \"$*\" in\n  *missing*) echo 'Error: No such object: missing' >&2; exit 1;;\n  *) cat {d}/out;;\nesac\n",
        d = dir.display()
    );
    fs::write(dir.join("fake.sh"), script).unwrap();
    config::Docker {
        containers: HashMap::new(),
        command: vec!["sh".into(), dir.join("fake.sh").display().to_string()],
    }
}

fn in_secs(secs: u64) -> Timestamp {
    let at = SystemTime::now() + Duration::from_secs(secs);
    let since = at
        .duration_since(SystemTime::UNIX_EPOCH)
        .expect("after the epoch");
    Timestamp {
        seconds: since.as_secs() as i64,
        nanos: since.subsec_nanos() as i32,
    }
}

fn text(d: Option<StringDiagnostic>) -> Option<String> {
    match d?.value? {
        string_diagnostic::Value::Available(v) => Some(v),
        string_diagnostic::Value::UnavailableReason(_) => None,
    }
}

fn number(d: Option<UintDiagnostic>) -> Option<u64> {
    match d?.value? {
        uint_diagnostic::Value::Available(v) => Some(v),
        uint_diagnostic::Value::UnavailableReason(_) => None,
    }
}

fn flag(d: Option<BoolDiagnostic>) -> Option<bool> {
    match d?.value? {
        bool_diagnostic::Value::Available(v) => Some(v),
        bool_diagnostic::Value::UnavailableReason(_) => None,
    }
}

#[tokio::test]
async fn reads_state_restarts_and_configured_limits_from_inspect() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake_docker(dir.path(), INSPECT);

    let info = Docker::new(&cfg).status("app").await.unwrap();

    assert_eq!(info.workload.id, "app");
    assert_eq!(info.workload.name, "app");
    let d = info.diagnostics;
    assert_eq!(text(d.runtime_state).as_deref(), Some("running"));
    assert_eq!(number(d.restart_count), Some(2));
    assert_eq!(flag(d.oom_killed), Some(false));
    assert_eq!(text(d.image).as_deref(), Some("postgres:16"));
    assert_eq!(number(d.configured_cpu_nanocpus), Some(500_000_000));
    assert_eq!(number(d.configured_memory_bytes), Some(268_435_456));
}

#[tokio::test]
async fn engine_keeps_good_services_and_marks_missing_ones_unavailable() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake_docker(dir.path(), INSPECT);
    let source = Source::build(&config::Source::Docker(cfg)).unwrap();
    let engine = Engine::new(vec!["app".into(), "missing".into()], source);
    let query = StatusQuery {
        query_id: "status-1".into(),
        deadline: Some(in_secs(5)),
    };

    let result = engine.status(&query).await.unwrap();

    assert_eq!(result.query_id, "status-1");
    let [app, missing] = result.services.as_slice() else {
        panic!("one entry per service, got {:?}", result.services);
    };
    assert_eq!(app.service, "app");
    assert_eq!(app.status, WorkloadStatus::Complete as i32);
    assert!(app.error.is_none());
    assert_eq!(
        text(app.diagnostics.clone().unwrap().runtime_state).as_deref(),
        Some("running")
    );
    assert_eq!(missing.service, "missing");
    assert_eq!(missing.status, WorkloadStatus::Unavailable as i32);
    assert!(missing.diagnostics.is_none());
    assert_eq!(
        missing.error.as_ref().map(|e| e.kind),
        Some(SourceErrorKind::WorkloadNotFound as i32)
    );
}

#[tokio::test]
async fn engine_rejects_a_status_query_whose_deadline_passed() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake_docker(dir.path(), INSPECT);
    let engine = Engine::new(
        vec!["app".into()],
        Source::build(&config::Source::Docker(cfg)).unwrap(),
    );
    let query = StatusQuery {
        query_id: "status-late".into(),
        deadline: Some(Timestamp {
            seconds: 1,
            nanos: 0,
        }),
    };

    let err = engine.status(&query).await.unwrap_err();

    assert_eq!(err.query_id, "status-late");
}
