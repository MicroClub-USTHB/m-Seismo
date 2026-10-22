mod common;

use common::{serve, streams};
use prost_types::Timestamp;
use seismo_agent::{config, engine::Engine, source::Source};
use seismo_proto::{
    CoverageStatus, MatchKind, Query, QueryLimits, QueryStatus, SourceErrorKind, TimeWindow,
    WorkloadStatus,
};
use serde_json::json;
use std::{
    collections::HashMap,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

fn engine(url: &str, services: &[&str]) -> Engine {
    let source = Source::build(&config::Source::Loki(config::Loki {
        url: url.into(),
        service_label: "service_name".into(),
        scope_label: Some("namespace".into()),
        tenant: None,
        trace_field: "trace_id".into(),
        auth: config::Auth::None,
    }))
    .unwrap();
    Engine::new(services.iter().map(|s| s.to_string()).collect(), source)
}

fn ts(s: i64) -> Timestamp {
    Timestamp {
        seconds: s,
        nanos: 0,
    }
}

fn in_secs(s: u64) -> Timestamp {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + Duration::from_secs(s);
    Timestamp {
        seconds: t.as_secs() as i64,
        nanos: 0,
    }
}

fn query() -> Query {
    Query {
        query_id: "q1".into(),
        window: Some(TimeWindow {
            from: Some(ts(100)),
            to: Some(ts(200)),
        }),
        trace_id: None,
        limits: Some(QueryLimits {
            max_lines_per_workload: 100,
            max_bytes_total: 1 << 20,
            context_lines: 0,
        }),
        deadline: Some(in_secs(30)),
    }
}

/// Fake loki whose answer depends on the service in the LogQL selector.
async fn loki_by_service(replies: &[(&str, u16, String)]) -> common::Fake {
    let replies: HashMap<String, (u16, String)> = replies
        .iter()
        .map(|(s, c, b)| (s.to_string(), (*c, b.clone())))
        .collect();
    serve(Duration::ZERO, move |target| {
        let url = reqwest::Url::parse(&format!("http://x{target}")).unwrap();
        let q = url.query_pairs().find(|(k, _)| k == "query").unwrap().1;
        let svc = q.split('"').nth(1).unwrap();
        replies.get(svc).cloned().unwrap_or((404, "nope".into()))
    })
    .await
}

fn lines(n: usize, text: &str) -> String {
    let vals: Vec<(String, String)> = (0..n)
        .map(|i| (format!("{}", 1 + i), text.into()))
        .collect();
    let refs: Vec<(&str, &str)> = vals.iter().map(|(a, b)| (a.as_str(), b.as_str())).collect();
    streams(&[(json!({"namespace": "prod"}), &refs)])
}

#[tokio::test]
async fn one_workload_per_service() {
    let fake = loki_by_service(&[("db", 200, lines(2, "x")), ("web", 200, lines(1, "y"))]).await;
    let r = engine(&fake.url, &["db", "web"])
        .answer(&query())
        .await
        .unwrap();

    assert_eq!(r.query_id, "q1");
    assert_eq!(r.status, QueryStatus::Complete as i32);
    assert!(!r.truncated);
    assert_eq!(r.requested_window, query().window);
    let names: Vec<_> = r
        .workloads
        .iter()
        .map(|w| w.workload.as_ref().unwrap().name.as_str())
        .collect();
    assert_eq!(names, ["db", "web"]);
    let db = &r.workloads[0];
    assert_eq!(db.entries.len(), 2);
    assert_eq!(db.workload.as_ref().unwrap().scope_label, "prod");
    assert_eq!(db.status, WorkloadStatus::Complete as i32);
    assert_eq!(db.coverage, CoverageStatus::Complete as i32);
    assert!(db.diagnostics.is_some());
}

#[tokio::test]
async fn trace_query_marks_entries() {
    let fake = loki_by_service(&[("db", 200, lines(1, "x"))]).await;
    let mut q = query();
    q.trace_id = Some("abc".into());
    let r = engine(&fake.url, &["db"]).answer(&q).await.unwrap();
    assert_eq!(
        r.workloads[0].entries[0].match_kind,
        MatchKind::Trace as i32
    );
}

#[tokio::test]
async fn one_failing_service_makes_it_partial() {
    let fake = loki_by_service(&[("db", 200, lines(1, "x")), ("web", 503, "down".into())]).await;
    let r = engine(&fake.url, &["db", "web"])
        .answer(&query())
        .await
        .unwrap();

    assert_eq!(r.status, QueryStatus::Partial as i32);
    assert!(r.source_error.is_none());
    assert_eq!(r.workloads[0].status, WorkloadStatus::Complete as i32);
    let web = &r.workloads[1];
    assert_eq!(web.status, WorkloadStatus::Unavailable as i32);
    assert_eq!(
        web.error.as_ref().unwrap().kind,
        SourceErrorKind::SourceUnreachable as i32
    );
    assert!(web.entries.is_empty());
}

#[tokio::test]
async fn everything_failing_reports_a_source_error() {
    let fake = loki_by_service(&[("db", 401, "no".into())]).await;
    let r = engine(&fake.url, &["db"]).answer(&query()).await.unwrap();
    assert_eq!(r.status, QueryStatus::Failed as i32);
    assert_eq!(
        r.source_error.unwrap().kind,
        SourceErrorKind::AccessDenied as i32
    );
}

#[tokio::test]
async fn line_limit_marks_partial() {
    let fake = loki_by_service(&[("db", 200, lines(3, "x"))]).await;
    let mut q = query();
    q.limits.as_mut().unwrap().max_lines_per_workload = 3;
    let r = engine(&fake.url, &["db"]).answer(&q).await.unwrap();

    assert_eq!(r.status, QueryStatus::Partial as i32);
    assert!(r.truncated);
    assert_eq!(r.workloads[0].coverage, CoverageStatus::Partial as i32);
}

#[tokio::test]
async fn byte_budget_keeps_newest_lines() {
    // 5 lines of 10 bytes, budget fits 3
    let body = streams(&[(
        json!({}),
        &[
            ("1", "aaaaaaaaaa"),
            ("2", "bbbbbbbbbb"),
            ("3", "cccccccccc"),
            ("4", "dddddddddd"),
            ("5", "eeeeeeeeee"),
        ],
    )]);
    let fake = loki_by_service(&[("db", 200, body)]).await;
    let mut q = query();
    q.limits.as_mut().unwrap().max_bytes_total = 30;
    let r = engine(&fake.url, &["db"]).answer(&q).await.unwrap();

    let w = &r.workloads[0];
    let msgs: Vec<_> = w.entries.iter().map(|e| e.message.as_str()).collect();
    assert_eq!(msgs, ["cccccccccc", "dddddddddd", "eeeeeeeeee"]);
    assert_eq!(w.dropped_line_count, 2);
    assert_eq!(r.dropped_line_count, 2);
    assert!(r.truncated);
}

#[tokio::test]
async fn byte_budget_is_shared_between_workloads() {
    let fake = loki_by_service(&[
        ("a", 200, lines(2, "xxxxxxxxxx")),
        ("b", 200, lines(2, "yyyyyyyyyy")),
    ])
    .await;
    let mut q = query();
    q.limits.as_mut().unwrap().max_bytes_total = 30;
    let r = engine(&fake.url, &["a", "b"]).answer(&q).await.unwrap();

    assert_eq!(r.workloads[0].entries.len(), 2);
    assert_eq!(r.workloads[1].entries.len(), 1);
    assert_eq!(r.dropped_line_count, 1);
}

#[tokio::test]
async fn rejects_bad_queries() {
    let fake = loki_by_service(&[]).await;
    let e = engine(&fake.url, &["db"]);

    let mut no_window = query();
    no_window.window = None;
    let mut backwards = query();
    backwards.window = Some(TimeWindow {
        from: Some(ts(200)),
        to: Some(ts(100)),
    });
    let mut expired = query();
    expired.deadline = Some(ts(1));

    for q in [no_window, backwards, expired] {
        let err = e.answer(&q).await.unwrap_err();
        assert_eq!(err.query_id, "q1");
        assert!(err.error.is_some());
    }
    assert!(
        fake.requests.lock().unwrap().is_empty(),
        "never reached loki"
    );
}
