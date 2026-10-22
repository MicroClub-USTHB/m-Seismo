use tonic::codegen::BoxStream;
use tonic::{Request, Response, Status, Streaming, transport::Server};

pub mod pb {
    tonic::include_proto!("seismo.v1");
}

use pb::agent_server::{Agent, AgentServer};
use pb::coordinator_server::{Coordinator, CoordinatorServer};
use pb::{AgentMessage, BackendRequest, CoordinatorEvent, CoordinatorMessage};

struct Service;

#[tonic::async_trait]
impl Agent for Service {
    type ConnectStream = BoxStream<CoordinatorMessage>;

    async fn connect(
        &self,
        _req: Request<Streaming<AgentMessage>>,
    ) -> Result<Response<Self::ConnectStream>, Status> {
        unimplemented!()
    }
}

#[tonic::async_trait]
impl Coordinator for Service {
    type AttachStream = BoxStream<BackendRequest>;

    async fn attach(
        &self,
        _req: Request<Streaming<CoordinatorEvent>>,
    ) -> Result<Response<Self::AttachStream>, Status> {
        unimplemented!()
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = std::env::var("SEISMO_LISTEN")
        .unwrap_or_else(|_| "[::]:50051".into())
        .parse()?;
    Server::builder()
        .add_service(AgentServer::new(Service))
        .add_service(CoordinatorServer::new(Service))
        .serve(addr)
        .await?;
    Ok(())
}
