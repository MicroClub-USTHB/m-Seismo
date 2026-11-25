//! Container status through the coordinator: fan-out to agents, and the
//! backend hop that asks for it.

mod common;

use common::Backend;
use prost_types::Timestamp;
use seismo_coordinator::{backend, config, registry::Registry};
use seismo_proto::{
    AgentSource, AgentStatus, CoordinatorMessage, Register, ServiceStatus, StatusQuery,
    StatusResult, WorkloadStatus, agent_message::Message, coordinator_event::Event,
    coordinator_message,
};
use std::{
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::sync::mpsc;
use tonic::Status;

fn registry() -> Arc<Registry> {
    Arc::new(Registry::new(
        "coord".into(),
        config::Query {
            margin_ms: 100,
            ..Default::default()
        },
    ))
}

/// Deadline `secs` from now.
fn status_query(id: &str, secs: u64) -> StatusQuery {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + Duration::from_secs(secs);
    StatusQuery {
        query_id: id.into(),
        deadline: Some(Timestamp {
            seconds: t.as_secs() as i64,
            nanos: 0,
        }),
    }
}

/// A scripted agent: registered for real, but we play the agent end.
struct Fake {
    conn: seismo_coordinator::registry::Conn,
    rx: mpsc::Receiver<Result<CoordinatorMessage, Status>>,
}

fn join(reg: &Registry, id: &str) -> Fake {
    let (tx, rx) = mpsc::channel(8);
    let conn = reg
        .register(
            Register {
                protocol_version: 1,
                agent_id: id.into(),
                source: AgentSource::Docker as i32,
                services: vec!["app".into()],
            },
            tx,
        )
        .unwrap();
    Fake { conn, rx }
}

impl Fake {
    async fn status_query(&mut self) -> StatusQuery {
        let m = tokio::time::timeout(Duration::from_secs(5), self.rx.recv())
            .await
            .expect("timed out waiting for the coordinator")
            .expect("coordinator hung up")
            .unwrap();
        match m.message.unwrap() {
            coordinator_message::Message::StatusQuery(q) => q,
            other => panic!("expected a status query, got {other:?}"),
        }
    }

    fn answer(&self, reg: &Registry, query_id: &str) {
        reg.deliver(
            &self.conn,
            Message::StatusResult(StatusResult {
                query_id: query_id.into(),
                services: vec![ServiceStatus {
                    service: "app".into(),
                    status: WorkloadStatus::Complete as i32,
                    workload: None,
                    diagnostics: None,
                    error: None,
                }],
            }),
        );
    }
}

#[tokio::test]
async fn status_fans_out_and_keeps_one_section_per_agent() {
    let reg = registry();
    let (mut a, mut b) = (join(&reg, "a"), join(&reg, "b"));

    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_status(status_query("s1", 10)).await })
    };
    let (qa, qb) = (a.status_query().await, b.status_query().await);
    a.answer(&reg, &qa.query_id);
    b.answer(&reg, &qb.query_id);

    let r = query.await.unwrap().unwrap();
    assert_eq!(r.query_id, "s1");
    let ids: Vec<&str> = r.agents.iter().map(|s| s.agent_id.as_str()).collect();
    assert_eq!(ids, ["a", "b"]);
    for section in &r.agents {
        assert_eq!(section.status, AgentStatus::Ok as i32);
        assert_eq!(section.services.len(), 1);
        assert_eq!(section.services[0].service, "app");
    }
}

#[tokio::test]
async fn silent_agent_times_out_without_failing_the_aggregate() {
    let reg = registry();
    let (mut quiet, mut chatty) = (join(&reg, "quiet"), join(&reg, "chatty"));

    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_status(status_query("s2", 1)).await })
    };
    let (_, qc) = (quiet.status_query().await, chatty.status_query().await);
    chatty.answer(&reg, &qc.query_id);

    let r = query.await.unwrap().unwrap();
    let by_id = |id: &str| r.agents.iter().find(|s| s.agent_id == id).unwrap();
    assert_eq!(by_id("chatty").status, AgentStatus::Ok as i32);
    assert_eq!(by_id("quiet").status, AgentStatus::Timeout as i32);
    assert!(by_id("quiet").services.is_empty());
}

#[tokio::test]
async fn backend_status_request_is_answered_with_an_aggregate_status() {
    let mut backend = Backend::start().await;
    let reg = registry();
    let cfg = config::Backend {
        url: backend.url.clone(),
        key_env: String::new(),
        key: "mco_testkey".into(),
    };
    tokio::spawn({
        let reg = reg.clone();
        async move { backend::attach(&cfg, "coord", reg).await }
    });
    let Event::Hello(_) = backend.next().await else {
        panic!("hello must come first");
    };
    let mut agent = join(&reg, "db");

    backend.ask_status("s3").await;
    let q = agent.status_query().await;
    agent.answer(&reg, &q.query_id);

    let Event::Status(s) = backend.next().await else {
        panic!("expected an aggregate status");
    };
    assert_eq!(s.query_id, "s3");
    assert_eq!(s.agents.len(), 1);
    assert_eq!(s.agents[0].agent_id, "db");
    assert_eq!(s.agents[0].status, AgentStatus::Ok as i32);
}
