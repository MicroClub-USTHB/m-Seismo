use chrono::{DateTime, SecondsFormat};
use prost_types::Timestamp;
use seismo_agent::{
    config,
    source::{Fetched, Params, docker::Docker},
};
use seismo_proto::{LogStream, MatchKind, SourceError, SourceErrorKind};
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

fn line(secs: i64, msg: &str) -> String {
    format!("{} {msg}", at(secs))
}

/// Fake docker: records its args, prints `out` on stdout and `errs` on
/// stderr like `docker logs` does, then exits with `code`.
fn fake(dir: &Path, out: &[String], errs: &[String], code: i32, extra: &str) -> config::Docker {
    fs::write(dir.join("out"), out.join("\n") + "\n").unwrap();
    fs::write(dir.join("err"), errs.join("\n") + "\n").unwrap();
    let script = format!(
        "printf '%s\\n' \"$@\" > {d}/args\n{extra}\ncat {d}/out\ncat {d}/err >&2\nexit {code}\n",
        d = dir.display()
    );
    fs::write(dir.join("fake.sh"), script).unwrap();
    config::Docker {
        containers: HashMap::new(),
        command: vec!["sh".into(), dir.join("fake.sh").display().to_string()],
    }
}

async fn fetch(
    cfg: &config::Docker,
    from: i64,
    to: i64,
    trace: Option<&str>,
    limit: u32,
) -> Result<Fetched, SourceError> {
    Docker::new(cfg)
        .fetch(Params {
            service: "app",
            from: &ts(from),
            to: &ts(to),
            trace_id: trace,
            limit,
            context: 0,
            timeout: Duration::from_secs(5),
        })
        .await
}

fn msgs(f: &Fetched) -> Vec<&str> {
    f.entries.iter().map(|e| e.message.as_str()).collect()
}

#[tokio::test]
async fn asks_docker_for_the_container_and_window() {
    let dir = tempfile::tempdir().unwrap();
    let mut cfg = fake(dir.path(), &[], &[], 0, "");
    fetch(&cfg, 0, 60, None, 10).await.unwrap();
    let args = fs::read_to_string(dir.path().join("args")).unwrap();
    let args: Vec<_> = args.lines().collect();
    assert_eq!(args[0], "logs");
    assert!(args.contains(&"--timestamps"));
    assert_eq!(
        args.last(),
        Some(&"app"),
        "defaults to the service name: {args:?}"
    );
    let since = args.iter().position(|a| *a == "--since").unwrap();
    assert_eq!(args[since + 1], "2026-10-14T17:46:40.000000000Z");

    cfg.containers.insert("app".into(), "proj-app-1".into());
    fetch(&cfg, 0, 60, None, 10).await.unwrap();
    let args = fs::read_to_string(dir.path().join("args")).unwrap();
    assert_eq!(args.lines().last(), Some("proj-app-1"));
}

#[tokio::test]
async fn interleaves_stdout_and_stderr_by_time() {
    let dir = tempfile::tempdir().unwrap();
    let out = [line(1, "out one"), line(3, "out three")];
    let errs = [line(2, "err two")];
    let got = fetch(&fake(dir.path(), &out, &errs, 0, ""), 0, 10, None, 100)
        .await
        .unwrap();

    assert_eq!(msgs(&got), ["out one", "err two", "out three"]);
    assert_eq!(got.entries[0].stream, LogStream::Stdout as i32);
    assert_eq!(got.entries[1].stream, LogStream::Stderr as i32);
    assert_eq!(got.entries[0].timestamp.unwrap().nanos, 123_456_789);
    assert_eq!(got.entries[0].match_kind, MatchKind::Window as i32);
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn cuts_to_the_exact_window() {
    let dir = tempfile::tempdir().unwrap();
    let out = [line(0, "before"), line(5, "in"), line(20, "after")];
    // nanos are 123456789, so second 5 is inside [5, 10]
    let got = fetch(&fake(dir.path(), &out, &[], 0, ""), 5, 10, None, 100)
        .await
        .unwrap();
    assert_eq!(msgs(&got), ["in"]);
}

#[tokio::test]
async fn limit_keeps_the_newest_lines_across_both_streams() {
    let dir = tempfile::tempdir().unwrap();
    let out: Vec<_> = (0..10)
        .step_by(2)
        .map(|i| line(i, &format!("o{i}")))
        .collect();
    let errs: Vec<_> = (1..10)
        .step_by(2)
        .map(|i| line(i, &format!("e{i}")))
        .collect();
    let cfg = fake(dir.path(), &out, &errs, 0, "");

    let got = fetch(&cfg, 0, 100, None, 3).await.unwrap();
    assert_eq!(msgs(&got), ["e7", "o8", "e9"]);
    assert!(got.hit_limit);

    let got = fetch(&cfg, 0, 100, None, 10).await.unwrap();
    assert_eq!(got.entries.len(), 10);
    assert!(!got.hit_limit, "exactly limit lines is not a truncation");
}

#[tokio::test]
async fn trace_filter() {
    let dir = tempfile::tempdir().unwrap();
    let out = [
        line(1, "login password=hunter2 trace_id=abc"),
        line(2, "unrelated"),
    ];
    let got = fetch(&fake(dir.path(), &out, &[], 0, ""), 0, 10, Some("abc"), 10)
        .await
        .unwrap();
    assert_eq!(msgs(&got), ["login password=hunter2 trace_id=abc"]);
    assert_eq!(got.entries[0].match_kind, MatchKind::Trace as i32);
}

#[tokio::test]
async fn docker_failures_map_to_source_errors() {
    let dir = tempfile::tempdir().unwrap();
    for (stderr, want) in [
        (
            "Error response from daemon: No such container: app",
            SourceErrorKind::WorkloadNotFound,
        ),
        (
            "Error response from daemon: configured logging driver does not support reading",
            SourceErrorKind::DriverUnsupported,
        ),
        (
            "permission denied while trying to connect to the Docker daemon socket",
            SourceErrorKind::AccessDenied,
        ),
        (
            "Cannot connect to the Docker daemon at unix:///var/run/docker.sock",
            SourceErrorKind::SourceUnreachable,
        ),
    ] {
        let cfg = fake(dir.path(), &[], &[stderr.into()], 1, "");
        let e = fetch(&cfg, 0, 10, None, 10).await.unwrap_err();
        assert_eq!(e.kind, want as i32, "{stderr}: {}", e.detail);
    }

    let missing = config::Docker {
        containers: HashMap::new(),
        command: vec!["definitely-not-docker".into()],
    };
    let e = fetch(&missing, 0, 10, None, 10).await.unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::DriverUnsupported as i32);

    let slow = fake(dir.path(), &[], &[], 0, "sleep 5");
    let e = Docker::new(&slow)
        .fetch(Params {
            service: "app",
            from: &ts(0),
            to: &ts(10),
            trace_id: None,
            limit: 10,
            context: 0,
            timeout: Duration::from_millis(200),
        })
        .await
        .unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::Timeout as i32);
}

