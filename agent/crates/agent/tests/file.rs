use chrono::{DateTime, SecondsFormat};
use prost_types::Timestamp;
use seismo_agent::{
    config,
    source::{Fetched, Params, file::File},
};
use seismo_proto::{MatchKind, SourceError, SourceErrorKind};
use std::{collections::HashMap, fs, path::Path, time::Duration};

const BASE: i64 = 1_792_000_000;

fn at(secs: i64) -> String {
    DateTime::from_timestamp(BASE + secs, 0)
        .unwrap()
        .to_rfc3339_opts(SecondsFormat::Secs, true)
}

fn ts(secs: i64) -> Timestamp {
    Timestamp {
        seconds: BASE + secs,
        nanos: 0,
    }
}

fn source(path: &Path, rotated: u32) -> File {
    File::new(&config::File {
        files: HashMap::from([("app".to_owned(), path.to_owned())]),
        rotated,
    })
}

async fn fetch(
    f: &File,
    from: i64,
    to: i64,
    trace: Option<&str>,
    limit: u32,
) -> Result<Fetched, SourceError> {
    f.fetch(Params {
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

fn write(path: &Path, lines: &[String]) {
    fs::write(path, lines.join("\n") + "\n").unwrap();
}

#[tokio::test]
async fn returns_the_window_oldest_first() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    let all: Vec<String> = (0..10).map(|i| format!("{} line {i}", at(i))).collect();
    write(&p, &all);

    let got = fetch(&source(&p, 0), 3, 5, None, 100).await.unwrap();
    assert_eq!(msgs(&got).len(), 3);
    assert!(msgs(&got)[0].ends_with("line 3"));
    assert!(msgs(&got)[2].ends_with("line 5"));
    assert_eq!(got.entries[0].timestamp.unwrap().seconds, BASE + 3);
    assert_eq!(got.entries[0].match_kind, MatchKind::Window as i32);
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn stack_traces_follow_their_header_line() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    write(
        &p,
        &[
            format!("{} first", at(1)),
            format!("{} boom", at(2)),
            "  at foo()".into(),
            "  at bar()".into(),
            format!("{} last", at(3)),
        ],
    );
    let got = fetch(&source(&p, 0), 2, 2, None, 100).await.unwrap();
    assert_eq!(
        msgs(&got),
        [&format!("{} boom", at(2)), "  at foo()", "  at bar()"]
    );

    let got = fetch(&source(&p, 0), 3, 3, None, 100).await.unwrap();
    assert_eq!(
        got.entries.len(),
        1,
        "trace of the previous entry must not leak in"
    );
}

#[tokio::test]
async fn limit_keeps_the_newest_lines() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    write(
        &p,
        &(0..10)
            .map(|i| format!("{} n{i}", at(i)))
            .collect::<Vec<_>>(),
    );

    let got = fetch(&source(&p, 0), 0, 100, None, 3).await.unwrap();
    assert!(msgs(&got)[0].ends_with("n7") && msgs(&got)[2].ends_with("n9"));
    assert!(got.hit_limit);
}

#[tokio::test]
async fn trace_id_is_a_substring_filter() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    write(
        &p,
        &[
            format!("{} a trace_id=abc123", at(1)),
            format!("{} b trace_id=zzz", at(2)),
            format!("{} c trace_id=abc123", at(3)),
        ],
    );
    let got = fetch(&source(&p, 0), 0, 10, Some("abc123"), 100)
        .await
        .unwrap();
    assert_eq!(got.entries.len(), 2);
    assert_eq!(got.entries[0].match_kind, MatchKind::Trace as i32);
}

#[tokio::test]
async fn reads_into_rotated_files_when_the_window_needs_them() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    write(
        &dir.path().join("app.log.2"),
        &[format!("{} oldest", at(1))],
    );
    write(
        &dir.path().join("app.log.1"),
        &[format!("{} old", at(2)), "  trace of old".into()],
    );
    write(&p, &[format!("{} new", at(3))]);

    let got = fetch(&source(&p, 3), 0, 10, None, 100).await.unwrap();
    let m = msgs(&got);
    assert_eq!(m.len(), 4, "{m:?}");
    assert!(m[0].ends_with("oldest") && m[1].ends_with("old") && m[3].ends_with("new"));

    let got = fetch(&source(&p, 1), 0, 10, None, 100).await.unwrap();
    assert_eq!(got.entries.len(), 3, "rotated = 1 stops at app.log.1");
    let got = fetch(&source(&p, 0), 0, 10, None, 100).await.unwrap();
    assert_eq!(got.entries.len(), 1);
}

