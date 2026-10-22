use prost_types::Timestamp;
use seismo_coordinator::{backend, config, registry::Registry};
use seismo_proto::{
    AgentSource, AgentStatus, BackendRequest, CoordinatorEvent, QueryLogs, QueryResult, Register,
    TimeWindow,
    agent_message::Message,
    backend_request::Request as Req,
    coordinator_event::Event,
    coordinator_message,
    coordinator_server::{Coordinator, CoordinatorServer},
};
use std::{
    sync::{Arc, Mutex},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{net::TcpListener, sync::mpsc, task::JoinHandle};
use tokio_stream::wrappers::{ReceiverStream, TcpListenerStream};
use tonic::{Code, Request, Response, Status, Streaming, transport::Server};

/// Backend stand-in: remembers what the coordinator says and lets the test
/// push requests (or a status) down the stream.
struct Fake {
    auth: Arc<Mutex<Option<String>>>,
    events: mpsc::UnboundedSender<Event>,
    outbound: Mutex<Option<mpsc::Receiver<Result<BackendRequest, Status>>>>,
}

#[tonic::async_trait]
impl Coordinator for Fake {
    type AttachStream = ReceiverStream<Result<BackendRequest, Status>>;

    async fn attach(
        &self,
        req: Request<Streaming<CoordinatorEvent>>,
    ) -> Result<Response<Self::AttachStream>, Status> {
        let auth = req.metadata().get("authorization");
        *self.auth.lock().unwrap() = auth.map(|v| v.to_str().unwrap().to_owned());
        let mut inbound = req.into_inner();
        let events = self.events.clone();
        tokio::spawn(async move {
            while let Ok(Some(m)) = inbound.message().await {
                if let Some(e) = m.event {
                    let _ = events.send(e);
                }
            }
        });
        let rx = self
            .outbound
            .lock()
            .unwrap()
            .take()
            .expect("one attach per fake");
        Ok(Response::new(ReceiverStream::new(rx)))
    }
}

struct Harness {
    to_coordinator: mpsc::Sender<Result<BackendRequest, Status>>,
    events: mpsc::UnboundedReceiver<Event>,
    auth: Arc<Mutex<Option<String>>>,
    registry: Arc<Registry>,
    attached: JoinHandle<Result<(), Status>>,
}

impl Harness {
    async fn next(&mut self) -> Event {
        tokio::time::timeout(Duration::from_secs(5), self.events.recv())
            .await
            .expect("timed out waiting for the coordinator")
            .expect("coordinator hung up")
    }

    async fn ask(&self, q: QueryLogs) {
        let req = BackendRequest {
            request: Some(Req::QueryLogs(q)),
        };
        self.to_coordinator.send(Ok(req)).await.unwrap();
    }
}

async fn start() -> Harness {
    let (to_coordinator, outbound) = mpsc::channel(8);
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
            .add_service(CoordinatorServer::new(fake))
            .serve_with_incoming(TcpListenerStream::new(listener)),
    );

    let registry = Arc::new(Registry::new(
        "coord-1".into(),
        config::Query {
            margin_ms: 100,
            ..Default::default()
        },
    ));
    let cfg = config::Backend {
        url,
        key_env: String::new(),
        key: "mco_testkey".into(),
    };
    let attached = {
        let registry = registry.clone();
        tokio::spawn(async move { backend::attach(&cfg, "coord-1", registry).await })
    };
    Harness {
        to_coordinator,
        events,
        auth,
        registry,
        attached,
    }
}

fn logs(id: &str) -> QueryLogs {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + Duration::from_secs(10);
    QueryLogs {
        query_id: id.into(),
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
async fn says_hello_first_with_the_key_then_answers_queries() {
    let mut h = start().await;

    let Event::Hello(hello) = h.next().await else {
        panic!("hello must come first");
    };
    assert_eq!(hello.protocol_version, 1);
    assert_eq!(hello.coordinator_id, "coord-1");
    assert_eq!(
        h.auth.lock().unwrap().as_deref(),
        Some("Bearer mco_testkey")
    );

    // no agent connected is still an answer
    h.ask(logs("q1")).await;
    let Event::Result(r) = h.next().await else {
        panic!("expected an aggregate result");
    };
    assert_eq!(r.query_id, "q1");
    assert!(r.agents.is_empty());
}

#[tokio::test]
async fn coordinator_faults_come_back_as_aggregate_errors() {
    let mut h = start().await;
    h.next().await;
    let mut q = logs("bad");
    q.window = None;
    h.ask(q).await;

    let Event::Error(e) = h.next().await else {
        panic!("expected an aggregate error");
    };
    assert_eq!(e.query_id, "bad");
    assert!(e.error.is_some());
}

#[tokio::test]
async fn queries_run_side_by_side() {
    let mut h = start().await;
    h.next().await;
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
    h.ask(logs("one")).await;
    h.ask(logs("two")).await;
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
        let Event::Result(r) = h.next().await else {
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
    h.next().await;
    h.to_coordinator
        .send(Err(Status::unauthenticated("bad key")))
        .await
        .unwrap();

    let e = h.attached.await.unwrap().unwrap_err();
    assert_eq!(e.code(), Code::Unauthenticated);
}

#[tokio::test]
async fn a_closed_stream_is_a_clean_end() {
    let mut h = start().await;
    h.next().await;
    drop(h.to_coordinator);
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
