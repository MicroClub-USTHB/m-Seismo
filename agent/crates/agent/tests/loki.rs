mod common;

use common::{serve, streams};
use prost_types::Timestamp;
use seismo_agent::{
    config,
    loki::{Loki, Params},
};
use seismo_proto::{LogStream, MatchKind, SourceErrorKind};
use serde_json::json;
use std::{collections::HashMap, time::Duration};

fn cfg(url: &str) -> config::Loki {
    config::Loki {
        url: url.into(),
        service_label: "service_name".into(),
        scope_label: Some("namespace".into()),
        tenant: None,
        trace_field: "trace_id".into(),
        auth: config::Auth::None,
    }
}

fn ts(s: i64) -> Timestamp {
    Timestamp {
        seconds: s,
        nanos: 0,
    }
}

async fn fetch(
    loki: &Loki,
    trace_id: Option<&str>,
    limit: u32,
    timeout: Duration,
) -> Result<seismo_agent::loki::Fetched, seismo_proto::SourceError> {
    loki.fetch(Params {
        service: "postgres",
        from: &ts(100),
        to: &ts(200),
        trace_id,
        limit,
        timeout,
    })
    .await
}

const T: Duration = Duration::from_secs(5);
const NONE: Duration = Duration::ZERO;

fn query_params(head: &str) -> HashMap<String, String> {
    let target = head.split_whitespace().nth(1).unwrap();
    reqwest::Url::parse(&format!("http://x{target}"))
        .unwrap()
        .query_pairs()
        .into_owned()
        .collect()
}

#[tokio::test]
async fn merges_streams_oldest_first_and_redacts() {
    let body = streams(&[
        (
            json!({"service_name": "postgres", "namespace": "prod", "stream": "stderr"}),
            &[("300", "c password=hunter2"), ("100", "a")],
        ),
        (
            json!({"service_name": "postgres", "stream": "stdout"}),
            &[("200", "b")],
        ),
    ]);
    let fake = serve(NONE, move |_| (200, body.clone())).await;
    let got = fetch(&Loki::new(cfg(&fake.url)).unwrap(), None, 100, T)
        .await
        .unwrap();

    let msgs: Vec<_> = got.entries.iter().map(|e| e.message.as_str()).collect();
    assert_eq!(msgs, ["a", "b", "c password=[REDACTED]"]);
    assert_eq!(got.entries[0].stream, LogStream::Stderr as i32);
    assert_eq!(got.entries[1].stream, LogStream::Stdout as i32);
    assert_eq!(got.entries[0].match_kind, MatchKind::Window as i32);
    assert_eq!(got.entries[0].timestamp.unwrap().nanos, 100);
    assert_eq!(got.scope_label, "prod");
    assert!(!got.hit_limit);
}

#[tokio::test]
async fn sends_window_limit_and_selector() {
    let fake = serve(NONE, |_| (200, streams(&[]))).await;
    fetch(&Loki::new(cfg(&fake.url)).unwrap(), None, 50, T)
        .await
        .unwrap();

    let head = fake.requests.lock().unwrap()[0].clone();
    assert!(head.starts_with("GET /loki/api/v1/query_range?"), "{head}");
    let q = query_params(&head);
    assert_eq!(q["query"], r#"{service_name="postgres"}"#);
    assert_eq!(q["start"], "100000000000");
    assert_eq!(q["end"], "200000000000");
    assert_eq!(q["limit"], "50");
    assert_eq!(q["direction"], "backward");
}

#[tokio::test]
async fn trace_id_filters_on_metadata_or_line() {
    let fake = serve(NONE, |_| (200, streams(&[(json!({}), &[("1", "hit")])]))).await;
    let got = fetch(&Loki::new(cfg(&fake.url)).unwrap(), Some("ab\"c"), 10, T)
        .await
        .unwrap();
    assert_eq!(got.entries[0].match_kind, MatchKind::Trace as i32);

    let mut line_cfg = cfg(&fake.url);
    line_cfg.trace_field = String::new();
    fetch(&Loki::new(line_cfg).unwrap(), Some("abc"), 10, T)
        .await
        .unwrap();

    let reqs = fake.requests.lock().unwrap();
    assert_eq!(
        query_params(&reqs[0])["query"],
        r#"{service_name="postgres"} | trace_id="ab\"c""#
    );
    assert_eq!(
        query_params(&reqs[1])["query"],
        r#"{service_name="postgres"} |= "abc""#
    );
}

#[tokio::test]
async fn sends_tenant_and_credentials() {
    let fake = serve(NONE, |_| (200, streams(&[]))).await;
    let mut c = cfg(&fake.url);
    c.tenant = Some("acme".into());
    c.auth = config::Auth::Bearer {
        token_env: String::new(),
        token: "tok123".into(),
    };
    fetch(&Loki::new(c).unwrap(), None, 10, T).await.unwrap();

    let head = fake.requests.lock().unwrap()[0].to_lowercase();
    assert!(head.contains("x-scope-orgid: acme"), "{head}");
    assert!(head.contains("authorization: bearer tok123"), "{head}");
}

#[tokio::test]
async fn full_response_means_truncated() {
    let body = streams(&[(json!({}), &[("1", "a"), ("2", "b")])]);
    let fake = serve(NONE, move |_| (200, body.clone())).await;
    let loki = Loki::new(cfg(&fake.url)).unwrap();
    assert!(fetch(&loki, None, 2, T).await.unwrap().hit_limit);
    assert!(!fetch(&loki, None, 3, T).await.unwrap().hit_limit);
}

#[tokio::test]
async fn maps_failures_to_source_errors() {
    for (status, body, want) in [
        (401, "no", SourceErrorKind::AccessDenied),
        (403, "no", SourceErrorKind::AccessDenied),
        (503, "down", SourceErrorKind::SourceUnreachable),
        (400, "bad query", SourceErrorKind::InvalidResponse),
        (200, "not json", SourceErrorKind::InvalidResponse),
        (
            200,
            r#"{"status":"success","data":{"result":[{"stream":{},"values":[["x","y"]]}]}}"#,
            SourceErrorKind::InvalidResponse,
        ),
    ] {
        let fake = serve(NONE, move |_| (status, body.to_owned())).await;
        let e = fetch(&Loki::new(cfg(&fake.url)).unwrap(), None, 10, T)
            .await
            .unwrap_err();
        assert_eq!(e.kind, want as i32, "{status} {body}: {}", e.detail);
    }
}

#[tokio::test]
async fn unreachable_and_slow_sources() {
    // bind then drop so nobody listens on the port
    let dead = {
        let l = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        format!("http://{}", l.local_addr().unwrap())
    };
    let e = fetch(&Loki::new(cfg(&dead)).unwrap(), None, 10, T)
        .await
        .unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::SourceUnreachable as i32);

    let slow = serve(Duration::from_secs(2), |_| (200, streams(&[]))).await;
    let e = fetch(
        &Loki::new(cfg(&slow.url)).unwrap(),
        None,
        10,
        Duration::from_millis(100),
    )
    .await
    .unwrap_err();
    assert_eq!(e.kind, SourceErrorKind::Timeout as i32);
}