#[tokio::test]
async fn understands_json_and_python_style_timestamps() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    let rfc = at(1);
    let naive = rfc.trim_end_matches('Z').replace('T', " ");
    write(
        &p,
        &[
            format!(r#"{{"time":"{rfc}","msg":"json string"}}"#),
            format!(r#"{{"ts":{},"msg":"json epoch"}}"#, BASE + 2),
            format!(r#"{{"t":{},"msg":"json millis"}}"#, (BASE + 3) * 1000),
            format!("{naive},250 INFO python style"),
            format!("[{rfc}] bracketed"),
        ],
    );
    let got = fetch(&source(&p, 0), 0, 10, None, 100).await.unwrap();
    assert_eq!(got.entries.len(), 5, "{:?}", msgs(&got));
}

#[tokio::test]
async fn big_file_crosses_chunk_boundaries() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    // ~1.5MB, way over the 64KiB read chunk, odd line lengths on purpose
    let all: Vec<String> = (0..20_000usize)
        .map(|i| format!("{} #{i} {}", at(i as i64 / 10), "x".repeat(i % 37)))
        .collect();
    write(&p, &all);

    let got = fetch(&source(&p, 0), 1000, 1009, None, 100_000)
        .await
        .unwrap();
    assert_eq!(got.entries.len(), 10 * 10);
    for (n, e) in got.entries.iter().enumerate() {
        assert_eq!(e.message, all[10_000 + n], "line {n}");
    }
}

#[tokio::test]
async fn keeps_lines_raw_and_reports_missing_files() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    write(&p, &[format!("{} login password=hunter2", at(1))]);
    let got = fetch(&source(&p, 0), 0, 10, None, 10).await.unwrap();
    assert!(
        got.entries[0].message.ends_with("password=hunter2"),
        "sources hand over raw lines"
    );

    let e = fetch(&source(&dir.path().join("nope.log"), 0), 0, 10, None, 10)
        .await
        .unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::WorkloadNotFound as i32);
}

// -- context_lines --

const TRACE: i32 = MatchKind::Trace as i32;
const CTX: i32 = MatchKind::Context as i32;

async fn fetch_ctx(f: &File, from: i64, to: i64, limit: u32, context: u32) -> Fetched {
    f.fetch(Params {
        service: "app",
        from: &ts(from),
        to: &ts(to),
        trace_id: Some("abc"),
        limit,
        context,
        timeout: Duration::from_secs(5),
    })
    .await
    .unwrap()
}

fn numbered(path: &Path, traced: &[i64]) {
    let lines: Vec<String> = (0..10)
        .map(|i| {
            let tag = if traced.contains(&i) {
                " trace_id=abc"
            } else {
                ""
            };
            format!("{} n{i}{tag}", at(i))
        })
        .collect();
    write(path, &lines);
}

fn shape(f: &Fetched) -> Vec<(String, i32)> {
    f.entries
        .iter()
        .map(|e| {
            let n = e.message.split(' ').nth(1).unwrap().to_owned();
            (n, e.match_kind)
        })
        .collect()
}

fn expect(items: &[(&str, i32)]) -> Vec<(String, i32)> {
    items.iter().map(|(n, k)| (n.to_string(), *k)).collect()
}

#[tokio::test]
async fn context_surrounds_each_trace_match() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    numbered(&p, &[4, 8]);

    let got = fetch_ctx(&source(&p, 0), 0, 100, 100, 1).await;
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
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn overlapping_context_is_not_duplicated() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    numbered(&p, &[4, 6]);

    let got = fetch_ctx(&source(&p, 0), 0, 100, 100, 1).await;
    assert_eq!(
        shape(&got),
        expect(&[
            ("n3", CTX),
            ("n4", TRACE),
            ("n5", CTX),
            ("n6", TRACE),
            ("n7", CTX)
        ])
    );
}

#[tokio::test]
async fn context_follows_the_limit_and_stays_in_the_window() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    numbered(&p, &[4, 8]);

    // limit 1 keeps only the newest match and its neighbours
    let got = fetch_ctx(&source(&p, 0), 0, 100, 1, 2).await;
    assert_eq!(
        shape(&got),
        expect(&[("n6", CTX), ("n7", CTX), ("n8", TRACE), ("n9", CTX)])
    );
    assert!(got.hit_limit);

    // n9 is outside the window, so it cannot be context either
    let got = fetch_ctx(&source(&p, 0), 0, 8, 100, 2).await;
    assert_eq!(shape(&got).last(), Some(&("n8".to_owned(), TRACE)));
}

#[tokio::test]
async fn context_is_ignored_without_a_trace_id() {
    let dir = tempfile::tempdir().unwrap();
    let p = dir.path().join("app.log");
    numbered(&p, &[4]);
    let got = source(&p, 0)
        .fetch(Params {
            service: "app",
            from: &ts(0),
            to: &ts(100),
            trace_id: None,
            limit: 100,
            context: 3,
            timeout: Duration::from_secs(5),
        })
        .await
        .unwrap();
    assert_eq!(got.entries.len(), 10);
    assert!(
        got.entries
            .iter()
            .all(|e| e.match_kind == MatchKind::Window as i32)
    );
}
