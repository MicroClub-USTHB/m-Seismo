//! The backend hop: we dial `Coordinator.Attach` and answer what comes down.
use crate::{agents::MAX_MESSAGE, config, registry::Registry};
use seismo_proto::{
    self as pb, backend_request::Request as BackendRequest, coordinator_client::CoordinatorClient,
    coordinator_event::Event,
};
use std::{
    sync::Arc,
    time::{Duration, Instant},
};
use tokio::{sync::mpsc, task::JoinSet};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Status, metadata::MetadataValue, transport::Endpoint};
use tracing::{info, warn};

pub const PROTOCOL_VERSION: u32 = 1;

const BACKOFF_MIN: Duration = Duration::from_secs(1);
const BACKOFF_MAX: Duration = Duration::from_secs(30);
// NOTE: same rule as the agent, a long healthy session resets the backoff
const BACKOFF_RESET_AFTER: Duration = Duration::from_secs(30);

/// Stay attached forever, reconnecting with exponential backoff.
pub async fn run(cfg: &config::Backend, coordinator_id: &str, registry: Arc<Registry>) -> ! {
    let mut backoff = BACKOFF_MIN;
    loop {
        let started = Instant::now();
        match attach(cfg, coordinator_id, registry.clone()).await {
            Ok(()) => info!("backend closed the stream"),
            Err(status) => warn!("backend attach ended: {status}"),
        }
        if started.elapsed() >= BACKOFF_RESET_AFTER {
            backoff = BACKOFF_MIN;
        }
        tokio::time::sleep(backoff).await;
        backoff = (backoff * 2).min(BACKOFF_MAX);
    }
}

/// One attachment: say hello, then answer `QueryLogs` until the stream dies.
/// `Ok` means the backend closed it cleanly.
pub async fn attach(
    cfg: &config::Backend,
    coordinator_id: &str,
    registry: Arc<Registry>,
) -> Result<(), Status> {
    let bearer: MetadataValue<_> = format!("Bearer {}", cfg.key)
        .parse()
        .map_err(|_| Status::invalid_argument("key is not a valid header value"))?;
    let channel = Endpoint::from_shared(cfg.url.clone())
        .map_err(|e| Status::invalid_argument(e.to_string()))?
        .connect_timeout(Duration::from_secs(10))
        .http2_keep_alive_interval(Duration::from_secs(20))
        .keep_alive_timeout(Duration::from_secs(10))
        .keep_alive_while_idle(true)
        .connect()
        .await
        .map_err(|e| Status::unavailable(e.to_string()))?;
    let mut client =
        CoordinatorClient::with_interceptor(channel, move |mut req: tonic::Request<()>| {
            req.metadata_mut().insert("authorization", bearer.clone());
            Ok(req)
        })
        .max_decoding_message_size(MAX_MESSAGE)
        .max_encoding_message_size(MAX_MESSAGE);

    let (tx, rx) = mpsc::channel::<pb::CoordinatorEvent>(16);
    // buffered, so it is the first thing on the wire
    let hello = pb::Hello {
        protocol_version: PROTOCOL_VERSION,
        coordinator_id: coordinator_id.to_owned(),
    };
    tx.send(wire(Event::Hello(hello)))
        .await
        .expect("receiver is alive");
    let mut inbound = client.attach(ReceiverStream::new(rx)).await?.into_inner();

    // queries run side by side, and die with the attachment
    let mut tasks = JoinSet::new();
    while let Some(msg) = inbound.message().await? {
        if let Some(BackendRequest::QueryLogs(q)) = msg.request {
            let (tx, registry) = (tx.clone(), registry.clone());
            tasks.spawn(async move {
                let event = match registry.query_logs(q).await {
                    Ok(result) => Event::Result(result),
                    Err(error) => Event::Error(error),
                };
                let _ = tx.send(wire(event)).await;
            });
        }
    }
    Ok(())
}

fn wire(e: Event) -> pb::CoordinatorEvent {
    pb::CoordinatorEvent { event: Some(e) }
}