// -- context_lines --

const TRACE: i32 = MatchKind::Trace as i32;
const CTX: i32 = MatchKind::Context as i32;

async fn fetch_ctx(cfg: &config::Docker, to: i64, limit: u32, context: u32) -> Fetched {
    Docker::new(cfg)
        .fetch(Params {
            service: "app",
            from: &ts(0),
            to: &ts(to),
            trace_id: Some("abc"),
            limit,
            context,
            timeout: Duration::from_secs(5),
        })
        .await
        .unwrap()
}

/// n0..n9, evens on stdout and odds on stderr, `traced` ones carry the id.
fn split_numbered(traced: &[i64]) -> (Vec<String>, Vec<String>) {
    let (mut out, mut errs) = (Vec::new(), Vec::new());
    for i in 0..10 {
        let tag = if traced.contains(&i) {
            " trace_id=abc"
        } else {
            ""
        };
        let l = line(i, &format!("n{i}{tag}"));
        if i % 2 == 0 {
            out.push(l)
        } else {
            errs.push(l)
        }
    }
    (out, errs)
}

fn shape(f: &Fetched) -> Vec<(String, i32)> {
    f.entries
        .iter()
        .map(|e| {
            (
                e.message.split(' ').next().unwrap().to_owned(),
                e.match_kind,
            )
        })
        .collect()
}

fn expect(items: &[(&str, i32)]) -> Vec<(String, i32)> {
    items.iter().map(|(n, k)| (n.to_string(), *k)).collect()
}

#[tokio::test]
async fn context_crosses_stdout_and_stderr() {
    let dir = tempfile::tempdir().unwrap();
    let (out, errs) = split_numbered(&[4, 8]);
    let cfg = fake(dir.path(), &out, &errs, 0, "");

    let got = fetch_ctx(&cfg, 100, 100, 1).await;
    assert_eq!(
        shape(&got),
        expect(&[
            ("n3", CTX),
            ("n4", TRACE),
            ("n5", CTX),
            ("n7", CTX),
            ("n8", TRACE),
            ("n9", CTX)
        ])
    );
    assert_eq!(got.entries[0].stream, LogStream::Stderr as i32, "n3 is odd");
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn context_follows_the_limit_and_stays_in_the_window() {
    let dir = tempfile::tempdir().unwrap();
    let (out, errs) = split_numbered(&[4, 8]);
    let cfg = fake(dir.path(), &out, &errs, 0, "");

    let got = fetch_ctx(&cfg, 100, 1, 2).await;
    assert_eq!(
        shape(&got),
        expect(&[("n6", CTX), ("n7", CTX), ("n8", TRACE), ("n9", CTX)])
    );
    assert!(got.hit_limit, "a second match was left out");

    let got = fetch_ctx(&cfg, 9, 100, 2).await; // n9 is at 9.12s
    assert_eq!(shape(&got).last(), Some(&("n8".to_owned(), TRACE)));
}
