use crate::{config::Config, engine::Engine};
use seismo_proto::{
    self as pb, AgentSource, agent_client::AgentClient, agent_message::Message, coordinator_message,
};
use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};
use tokio::{sync::mpsc, task::JoinSet};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Status, metadata::MetadataValue, transport::Endpoint};

pub const PROTOCOL_VERSION: u32 = 1;

const BACKOFF_MIN: Duration = Duration::from_secs(1);
const BACKOFF_MAX: Duration = Duration::from_secs(30);
// NOTE: a session that lived this long was healthy, so the next failure is
// not part of a crash loop and we retry fast again
const BACKOFF_RESET_AFTER: Duration = Duration::from_secs(30);

/// Stay connected forever, reconnecting with exponential backoff.
pub async fn run(cfg: &Config, engine: Arc<Engine>) -> ! {
    let mut backoff = BACKOFF_MIN;
    loop {
        let started = Instant::now();
        match session(cfg, engine.clone()).await {
            Ok(()) => eprintln!("coordinator closed the stream"),
            Err(status) => eprintln!("session ended: {status}"),
        }
        if started.elapsed() >= BACKOFF_RESET_AFTER {
            backoff = BACKOFF_MIN;
        }
        // XXX: no jitter, fine until we have hundreds of agents restarting together
        tokio::time::sleep(backoff).await;
        backoff = (backoff * 2).min(BACKOFF_MAX);
    }
}

/// One connection: register, then answer queries until the stream dies.
/// `Ok` means the coordinator closed it cleanly.
pub async fn session(cfg: &Config, engine: Arc<Engine>) -> Result<(), Status> {
    let bearer: MetadataValue<_> = format!("Bearer {}", cfg.coordinator.token)
        .parse()
        .map_err(|_| Status::invalid_argument("token is not a valid header value"))?;
    let channel = Endpoint::from_shared(cfg.coordinator.url.clone())
        .map_err(|e| Status::invalid_argument(e.to_string()))?
        .connect_timeout(Duration::from_secs(10))
        // the coordinator drops us when keepalive fails, so we need to
        // notice a dead link too
        .http2_keep_alive_interval(Duration::from_secs(20))
        .keep_alive_timeout(Duration::from_secs(10))
        .keep_alive_while_idle(true)
        .connect()
        .await
        .map_err(|e| Status::unavailable(e.to_string()))?;
    let mut client = AgentClient::with_interceptor(channel, move |mut req: tonic::Request<()>| {
        req.metadata_mut().insert("authorization", bearer.clone());
        Ok(req)
    });

    let (tx, rx) = mpsc::channel::<pb::AgentMessage>(16);
    // buffered, so it is on the wire before anything else
    let register = pb::Register {
        protocol_version: PROTOCOL_VERSION,
        agent_id: cfg.agent_id.clone(),
        source: AgentSource::Loki as i32,
        services: engine.services().to_vec(),
    };
    tx.send(wire(Message::Register(register)))
        .await
        .expect("receiver is alive");
    let mut inbound = client.connect(ReceiverStream::new(rx)).await?.into_inner();

    // dropping the set aborts whatever is still running when we leave
    let mut tasks = JoinSet::new();
    let inflight: Arc<Mutex<HashMap<String, tokio::task::AbortHandle>>> = Default::default();
    while let Some(msg) = inbound.message().await? {
        match msg.message {
            Some(coordinator_message::Message::Query(q)) => {
                let (tx, engine, map) = (tx.clone(), engine.clone(), inflight.clone());
                let id = q.query_id.clone();
                let handle = tasks.spawn({
                    let id = id.clone();
                    async move {
                        let reply = match engine.answer(&q).await {
                            Ok(r) => Message::Result(r),
                            Err(e) => Message::Error(e),
                        };
                        map.lock().unwrap().remove(&id);
                        let _ = tx.send(wire(reply)).await;
                    }
                });
                inflight.lock().unwrap().insert(id, handle);
            }
            Some(coordinator_message::Message::Cancel(c)) => {
                if let Some(h) = inflight.lock().unwrap().remove(&c.query_id) {
                    h.abort();
                }
            }
            None => {}
        }
    }
    Ok(())
}

fn wire(m: Message) -> pb::AgentMessage {
    pb::AgentMessage { message: Some(m) }
}
