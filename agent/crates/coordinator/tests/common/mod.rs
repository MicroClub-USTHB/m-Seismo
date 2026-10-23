//! A stand-in for the Go backend: serves `Coordinator.Attach`, remembers
//! what the coordinator says and lets the test push requests down.
#![allow(dead_code)]
use prost_types::Timestamp;
use seismo_proto::{
    BackendRequest, CoordinatorEvent, QueryLogs, TimeWindow,
    backend_request::Request as Req,
    coordinator_event::Event,
    coordinator_server::{Coordinator, CoordinatorServer},
};
use std::{
    sync::{Arc, Mutex},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{net::TcpListener, sync::mpsc};
use tokio_stream::wrappers::{ReceiverStream, TcpListenerStream};
use tonic::{Request, Response, Status, Streaming, transport::Server};

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

pub struct Backend {
    pub url: String,
    pub to_coordinator: mpsc::Sender<Result<BackendRequest, Status>>,
    pub events: mpsc::UnboundedReceiver<Event>,
    pub auth: Arc<Mutex<Option<String>>>,
}

impl Backend {
    pub async fn start() -> Backend {
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
        Backend {
            url,
            to_coordinator,
            events,
            auth,
        }
    }

    pub async fn next(&mut self) -> Event {
        tokio::time::timeout(Duration::from_secs(10), self.events.recv())
            .await
            .expect("timed out waiting for the coordinator")
            .expect("coordinator hung up")
    }

    pub async fn send(&self, req: BackendRequest) {
        self.to_coordinator.send(Ok(req)).await.unwrap();
    }

    pub async fn ask(&self, q: QueryLogs) {
        let req = BackendRequest {
            request: Some(Req::QueryLogs(q)),
        };
        self.to_coordinator.send(Ok(req)).await.unwrap();
    }
}

/// Window 100s..200s, deadline `secs` from now.
pub fn logs_within(id: &str, secs: u64) -> QueryLogs {
    let t = SystemTime::now().duration_since(UNIX_EPOCH).unwrap() + Duration::from_secs(secs);
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

pub fn logs(id: &str) -> QueryLogs {
    logs_within(id, 10)
}
