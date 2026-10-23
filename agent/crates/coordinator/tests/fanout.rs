use prost_types::Timestamp;
use seismo_coordinator::{SCHEMA_VERSION, config, registry::Registry};
use seismo_proto::{
    AgentSource, AgentStatus, CoordinatorMessage, QueryError, QueryLogs, QueryResult, SourceError,
    SourceErrorKind, TimeWindow, WarningCode, agent_message::Message, coordinator_message,
};
use std::{
    sync::Arc,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tokio::sync::mpsc;
use tonic::Status;

fn registry(margin_ms: u64, total_budget_bytes: u32) -> Arc<Registry> {
    Arc::new(Registry::new(
        "coord".into(),
        config::Query {
            margin_ms,
            total_budget_bytes,
            ..Default::default()
        },
    ))
}

/// A scripted agent: registered for real, but we play both ends.
struct Fake {
    conn: seismo_coordinator::registry::Conn,
    rx: mpsc::Receiver<Result<CoordinatorMessage, Status>>,
}

fn join(reg: &Registry, id: &str, services: &[&str]) -> Fake {
    let (tx, rx) = mpsc::channel(8);
    let conn = reg
        .register(
            seismo_proto::Register {
                protocol_version: 1,
                agent_id: id.into(),
                source: AgentSource::Loki as i32,
                services: services.iter().map(|s| s.to_string()).collect(),
            },
            tx,
        )
        .unwrap();
    Fake { conn, rx }
}

impl Fake {
    async fn next(&mut self) -> coordinator_message::Message {
        let m = tokio::time::timeout(Duration::from_secs(5), self.rx.recv())
            .await
            .expect("timed out waiting for the coordinator")
            .expect("coordinator hung up")
            .unwrap();
        m.message.unwrap()
    }

    async fn query(&mut self) -> seismo_proto::Query {
        match self.next().await {
            coordinator_message::Message::Query(q) => q,
            other => panic!("expected a query, got {other:?}"),
        }
    }

    fn answer(&self, reg: &Registry, q: &seismo_proto::Query, truncated: bool) {
        reg.deliver(
            &self.conn,
            Message::Result(QueryResult {
                query_id: q.query_id.clone(),
                truncated,
                ..Default::default()
            }),
        );
    }
}

fn now_plus(d: Duration) -> Timestamp {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + d;
    Timestamp {
        seconds: t.as_secs() as i64,
        nanos: t.subsec_nanos() as i32,
    }
}

fn logs(deadline: Duration) -> QueryLogs {
    QueryLogs {
        query_id: "b1".into(),
        window: Some(TimeWindow {
            from: Some(Timestamp {
                seconds: 100,
                nanos: 0,
            }),
            to: Some(Timestamp {
                seconds: 200,
                nanos: 0,
            }),
        }),
        trace_id: None,
        deadline: Some(now_plus(deadline)),
        limits: None,
    }
}

const LONG: Duration = Duration::from_secs(10);

#[tokio::test]
async fn no_agents_is_an_empty_result_not_an_error() {
    let reg = registry(100, 8 << 20);
    let r = reg.query_logs(logs(LONG)).await.unwrap();
    assert_eq!(r.schema_version, SCHEMA_VERSION);
    assert_eq!(r.query_id, "b1");
    assert_eq!(r.requested_window, logs(LONG).window);
    assert!(r.agents.is_empty() && r.warnings.is_empty());
}

#[tokio::test]
async fn asks_every_agent_with_its_share_of_the_limits() {
    let reg = registry(500, 8 << 20);
    let (mut b, mut a) = (join(&reg, "b", &["redis"]), join(&reg, "a", &["db"]));
    let mut q = logs(LONG);
    let backend_deadline = q.deadline.unwrap();
    q.trace_id = Some("abc".into());

    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(q).await })
    };
    let (qa, qb) = (a.query().await, b.query().await);
    for (agent, q) in [(&a, &qa), (&b, &qb)] {
        agent.answer(&reg, q, false);
    }
    let r = query.await.unwrap().unwrap();

    // sorted by agent id, whatever order they joined in
    let ids: Vec<_> = r.agents.iter().map(|s| s.agent_id.as_str()).collect();
    assert_eq!(ids, ["a", "b"]);
    assert!(
        r.agents
            .iter()
            .all(|s| s.status == AgentStatus::Ok as i32 && s.result.is_some())
    );
    assert_eq!(r.agents[0].source, AgentSource::Loki as i32);

    assert_ne!(qa.query_id, qb.query_id, "one id per agent");
    assert_eq!(r.agents[0].result.as_ref().unwrap().query_id, qa.query_id);
    assert_eq!(qa.trace_id.as_deref(), Some("abc"));
    assert_eq!(qa.window, logs(LONG).window);
    let limits = qa.limits.unwrap();
    assert_eq!(limits.max_bytes_total, 4 << 20, "8MiB over two agents");
    assert_eq!(limits.max_lines_per_workload, 1000);
    assert_eq!(limits.context_lines, 5);
    // the agent must answer before the backend's deadline minus our margin
    let agent_deadline = qa.deadline.unwrap();
    let ns = |t: &Timestamp| t.seconds as i128 * 1_000_000_000 + t.nanos as i128;
    let early_by = Duration::from_nanos((ns(&backend_deadline) - ns(&agent_deadline)) as u64);
    // margin is 500ms, give or take the time the query itself took
    assert!(
        early_by > Duration::from_millis(450) && early_by <= Duration::from_millis(500),
        "{early_by:?}"
    );
}

