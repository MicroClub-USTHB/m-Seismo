use prost_types::Timestamp;
use seismo_agent::{
    config,
    source::{Fetched, Params, journal::Journal},
};
use seismo_proto::{MatchKind, SourceError, SourceErrorKind};
use serde_json::json;
use std::{collections::HashMap, fs, path::Path, time::Duration};

const BASE: i64 = 1_792_000_000;

fn ts(secs: i64) -> Timestamp {
    Timestamp {
        seconds: BASE + secs,
        nanos: 0,
    }
}

/// A journal entry as `journalctl -o json` prints it.
fn entry(secs: i64, msg: serde_json::Value) -> String {
    json!({
        "__REALTIME_TIMESTAMP": ((BASE + secs) * 1_000_000).to_string(),
        "_HOSTNAME": "box1",
        "MESSAGE": msg,
    })
    .to_string()
}

/// Fake journalctl: records its args, prints canned output (newest first
/// like `-r`), then exits with `code`. Run through `sh` so we never exec a
/// file we just wrote.
fn fake(dir: &Path, output: &[String], code: i32, extra: &str) -> config::Journal {
    fs::write(dir.join("out"), output.join("\n") + "\n").unwrap();
    let script = format!(
        "printf '%s\\n' \"$@\" > {d}/args\n{extra}\ncat {d}/out\nexit {code}\n",
        d = dir.display()
    );
    fs::write(dir.join("fake.sh"), script).unwrap();
    config::Journal {
        units: HashMap::new(),
        command: vec!["sh".into(), dir.join("fake.sh").display().to_string()],
    }
}

async fn fetch(
    cfg: &config::Journal,
    from: i64,
    to: i64,
    trace: Option<&str>,
    limit: u32,
) -> Result<Fetched, SourceError> {
    Journal::new(cfg)
        .fetch(Params {
            service: "app",
            from: &ts(from),
            to: &ts(to),
            trace_id: trace,
            limit,
            timeout: Duration::from_secs(5),
        })
        .await
}

fn msgs(f: &Fetched) -> Vec<&str> {
    f.entries.iter().map(|e| e.message.as_str()).collect()
}

#[tokio::test]
async fn asks_journalctl_for_the_unit_and_window() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], 0, "");
    fetch(&cfg, 0, 60, None, 10).await.unwrap();

    let args = fs::read_to_string(dir.path().join("args")).unwrap();
    let args: Vec<_> = args.lines().collect();
    let has = |a: &[&str]| args.windows(a.len()).any(|w| w == a);
    assert!(has(&["-u", "app.service"]), "{args:?}");
    assert!(
        has(&["-o", "json"]) && args.contains(&"-r") && args.contains(&"--utc"),
        "{args:?}"
    );
    // 1_792_000_000 is 2026-10-14 17:46:40 UTC, until gets +1s of slack
    assert!(has(&["--since", "2026-10-14 17:46:40"]), "{args:?}");
    assert!(has(&["--until", "2026-10-14 17:47:41"]), "{args:?}");
}

#[tokio::test]
async fn custom_unit_name_is_used() {
    let dir = tempfile::tempdir().unwrap();
    let mut cfg = fake(dir.path(), &[], 0, "");
    cfg.units
        .insert("app".into(), "postgresql@16-main.service".into());
    fetch(&cfg, 0, 60, None, 10).await.unwrap();
    let args = fs::read_to_string(dir.path().join("args")).unwrap();
    assert!(args.contains("postgresql@16-main.service"));
}

#[tokio::test]
async fn cuts_to_the_exact_window_and_returns_oldest_first() {
    let dir = tempfile::tempdir().unwrap();
    // newest first, with a line past `to` and one before `from` (the
    // whole-second slack lets those through)
    let out = [
        entry(31, json!("too new")),
        entry(30, json!("c")),
        entry(20, json!("b")),
        entry(10, json!("a")),
        entry(9, json!("too old")),
        entry(1, json!("never read")),
    ];
    let got = fetch(&fake(dir.path(), &out, 0, ""), 10, 30, None, 100)
        .await
        .unwrap();
    assert_eq!(msgs(&got), ["a", "b", "c"]);
    assert_eq!(got.entries[0].timestamp.unwrap().seconds, BASE + 10);
    assert_eq!(got.entries[0].match_kind, MatchKind::Window as i32);
    assert_eq!(got.scope_label, "box1");
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn limit_keeps_the_newest_lines() {
    let dir = tempfile::tempdir().unwrap();
    let out: Vec<_> = (0..10)
        .rev()
        .map(|i| entry(i, json!(format!("n{i}"))))
        .collect();
    let got = fetch(&fake(dir.path(), &out, 0, ""), 0, 100, None, 3)
        .await
        .unwrap();
    assert_eq!(msgs(&got), ["n7", "n8", "n9"]);
    assert!(got.hit_limit);
}

#[tokio::test]
async fn trace_filter_and_binary_messages() {
    let dir = tempfile::tempdir().unwrap();
    let out = [
        entry(3, json!("login password=hunter2 trace_id=abc")),
        entry(2, json!("other request")),
        // journald prints non-utf8 / binary messages as a byte array
        entry(
            1,
            json!([116, 114, 97, 99, 101, 95, 105, 100, 61, 97, 98, 99]),
        ),
    ];
    let cfg = fake(dir.path(), &out, 0, "");
    let got = fetch(&cfg, 0, 10, Some("abc"), 10).await.unwrap();
    assert_eq!(
        msgs(&got),
        ["trace_id=abc", "login password=hunter2 trace_id=abc"]
    );
    assert_eq!(got.entries[0].match_kind, MatchKind::Trace as i32);
}

#[tokio::test]
async fn failures_map_to_source_errors() {
    let dir = tempfile::tempdir().unwrap();
    let cfg = fake(dir.path(), &[], 1, "echo 'Failed to open journal' >&2");
    let e = fetch(&cfg, 0, 10, None, 10).await.unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::SourceUnreachable as i32);
    assert!(e.detail.contains("Failed to open journal"), "{}", e.detail);

    let missing = config::Journal {
        units: HashMap::new(),
        command: vec!["definitely-not-journalctl".into()],
    };
    let e = fetch(&missing, 0, 10, None, 10).await.unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::DriverUnsupported as i32);

    let slow = fake(dir.path(), &[], 0, "sleep 5");
    let e = Journal::new(&slow)
        .fetch(Params {
            service: "app",
            from: &ts(0),
            to: &ts(10),
            trace_id: None,
            limit: 10,
            timeout: Duration::from_millis(200),
        })
        .await
        .unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::Timeout as i32);
}
