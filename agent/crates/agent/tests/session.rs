mod common;

use common::{serve, streams};
use prost_types::Timestamp;
use seismo_agent::{config, engine::Engine, session, source::Source};
use seismo_proto::{
    AgentMessage, AgentSource, Cancel, CoordinatorMessage, Query, QueryLimits, TimeWindow,
    agent_message::Message,
    agent_server::{Agent, AgentServer},
    coordinator_message,
};
use serde_json::json;
use std::{
    sync::{Arc, Mutex},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{net::TcpListener, sync::mpsc, task::JoinHandle};
use tokio_stream::wrappers::{ReceiverStream, TcpListenerStream};
use tonic::{Code, Request, Response, Status, Streaming, transport::Server};

type ToAgent = mpsc::Sender<Result<CoordinatorMessage, Status>>;

/// Coordinator stand-in: forwards what the agent says to the test and lets
/// the test push whatever it wants down the stream.
struct Fake {
    auth: Arc<Mutex<Option<String>>>,
    events: mpsc::UnboundedSender<Message>,
    outbound: Mutex<Option<mpsc::Receiver<Result<CoordinatorMessage, Status>>>>,
}

#[tonic::async_trait]
impl Agent for Fake {
    type ConnectStream = ReceiverStream<Result<CoordinatorMessage, Status>>;

    async fn connect(
        &self,
        req: Request<Streaming<AgentMessage>>,
    ) -> Result<Response<Self::ConnectStream>, Status> {
        let auth = req.metadata().get("authorization");
        *self.auth.lock().unwrap() = auth.map(|v| v.to_str().unwrap().to_owned());
        let mut inbound = req.into_inner();
        let events = self.events.clone();
        tokio::spawn(async move {
            while let Ok(Some(m)) = inbound.message().await {
                if let Some(m) = m.message {
                    let _ = events.send(m);
                }
            }
        });
        let rx = self
            .outbound
            .lock()
            .unwrap()
            .take()
            .expect("one connection per fake");
        Ok(Response::new(ReceiverStream::new(rx)))
    }
}

struct Harness {
    to_agent: ToAgent,
    events: mpsc::UnboundedReceiver<Message>,
    auth: Arc<Mutex<Option<String>>>,
    agent: JoinHandle<Result<(), Status>>,
}

impl Harness {
    async fn next(&mut self) -> Message {
        tokio::time::timeout(Duration::from_secs(5), self.events.recv())
            .await
            .expect("timed out waiting for the agent")
            .expect("agent hung up")
    }

    async fn send(&self, m: coordinator_message::Message) {
        self.to_agent
            .send(Ok(CoordinatorMessage { message: Some(m) }))
            .await
            .unwrap();
    }
}

fn ts(s: i64) -> Timestamp {
    Timestamp {
        seconds: s,
        nanos: 0,
    }
}

fn query(id: &str) -> Query {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;
    Query {
        query_id: id.into(),
        window: Some(TimeWindow {
            from: Some(ts(100)),
            to: Some(ts(200)),
        }),
        trace_id: None,
        limits: Some(QueryLimits {
            max_lines_per_workload: 10,
            max_bytes_total: 1 << 20,
            context_lines: 0,
        }),
        deadline: Some(ts(now + 30)),
    }
}

fn loki_cfg(url: &str) -> config::Loki {
    config::Loki {
        url: url.into(),
        service_label: "service_name".into(),
        scope_label: None,
        tenant: None,
        trace_field: "trace_id".into(),
        auth: config::Auth::None,
    }
}

/// Starts the fake coordinator and an agent session talking to it.
async fn start(loki_delay: Duration) -> Harness {
    let loki = serve(loki_delay, |_| {
        (
            200,
            streams(&[(json!({}), &[("150", "hello password=pw")])]),
        )
    })
    .await;
    let (to_agent, outbound) = mpsc::channel(8);
    let (events_tx, events) = mpsc::unbounded_channel();
    let auth = Arc::new(Mutex::new(None));
    let fake = Fake {
        auth: auth.clone(),
        events: events_tx,
        outbound: Mutex::new(Some(outbound)),
    };

    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    tokio::spawn(
        Server::builder()
            .add_service(AgentServer::new(fake))
            .serve_with_incoming(TcpListenerStream::new(listener)),
    );

    let cfg = config::Config {
        agent_id: "db-agent".into(),
        services: vec!["postgres".into()],
        coordinator: config::Coordinator {
            url,
            token_env: String::new(),
            token: "tok".into(),
            ca_file: None,
            ca_pem: None,
        },
        source: config::Source::Loki(loki_cfg(&loki.url)),
    };
    let engine = Arc::new(Engine::new(
        cfg.services.clone(),
        Source::build(&cfg.source).unwrap(),
    ));
    let agent = tokio::spawn(async move {
        let _keep_loki_alive = &loki;
        session::session(&cfg, engine).await
    });
    Harness {
        to_agent,
        events,
        auth,
        agent,
    }
}

#[tokio::test]
async fn registers_with_token_then_answers_queries() {
    let mut h = start(Duration::ZERO).await;

    let Message::Register(r) = h.next().await else {
        panic!("register must come first")
    };
    assert_eq!(r.protocol_version, 1);
    assert_eq!(r.agent_id, "db-agent");
    assert_eq!(r.source, AgentSource::Loki as i32);
    assert_eq!(r.services, ["postgres"]);
    assert_eq!(h.auth.lock().unwrap().as_deref(), Some("Bearer tok"));

    h.send(coordinator_message::Message::Query(query("q1")))
        .await;
    let Message::Result(res) = h.next().await else {
        panic!("expected a result")
    };
    assert_eq!(res.query_id, "q1");
    assert_eq!(
        res.workloads[0].entries[0].message,
        "hello password=[REDACTED]"
    );

    // closing the stream is a clean end
    drop(h.to_agent);
    assert!(h.agent.await.unwrap().is_ok());
}

#[tokio::test]
async fn answers_concurrent_queries_by_id() {
    let mut h = start(Duration::from_millis(50)).await;
    h.next().await; // register
    h.send(coordinator_message::Message::Query(query("a")))
        .await;
    h.send(coordinator_message::Message::Query(query("b")))
        .await;

    let mut ids = Vec::new();
    for _ in 0..2 {
        let Message::Result(r) = h.next().await else {
            panic!("expected a result")
        };
        ids.push(r.query_id);
    }
    ids.sort();
    assert_eq!(ids, ["a", "b"]);
}

#[tokio::test]
async fn unanswerable_query_comes_back_as_query_error() {
    let mut h = start(Duration::ZERO).await;
    h.next().await;
    let mut q = query("bad");
    q.window = None;
    h.send(coordinator_message::Message::Query(q)).await;

    let Message::Error(e) = h.next().await else {
        panic!("expected an error")
    };
    assert_eq!(e.query_id, "bad");
    assert!(e.error.is_some());
}

#[tokio::test]
async fn cancelled_query_gets_no_reply() {
    let mut h = start(Duration::from_millis(300)).await;
    h.next().await;
    h.send(coordinator_message::Message::Query(query("q1")))
        .await;
    h.send(coordinator_message::Message::Cancel(Cancel {
        query_id: "q1".into(),
    }))
    .await;

    let late = tokio::time::timeout(Duration::from_millis(900), h.events.recv()).await;
    assert!(late.is_err(), "got a reply for a cancelled query: {late:?}");
}

#[tokio::test]
async fn logs_only_source_answers_status_per_service_as_unsupported() {
    let mut h = start(Duration::ZERO).await;
    h.next().await; // register
    let later = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
        + 60;
    h.send(coordinator_message::Message::StatusQuery(
        seismo_proto::StatusQuery {
            query_id: "s1".into(),
            deadline: Some(ts(later)),
        },
    ))
    .await;

    let Message::StatusResult(r) = h.next().await else {
        panic!("expected a status result")
    };
    assert_eq!(r.query_id, "s1");
    assert_eq!(r.services.len(), 1);
    let postgres = &r.services[0];
    assert_eq!(postgres.service, "postgres");
    assert_eq!(
        postgres.status,
        seismo_proto::WorkloadStatus::Unavailable as i32
    );
    assert_eq!(
        postgres.error.as_ref().map(|e| e.kind),
        Some(seismo_proto::SourceErrorKind::DriverUnsupported as i32)
    );
}

#[tokio::test]
async fn rejection_status_ends_the_session() {
    let mut h = start(Duration::ZERO).await;
    h.next().await;
    h.to_agent
        .send(Err(Status::already_exists("agent_id in use")))
        .await
        .unwrap();

    let err = h.agent.await.unwrap().unwrap_err();
    assert_eq!(err.code(), Code::AlreadyExists);
}

#[tokio::test]
async fn unreachable_coordinator_is_an_error() {
    let dead = {
        let l = TcpListener::bind("127.0.0.1:0").await.unwrap();
        format!("http://{}", l.local_addr().unwrap())
    };
    let cfg = config::Config {
        agent_id: "x".into(),
        services: vec!["s".into()],
        coordinator: config::Coordinator {
            url: dead,
            token_env: String::new(),
            token: "t".into(),
            ca_file: None,
            ca_pem: None,
        },
        source: config::Source::Loki(loki_cfg("http://127.0.0.1:1")),
    };
    let engine = Arc::new(Engine::new(
        cfg.services.clone(),
        Source::build(&cfg.source).unwrap(),
    ));
    let err = session::session(&cfg, engine).await.unwrap_err();
    assert_eq!(err.code(), Code::Unavailable);
}