#[tokio::test]
async fn budget_share_stays_inside_the_protocol_bounds() {
    let reg = registry(100, 1 << 10);
    let mut agents: Vec<_> = (0..3).map(|i| join(&reg, &format!("a{i}"), &[])).collect();
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs(LONG)).await })
    };
    for a in &mut agents {
        let q = a.query().await;
        assert_eq!(
            q.limits.unwrap().max_bytes_total,
            1 << 10,
            "never below 1KiB"
        );
        a.answer(&reg, &q, false);
    }
    query.await.unwrap().unwrap();
}

#[tokio::test]
async fn truncated_and_failed_answers_get_their_own_status() {
    let reg = registry(100, 8 << 20);
    let (mut cut, mut bad) = (join(&reg, "cut", &[]), join(&reg, "bad", &[]));
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs(LONG)).await })
    };
    let q = cut.query().await;
    cut.answer(&reg, &q, true);
    let q = bad.query().await;
    reg.deliver(
        &bad.conn,
        Message::Error(QueryError {
            query_id: q.query_id,
            error: Some(SourceError {
                kind: SourceErrorKind::AccessDenied as i32,
                detail: "nope".into(),
            }),
        }),
    );

    let r = query.await.unwrap().unwrap();
    let (bad_s, cut_s) = (&r.agents[0], &r.agents[1]);
    assert_eq!(bad_s.status, AgentStatus::Error as i32);
    assert!(bad_s.result.is_none());
    assert!(
        bad_s.detail.contains("ACCESS_DENIED") && bad_s.detail.contains("nope"),
        "{}",
        bad_s.detail
    );
    assert_eq!(cut_s.status, AgentStatus::Truncated as i32);
    assert!(cut_s.result.as_ref().unwrap().truncated);
}

#[tokio::test]
async fn a_silent_agent_times_out_and_gets_cancelled_while_others_answer() {
    let reg = registry(100, 8 << 20);
    let (mut slow, mut fast) = (join(&reg, "slow", &[]), join(&reg, "fast", &[]));
    let started = Instant::now();
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs(Duration::from_millis(700))).await })
    };
    let q = fast.query().await;
    fast.answer(&reg, &q, false);
    let asked = slow.query().await;

    let r = query.await.unwrap().unwrap();
    assert!(
        started.elapsed() < Duration::from_millis(700),
        "must reply before the backend deadline"
    );
    assert_eq!(r.agents[0].status, AgentStatus::Ok as i32);
    assert_eq!(r.agents[1].agent_id, "slow");
    assert_eq!(r.agents[1].status, AgentStatus::Timeout as i32);
    assert!(r.agents[1].result.is_none());

    match slow.next().await {
        coordinator_message::Message::Cancel(c) => assert_eq!(c.query_id, asked.query_id),
        other => panic!("expected a cancel, got {other:?}"),
    }
    // a late answer for the cancelled query goes nowhere, and must not panic
    slow.answer(&reg, &asked, false);
}

