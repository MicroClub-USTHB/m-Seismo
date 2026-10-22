use prost_types::Timestamp;
use seismo_coordinator::{agents, config, registry::Registry};
use seismo_proto::{
    AgentMessage, AgentSource, AgentStatus, CoordinatorMessage, LogEntry, QueryLogs, QueryResult,
    Register, TimeWindow, WorkloadResult, agent_client::AgentClient, agent_message::Message,
    coordinator_message,
};
use std::{
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{net::TcpListener, sync::mpsc};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Code, Request, Status, Streaming, transport::Endpoint};

const TOKEN: &str = "s3cret";

async fn start() -> (String, Arc<Registry>) {
    let registry = Arc::new(Registry::new("coord".into(), config::Query::default()));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    tokio::spawn(agents::serve(listener, registry.clone(), TOKEN.into()));
    (url, registry)
}

fn register(version: u32, id: &str) -> AgentMessage {
    AgentMessage {
        message: Some(Message::Register(Register {
            protocol_version: version,
            agent_id: id.into(),
            source: AgentSource::Loki as i32,
            services: vec!["db".into()],
        })),
    }
}

type Live = (mpsc::Sender<AgentMessage>, Streaming<CoordinatorMessage>);

/// Like a real agent: bearer token in the metadata, `first` as first message.
async fn dial(url: &str, token: Option<&str>, first: AgentMessage) -> Result<Live, Status> {
    let channel = Endpoint::from_shared(url.to_owned())
        .unwrap()
        .connect()
        .await
        .unwrap();
    let bearer = token.map(|t| format!("Bearer {t}"));
    let mut client = AgentClient::with_interceptor(channel, move |mut req: Request<()>| {
        if let Some(b) = &bearer {
            req.metadata_mut()
                .insert("authorization", b.parse().unwrap());
        }
        Ok(req)
    });
    let (tx, rx) = mpsc::channel(8);
    tx.send(first).await.unwrap();
    let inbound = client.connect(ReceiverStream::new(rx)).await?.into_inner();
    Ok((tx, inbound))
}

async fn eventually(what: &str, mut cond: impl FnMut() -> bool) {
    for _ in 0..100 {
        if cond() {
            return;
        }
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    panic!("never happened: {what}");
}

fn logs() -> QueryLogs {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + Duration::from_secs(10);
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
        deadline: Some(Timestamp {
            seconds: t.as_secs() as i64,
            nanos: 0,
        }),
    }
}

#[tokio::test]
async fn registered_agent_answers_a_query_over_grpc() {
    let (url, reg) = start().await;
    let (tx, mut inbound) = dial(&url, Some(TOKEN), register(1, "db")).await.unwrap();
    assert_eq!(reg.len(), 1);

    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs()).await })
    };
    let Some(coordinator_message::Message::Query(q)) =
        inbound.message().await.unwrap().and_then(|m| m.message)
    else {
        panic!("expected a query");
    };
    tx.send(AgentMessage {
        message: Some(Message::Result(QueryResult {
            query_id: q.query_id,
            ..Default::default()
        })),
    })
    .await
    .unwrap();

    let r = query.await.unwrap().unwrap();
    assert_eq!(r.agents[0].agent_id, "db");
    assert_eq!(r.agents[0].status, AgentStatus::Ok as i32);
    assert_eq!(r.agents[0].source, AgentSource::Loki as i32);
}

#[tokio::test]
async fn bad_or_missing_token_is_unauthenticated() {
    let (url, reg) = start().await;
    for token in [Some("wrong"), Some(""), None] {
        let e = dial(&url, token, register(1, "db")).await.err().unwrap();
        assert_eq!(e.code(), Code::Unauthenticated, "{token:?}");
    }
    assert!(reg.is_empty());
}

#[tokio::test]
async fn bad_registrations_are_rejected_with_a_status() {
    let (url, reg) = start().await;
    let not_register = AgentMessage {
        message: Some(Message::Result(QueryResult::default())),
    };
    for (first, code, why) in [
        (
            register(2, "db"),
            Code::FailedPrecondition,
            "unsupported version",
        ),
        (register(1, ""), Code::InvalidArgument, "no agent_id"),
        (
            not_register,
            Code::InvalidArgument,
            "first message is not Register",
        ),
    ] {
        let e = dial(&url, Some(TOKEN), first).await.err().unwrap();
        assert_eq!(e.code(), code, "{why}");
    }
    assert!(reg.is_empty());
}

#[tokio::test]
async fn a_second_live_stream_for_an_agent_id_is_refused_until_the_first_closes() {
    let (url, reg) = start().await;
    let first = dial(&url, Some(TOKEN), register(1, "db")).await.unwrap();

    let e = dial(&url, Some(TOKEN), register(1, "db"))
        .await
        .err()
        .unwrap();
    assert_eq!(e.code(), Code::AlreadyExists);
    assert_eq!(reg.len(), 1);

    drop(first);
    eventually("the closed agent leaves the registry", || reg.is_empty()).await;
    dial(&url, Some(TOKEN), register(1, "db")).await.unwrap();
    assert_eq!(reg.len(), 1);
}

#[tokio::test]
async fn registering_twice_on_one_stream_ends_it() {
    let (url, reg) = start().await;
    let (tx, mut inbound) = dial(&url, Some(TOKEN), register(1, "db")).await.unwrap();
    tx.send(register(1, "db")).await.unwrap();

    let e = inbound.message().await.unwrap_err();
    assert_eq!(e.code(), Code::InvalidArgument);
    eventually("the offender is dropped", || reg.is_empty()).await;
}

#[tokio::test]
async fn results_bigger_than_grpcs_default_limit_get_through() {
    let (url, reg) = start().await;
    let (tx, mut inbound) = dial(&url, Some(TOKEN), register(1, "db")).await.unwrap();
    let query = {
        let reg = reg.clone();
        tokio::spawn(async move { reg.query_logs(logs()).await })
    };
    let Some(coordinator_message::Message::Query(q)) =
        inbound.message().await.unwrap().and_then(|m| m.message)
    else {
        panic!("expected a query");
    };
    // ~9 MiB, over tonic's 4 MiB default, under our 10 MiB cap
    let big = WorkloadResult {
        entries: vec![LogEntry {
            message: "x".repeat(9 << 20),
            ..Default::default()
        }],
        ..Default::default()
    };
    tx.send(AgentMessage {
        message: Some(Message::Result(QueryResult {
            query_id: q.query_id,
            workloads: vec![big],
            ..Default::default()
        })),
    })
    .await
    .unwrap();

    let r = query.await.unwrap().unwrap();
    assert_eq!(r.agents[0].status, AgentStatus::Ok as i32);
    assert_eq!(
        r.agents[0].result.as_ref().unwrap().workloads[0].entries[0]
            .message
            .len(),
        9 << 20
    );
}
