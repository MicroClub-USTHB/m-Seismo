mod common;

use common::{Backend, logs};
use seismo_coordinator::{backend, config, registry::Registry};
use seismo_proto::{
    AgentSource, AgentStatus, QueryResult, Register, agent_message::Message,
    coordinator_event::Event, coordinator_message,
};
use std::sync::Arc;
use tokio::{net::TcpListener, sync::mpsc, task::JoinHandle};
use tonic::{Code, Status};

struct Harness {
    backend: Backend,
    registry: Arc<Registry>,
    attached: JoinHandle<Result<(), Status>>,
}

async fn start() -> Harness {
    let backend = Backend::start().await;
    let registry = Arc::new(Registry::new(
        "coord-1".into(),
        config::Query {
            margin_ms: 100,
            ..Default::default()
        },
    ));
    let cfg = config::Backend {
        url: backend.url.clone(),
        key_env: String::new(),
        key: "mco_testkey".into(),
    };
    let attached = {
        let registry = registry.clone();
        tokio::spawn(async move { backend::attach(&cfg, "coord-1", registry).await })
    };
    Harness {
        backend,
        registry,
        attached,
    }
}

#[tokio::test]
async fn says_hello_first_with_the_key_then_answers_queries() {
    let mut h = start().await;

    let Event::Hello(hello) = h.backend.next().await else {
        panic!("hello must come first");
    };
    assert_eq!(hello.protocol_version, 1);
    assert_eq!(hello.coordinator_id, "coord-1");
    assert_eq!(
        h.backend.auth.lock().unwrap().as_deref(),
        Some("Bearer mco_testkey")
    );

    // no agent connected is still an answer
    h.backend.ask(logs("q1")).await;
    let Event::Result(r) = h.backend.next().await else {
        panic!("expected an aggregate result");
    };
    assert_eq!(r.query_id, "q1");
    assert!(r.agents.is_empty());
}

#[tokio::test]
async fn coordinator_faults_come_back_as_aggregate_errors() {
    let mut h = start().await;
    h.backend.next().await;
    let mut q = logs("bad");
    q.window = None;
    h.backend.ask(q).await;

    let Event::Error(e) = h.backend.next().await else {
        panic!("expected an aggregate error");
    };
    assert_eq!(e.query_id, "bad");
    assert!(e.error.is_some());
}

#[tokio::test]
async fn queries_run_side_by_side() {
    let mut h = start().await;
    h.backend.next().await;
    let (tx, mut agent_rx) = mpsc::channel(8);
    let conn = h
        .registry
        .register(
            Register {
                protocol_version: 1,
                agent_id: "db".into(),
                source: AgentSource::Loki as i32,
                services: vec![],
            },
            tx,
        )
        .unwrap();

    // two in flight before the agent answers either, and it answers backwards
    h.backend.ask(logs("one")).await;
    h.backend.ask(logs("two")).await;
    let mut asked = Vec::new();
    for _ in 0..2 {
        let m = agent_rx.recv().await.unwrap().unwrap().message.unwrap();
        let coordinator_message::Message::Query(q) = m else {
            panic!("expected a query");
        };
        asked.push(q.query_id);
    }
    for id in asked.into_iter().rev() {
        h.registry.deliver(
            &conn,
            Message::Result(QueryResult {
                query_id: id,
                ..Default::default()
            }),
        );
    }

    let mut got = Vec::new();
    for _ in 0..2 {
        let Event::Result(r) = h.backend.next().await else {
            panic!("expected an aggregate result");
        };
        assert_eq!(r.agents[0].status, AgentStatus::Ok as i32);
        got.push(r.query_id);
    }
    got.sort();
    assert_eq!(got, ["one", "two"]);
}

#[tokio::test]
async fn a_rejection_status_ends_the_attachment() {
    let mut h = start().await;
    h.backend.next().await;
    h.backend
        .to_coordinator
        .send(Err(Status::unauthenticated("bad key")))
        .await
        .unwrap();

    let e = h.attached.await.unwrap().unwrap_err();
    assert_eq!(e.code(), Code::Unauthenticated);
}

#[tokio::test]
async fn a_closed_stream_is_a_clean_end() {
    let mut h = start().await;
    h.backend.next().await;
    drop(h.backend.to_coordinator);
    assert!(h.attached.await.unwrap().is_ok());
}

#[tokio::test]
async fn an_unreachable_backend_is_an_error() {
    let dead = {
        let l = TcpListener::bind("127.0.0.1:0").await.unwrap();
        format!("http://{}", l.local_addr().unwrap())
    };
    let cfg = config::Backend {
        url: dead,
        key_env: String::new(),
        key: "mco_x".into(),
    };
    let registry = Arc::new(Registry::new("c".into(), config::Query::default()));
    let e = backend::attach(&cfg, "c", registry).await.unwrap_err();
    assert_eq!(e.code(), Code::Unavailable);
}