#[tokio::test]
async fn an_agent_that_drops_mid_query_is_disconnected_right_away() {
    let reg = registry(100, 8 << 20);
    let mut gone = join(&reg, "gone", &["db"]);
    let started = Instant::now();
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs(LONG)).await })
    };
    gone.query().await;
    reg.deregister(&gone.conn);

    let r = query.await.unwrap().unwrap();
    assert_eq!(r.agents[0].status, AgentStatus::Disconnected as i32);
    assert!(
        started.elapsed() < Duration::from_secs(2),
        "no waiting for the deadline"
    );
    assert!(reg.is_empty());
}

#[tokio::test]
async fn services_claimed_twice_are_flagged_but_not_deduped() {
    let reg = registry(100, 8 << 20);
    let mut agents = [
        join(&reg, "a", &["db", "cache", "cache"]),
        join(&reg, "b", &["db"]),
        join(&reg, "c", &["web"]),
    ];
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs(LONG)).await })
    };
    for a in &mut agents {
        let q = a.query().await;
        a.answer(&reg, &q, false);
    }
    let r = query.await.unwrap().unwrap();

    assert_eq!(r.agents.len(), 3, "both claimants keep their section");
    assert_eq!(
        r.warnings.len(),
        1,
        "cache listed twice by one agent is not a duplicate"
    );
    let w = &r.warnings[0];
    assert_eq!(w.code, WarningCode::DuplicateService as i32);
    assert_eq!(w.service.as_deref(), Some("db"));
    assert_eq!(w.agent_ids, ["a", "b"]);
}

#[tokio::test]
async fn concurrent_backend_queries_do_not_mix_answers() {
    let reg = registry(100, 8 << 20);
    let mut agent = join(&reg, "a", &[]);
    let run = |id: &'static str| {
        let reg = reg.clone();
        tokio::spawn(async move {
            let mut q = logs(LONG);
            q.query_id = id.into();
            reg.query_logs(q).await
        })
    };
    let (one, two) = (run("one"), run("two"));
    for _ in 0..2 {
        let q = agent.query().await;
        agent.answer(&reg, &q, false);
    }
    let (one, two) = (one.await.unwrap().unwrap(), two.await.unwrap().unwrap());
    assert_eq!(
        (one.query_id.as_str(), two.query_id.as_str()),
        ("one", "two")
    );
    assert_eq!(one.agents.len() + two.agents.len(), 2);
    assert!(one.agents[0].result.is_some() && two.agents[0].result.is_some());
}

#[tokio::test]
async fn unusable_requests_are_aggregate_errors() {
    let reg = registry(500, 8 << 20);
    let mut no_window = logs(LONG);
    no_window.window = None;
    let mut no_deadline = logs(LONG);
    no_deadline.deadline = None;
    let mut past = logs(LONG);
    past.deadline = Some(Timestamp {
        seconds: 1,
        nanos: 0,
    });

    for (q, why) in [
        (no_window, "no window"),
        (no_deadline, "no deadline"),
        (past, "deadline passed"),
        (
            logs(Duration::from_millis(300)),
            "deadline inside the margin",
        ),
    ] {
        let e = reg.query_logs(q).await.unwrap_err();
        assert_eq!(e.query_id, "b1", "{why}");
        assert!(e.error.is_some(), "{why}");
    }
}

#[tokio::test]
async fn registration_rules() {
    let reg = registry(100, 8 << 20);
    let (tx, _rx) = mpsc::channel(1);
    let reg_with = |version, id: &str| seismo_proto::Register {
        protocol_version: version,
        agent_id: id.into(),
        ..Default::default()
    };

    assert_eq!(
        reg.register(reg_with(2, "a"), tx.clone())
            .err()
            .unwrap()
            .code(),
        tonic::Code::FailedPrecondition
    );
    assert_eq!(
        reg.register(reg_with(1, ""), tx.clone())
            .err()
            .unwrap()
            .code(),
        tonic::Code::InvalidArgument
    );
    let first = reg.register(reg_with(1, "a"), tx.clone()).unwrap();
    assert_eq!(
        reg.register(reg_with(1, "a"), tx.clone())
            .err()
            .unwrap()
            .code(),
        tonic::Code::AlreadyExists
    );
    // once the stream is gone the id is free again
    reg.deregister(&first);
    reg.register(reg_with(1, "a"), tx).unwrap();
}
