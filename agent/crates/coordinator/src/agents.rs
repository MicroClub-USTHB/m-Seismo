//! The agent hop: serves `Agent.Connect`.
use crate::registry::Registry;
use seismo_proto::{
    AgentMessage, CoordinatorMessage,
    agent_message::Message,
    agent_server::{Agent, AgentServer},
};
use std::{sync::Arc, time::Duration};
use tokio::{net::TcpListener, sync::mpsc};
use tokio_stream::wrappers::{ReceiverStream, TcpListenerStream};
use tonic::{Request, Response, Status, Streaming, metadata::MetadataMap, transport::Server};

/// Both hops cap messages at 10 MiB (8 MiB of logs plus overhead).
pub const MAX_MESSAGE: usize = 10 << 20;

pub struct AgentService {
    registry: Arc<Registry>,
    token: String,
}

impl AgentService {
    pub fn new(registry: Arc<Registry>, token: String) -> Self {
        Self { registry, token }
    }
}

#[tonic::async_trait]
impl Agent for AgentService {
    type ConnectStream = ReceiverStream<Result<CoordinatorMessage, Status>>;

    async fn connect(
        &self,
        req: Request<Streaming<AgentMessage>>,
    ) -> Result<Response<Self::ConnectStream>, Status> {
        authorize(req.metadata(), &self.token)?;
        let mut inbound = req.into_inner();
        let Some(Message::Register(register)) = inbound.message().await?.and_then(|m| m.message)
        else {
            return Err(Status::invalid_argument("first message must be Register"));
        };
        let (tx, rx) = mpsc::channel(32);
        let conn = self.registry.register(register, tx.clone())?;

        let registry = self.registry.clone();
        tokio::spawn(async move {
            loop {
                match inbound.message().await {
                    Ok(Some(AgentMessage {
                        message: Some(Message::Register(_)),
                    })) => {
                        let _ = tx
                            .send(Err(Status::invalid_argument("already registered")))
                            .await;
                        break;
                    }
                    Ok(Some(AgentMessage { message: Some(m) })) => registry.deliver(&conn, m),
                    Ok(Some(_)) => {}
                    // closed, or the link died (keepalive)
                    Ok(None) | Err(_) => break,
                }
            }
            registry.deregister(&conn);
        });
        Ok(Response::new(ReceiverStream::new(rx)))
    }
}

fn authorize(md: &MetadataMap, token: &str) -> Result<(), Status> {
    let given = md
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "));
    match given {
        Some(g) if ct_eq(g.as_bytes(), token.as_bytes()) => Ok(()),
        _ => Err(Status::unauthenticated("bad registration token")),
    }
}

/// Compare without bailing at the first differing byte.
fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0, |acc, (x, y)| acc | (x ^ y)) == 0
}

pub async fn serve(
    listener: TcpListener,
    registry: Arc<Registry>,
    token: String,
) -> Result<(), tonic::transport::Error> {
    let service = AgentServer::new(AgentService::new(registry, token))
        .max_decoding_message_size(MAX_MESSAGE)
        .max_encoding_message_size(MAX_MESSAGE);
    Server::builder()
        // a dead agent must drop out of the registry, the closed stream does that
        .http2_keepalive_interval(Some(Duration::from_secs(20)))
        .http2_keepalive_timeout(Some(Duration::from_secs(10)))
        .add_service(service)
        .serve_with_incoming(TcpListenerStream::new(listener))
        .await
}
